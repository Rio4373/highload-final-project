package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"math"
	"net/http"
	"net/http/pprof"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
)
type Metric struct {
	Timestamp int64   `json:"timestamp"`
	CPU       float64 `json:"cpu"`
	RPS       float64 `json:"rps"`
}
type Analysis struct {
	Timestamp int64   `json:"timestamp"`
	RPSAvg    float64 `json:"rps_avg"`
	RPSZ      float64 `json:"rps_z"`
	Anomaly   bool    `json:"anomaly"`
	Window    int     `json:"window"`
	Count    int64   `json:"count"`
}
type Analyzer struct {
	mu          sync.RWMutex
	windowSize  int
	threshold   float64
	rpsWindow   []float64
	lastAnalyze Analysis
	processed   int64
}

func NewAnalyzer(windowSize int, threshold float64) *Analyzer {
	return &Analyzer{
		windowSize: windowSize,
		threshold:  threshold,
		rpsWindow:  make([]float64, 0, windowSize),
	}
}
func (a *Analyzer) Process(m Metric) Analysis {
	a.mu.Lock()
	defer a.mu.Unlock()

	if m.Timestamp == 0 {
		m.Timestamp = time.Now().Unix()
	}

	meanPrev := mean(a.rpsWindow)
	stdPrev := stddev(a.rpsWindow, meanPrev)
	z := 0.0
	if stdPrev > 0 {
		z = (m.RPS - meanPrev) / stdPrev
	}
	anomaly := math.Abs(z) >= a.threshold

	a.rpsWindow = appendTrim(a.rpsWindow, m.RPS, a.windowSize)
	avg := mean(a.rpsWindow)

	a.processed++

	analysis := Analysis{
		Timestamp: m.Timestamp,
		RPSAvg:    avg,
		RPSZ:      z,
		Anomaly:   anomaly,
		Window:    a.windowSize,
		Count:     a.processed,
	}

	a.lastAnalyze = analysis
	return analysis
}
func (a *Analyzer) Snapshot() Analysis {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.lastAnalyze
}
type Processor struct {
	ctx      context.Context
	redis    *redis.Client
	analyzer *Analyzer
	input    <-chan Metric
}

func NewProcessor(ctx context.Context, redis *redis.Client, analyzer *Analyzer, input <-chan Metric) *Processor {
	return &Processor{ctx: ctx, redis: redis, analyzer: analyzer, input: input}
}
func (p *Processor) Run() {
	for metric := range p.input {
		analysis := p.analyzer.Process(metric)
		if analysis.Anomaly {
			anomalyTotal.Inc()
		}
		if p.redis == nil {
			continue
		}
		metricJSON, _ := json.Marshal(metric)
		analysisJSON, _ := json.Marshal(analysis)
		pipe := p.redis.Pipeline()
		pipe.LPush(p.ctx, "metrics", metricJSON)
		pipe.LTrim(p.ctx, "metrics", 0, 999)
		pipe.Set(p.ctx, "last_metric", metricJSON, 0)
		pipe.Set(p.ctx, "last_analysis", analysisJSON, 0)
		if _, err := pipe.Exec(p.ctx); err != nil {
			log.Printf("redis write failed: %v", err)
		}
	}
}

var (
	requestLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request latency in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"path", "code"},
	)
	ingestTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "metrics_ingest_total",
		Help: "Total accepted metrics.",
	})
	anomalyTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "metrics_anomaly_total",
		Help: "Total detected anomalies.",
	})
)

func init() {
	prometheus.MustRegister(requestLatency, ingestTotal, anomalyTotal)
}
func main() {
	windowSize := envInt("WINDOW_SIZE", 50)
	threshold := 2.0
	if val := os.Getenv("Z_THRESHOLD"); val != "" {
		if parsed, err := strconv.ParseFloat(val, 64); err == nil {
			threshold = parsed
		}
	}
	listenAddr := os.Getenv("LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = ":8080"
	}
	queueSize := envInt("QUEUE_SIZE", 10000)
	ctx := context.Background()
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "redis:6379"
	}
	redisClient := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: os.Getenv("REDIS_PASSWORD"),
		DB:       envInt("REDIS_DB", 0),
	})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Printf("redis ping failed: %v", err)
		redisClient = nil
	}
	if redisClient != nil {
		log.Println("redis connected")
	} else {
		log.Println("redis disabled")
	}
	analyzer := NewAnalyzer(windowSize, threshold)
	queue := make(chan Metric, queueSize)
	processor := NewProcessor(ctx, redisClient, analyzer, queue)
	go processor.Run()
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/ingest", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		status := http.StatusAccepted
		defer func() {
			requestLatency.WithLabelValues("/ingest", strconv.Itoa(status)).Observe(time.Since(start).Seconds())
		}()
		if r.Method != http.MethodPost {
			status = http.StatusMethodNotAllowed
			http.Error(w, "method not allowed", status)
			return
		}
		metric, err := decodeMetric(r)
		if err != nil {
			status = http.StatusBadRequest
			http.Error(w, err.Error(), status)
			return
		}
		select {
		case queue <- metric:
			ingestTotal.Inc()
			w.WriteHeader(status)
			_, _ = w.Write([]byte("accepted"))
		default:
			status = http.StatusServiceUnavailable
			http.Error(w, "queue full", status)
		}
	})
	mux.HandleFunc("/analyze", func(w http.ResponseWriter, r *http.Request) {
		analysis := analyzer.Snapshot()
		if analysis.Count == 0 {
			_, _ = w.Write([]byte("no data"))
			return
		}
		payload := map[string]any{
			"analysis": analysis,
		}
		writeJSON(w, payload)
	})

	server := &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	log.Printf("listening on %s", listenAddr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server failed: %v", err)
	}
}

func decodeMetric(r *http.Request) (Metric, error) {
	var metric Metric
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&metric); err != nil {
		return Metric{}, err
	}
	if metric.RPS < 0 {
		return Metric{}, errors.New("rps must be >= 0")
	}
	return metric, nil
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	_ = enc.Encode(payload)
}

func envInt(key string, fallback int) int {
	if val := os.Getenv(key); val != "" {
		parsed, err := strconv.Atoi(val)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func appendTrim(values []float64, v float64, limit int) []float64 {
	values = append(values, v)
	if len(values) > limit {
		values = values[len(values)-limit:]
	}
	return values
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func stddev(values []float64, mean float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		diff := v - mean
		sum += diff * diff
	}
	return math.Sqrt(sum / float64(len(values)))
}
