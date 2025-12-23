# Высоконагруженные системы — финальный проект

## Титульный лист

Проект: Сервис обработки потоковых метрик (Go + Redis + Prometheus)

Дисциплина: Высоконагруженные системы


## Введение

Цель проекта — реализовать высоконагруженный сервис для приема потоковых метрик, выполнить базовую статистическую аналитику (rolling average и z-score), развернуть сервис в Kubernetes с автоскейлингом и мониторингом. Решение ориентировано на поток данных от IoT/edge-устройств или API-шлюзов.

## Архитектура решения

Компоненты:
- Go-сервис (HTTP + горутины + каналы) принимает метрики и выполняет rolling average (окно 50) и z-score (threshold=2.0).
- Redis используется для кэширования последних метрик и вычислений.
- Prometheus собирает метрики сервиса, Grafana визуализирует дашборды, Alertmanager отправляет алерты по аномалиям.
- Minikube — локальный кластер Kubernetes, HPA — автоскейлинг по CPU.

Поток данных:
1) Клиент отправляет JSON на `/ingest`.
2) Сервис кладет метрику в очередь, воркер обрабатывает статистику.
3) Результаты кэшируются в Redis и экспортируются в Prometheus.

## Развертывание

### 1) Требования
- Go 1.23+
- Docker
- Minikube
- kubectl
- Helm
- Python 3 + Locust

### 2) Локальный запуск (без Kubernetes)

Запуск Redis:
```
docker run -p 6379:6379 --name redis -d redis:7
```

Запуск сервиса:
```
export REDIS_ADDR=127.0.0.1:6379
export WINDOW_SIZE=50
export Z_THRESHOLD=2.0
export QUEUE_SIZE=10000
export LISTEN_ADDR=0.0.0.0:8080

go run ./
```

Примечание: на macOS используйте `127.0.0.1` вместо `localhost`, чтобы избежать IPv6 адреса `::1`.

Быстрый запуск и остановка:
```
./scripts/run-local.sh
./scripts/stop-local.sh
```

Проверка:
```
curl -X POST http://localhost:8080/ingest -d '{"timestamp": 1710000000, "cpu": 55.0, "rps": 420.0}'

curl http://localhost:8080/analyze

curl http://localhost:8080/metrics | head -n 20
```

Профилирование (pprof):
```
go tool pprof http://localhost:8080/debug/pprof/profile?seconds=30
```

### 3) Docker образ

```
docker build -t streaming-service:latest .
```

### 4) Minikube + Kubernetes

Запуск Minikube:
```
minikube start --cpus=2 --memory=4g
minikube addons enable ingress
minikube addons enable metrics-server
```

Сборка образа прямо в Minikube:
```
minikube image build -t streaming-service:latest .
```

Namespace:
```
kubectl apply -f k8s/namespace.yaml
```

Redis через Helm (bitnami/redis):
```
helm repo add bitnami https://charts.bitnami.com/bitnami
helm repo update
helm install redis bitnami/redis -n highload -f k8s/redis-values.yaml
```

Сервис и HPA:
```
kubectl apply -f k8s/configmap.yaml
kubectl apply -f k8s/deployment.yaml
kubectl apply -f k8s/service.yaml
kubectl apply -f k8s/ingress.yaml
kubectl apply -f k8s/hpa.yaml
```

Проверка:
```
kubectl get pods -n highload
kubectl get hpa -n highload
```

Ingress (локально):
```
minikube ip
```
Добавьте в `/etc/hosts` запись: `<minikube_ip> streaming.local`.

### 5) Prometheus + Grafana + Alertmanager

Установка через kube-prometheus-stack (включает Prometheus, Grafana, Alertmanager):
```
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update
helm install kube-prometheus-stack prometheus-community/kube-prometheus-stack -n highload
```

ServiceMonitor и alert rules:
```
kubectl apply -f k8s/monitoring/servicemonitor.yaml
kubectl apply -f k8s/monitoring/prometheus-rule.yaml
```

Grafana:
```
kubectl -n highload port-forward svc/kube-prometheus-stack-grafana 3000:80
```
Логин/пароль по умолчанию: `admin/prom-operator`.

Импортируйте дашборды из файлов:
- `k8s/monitoring/grafana-dashboard-rps.json`
- `k8s/monitoring/grafana-dashboard-latency.json`
- `k8s/monitoring/grafana-dashboard-anomalies.json`

### 6) Нагрузочное тестирование (Locust)

Установка:
```
python -m pip install locust
```

Запуск:
```
locust -f locust/locustfile.py --headless -u 300 -r 50 --run-time 5m --host http://streaming.local --csv locust/results
```

Мониторинг HPA:
```
kubectl get hpa -n highload -w
kubectl get deploy -n highload -w
```

Логи сервиса:
```
kubectl logs -n highload -l app=streaming-service --tail=200
```

### 7) Онлайн-лаборатория (Killercoda)

Если используете Killercoda:
- Запустите Minikube или предоставленный кластер.
- Повторите команды из раздела Kubernetes (Helm + kubectl apply).
- Эквивалент локальной команды `minikube image build` замените на Docker Hub push и правку `image:` в `k8s/deployment.yaml`.

## Проверка по шагам (чеклист)

### Шаг 0. Подготовка окружения
```
go version
docker --version
minikube version
kubectl version --client
helm version
python3 --version
```

### Шаг 1. Локальный сервис + Redis
1) Запустите Redis:
```
docker run -d -p 6379:6379 --name redis-test redis:7
```
2) Запустите сервис:
```
./scripts/run-local.sh
```
3) Проверьте эндпоинты:
```
PORT=$(awk -F'[:=]' '/LISTEN_ADDR/{print $3}' .run-local.env)
curl -i http://127.0.0.1:${PORT}/health
curl -i -X POST http://127.0.0.1:${PORT}/ingest -H 'Content-Type: application/json' -d '{"timestamp": 1710000000, "cpu": 55, "rps": 420}'
curl -s http://127.0.0.1:${PORT}/analyze
curl -s http://127.0.0.1:${PORT}/metrics | head -n 20
```
4) Проверьте Redis:
```
docker exec redis-test redis-cli keys '*'
docker exec redis-test redis-cli get last_analysis
```

### Шаг 2. Docker образ
```
docker build -t streaming-service:latest .
docker images streaming-service:latest
```
Проверьте размер образа (должен быть < 300MB).

### Шаг 3. Minikube + Kubernetes
1) Запуск и аддоны:
```
minikube start --cpus=2 --memory=4g
minikube addons enable ingress
minikube addons enable metrics-server
```
2) Сборка образа внутри Minikube:
```
minikube image build -t streaming-service:latest .
```
3) Namespace и Redis:
```
kubectl apply -f k8s/namespace.yaml
helm repo add bitnami https://charts.bitnami.com/bitnami
helm repo update
helm install redis bitnami/redis -n highload -f k8s/redis-values.yaml
```
4) Сервис и HPA:
```
kubectl apply -f k8s/configmap.yaml
kubectl apply -f k8s/deployment.yaml
kubectl apply -f k8s/service.yaml
kubectl apply -f k8s/ingress.yaml
kubectl apply -f k8s/hpa.yaml
```
5) Проверка:
```
kubectl get pods -n highload
kubectl get svc -n highload
kubectl get ingress -n highload
kubectl get hpa -n highload
```
6) Настройка Ingress:
```
minikube ip
```
Добавьте в `/etc/hosts` строку: `<minikube_ip> streaming.local`.

### Шаг 4. Prometheus + Grafana + Alertmanager
1) Установка стека:
```
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update
helm install kube-prometheus-stack prometheus-community/kube-prometheus-stack -n highload
```
2) ServiceMonitor и алерты:
```
kubectl apply -f k8s/monitoring/servicemonitor.yaml
kubectl apply -f k8s/monitoring/prometheus-rule.yaml
```
3) Grafana:
```
kubectl -n highload port-forward svc/kube-prometheus-stack-grafana 3000:80
```
Логин/пароль: `admin/prom-operator`. Импортируйте 3 JSON дашборда.
4) Prometheus Targets:
```
kubectl -n highload port-forward svc/kube-prometheus-stack-prometheus 9090:9090
```
5) Alertmanager:
```
kubectl -n highload port-forward svc/kube-prometheus-stack-alertmanager 9093:9093
```

### Шаг 5. Нагрузочный тест (500 RPS, 5 минут)
```
python3 -m pip install locust
locust -f locust/locustfile.py --headless -u 300 -r 50 --run-time 5m --host http://streaming.local --csv locust/results
```
Одновременно:
```
kubectl get hpa -n highload -w
kubectl get deploy -n highload -w
```
Цели: HPA масштабирует до 4 реплик, p95 latency < 50ms, error rate минимальный.

### Шаг 6. Проверка аналитики (rolling average + z-score)
1) Отправьте стабильный поток RPS (например, 400–450) и убедитесь, что `rps_avg` близко к реальным значениям:
```
for rps in 420 430 410 440 425; do curl -s -X POST http://streaming.local/ingest -H 'Content-Type: application/json' -d "{\"timestamp\": 1710000001, \"cpu\": 60, \"rps\": $rps}" >/dev/null; done
curl -s http://streaming.local/analyze
```
2) Сымитируйте всплеск RPS и убедитесь, что `anomaly=true`:
```
curl -s -X POST http://streaming.local/ingest -H 'Content-Type: application/json' -d '{"timestamp": 1710000002, "cpu": 60, "rps": 900}'
curl -s http://streaming.local/analyze
```
3) Для отчета зафиксируйте: точность > 70% и false positive < 10% (по синтетическим данным).

## Сверка с ТЗ (максимальный балл)

Что уже реализовано в коде и манифестах:
- Go‑сервис 200–300 строк: `main.go` (299 строк).
- HTTP эндпоинты `/ingest`, `/analyze`, `/metrics` и `/health`.
- Rolling average (окно 50) и z-score (threshold=2.0).
- Goroutines + channels (асинхронная очередь и воркер).
- Redis кэширование последних метрик и расчетов.
- Docker образ на Alpine (ожидаемо < 300MB).
- Kubernetes: Deployment (2 replicas), Service, Ingress, HPA (70% CPU, 2–5).
- Prometheus/Grafana/Alertmanager, ServiceMonitor и alert rule.
- Dashboards: RPS, latency, anomaly rate.

Что нужно подтвердить запуском:
- Нагрузка 500 RPS (5 минут) с логами и CSV от Locust.
- Масштабирование HPA до 4 реплик.
- p95 latency < 50ms.
- Метрики точности и false positive на синтетических данных.

## Заключение

Проект реализует потоковый прием метрик, кэширование в Redis и статистическую аналитику без ИИ-моделей. В Kubernetes настроено масштабирование по CPU и мониторинг с алертами, что позволяет оценивать поведение сервиса под нагрузкой.

## Ссылки

- Go: https://go.dev/
- Redis: https://redis.io/
- Minikube: https://minikube.sigs.k8s.io/
- Helm: https://helm.sh/
- Prometheus: https://prometheus.io/
- Grafana: https://grafana.com/
- Locust: https://locust.io/

## Приложения

Файлы проекта:
- Исходный код: `main.go`
- Dockerfile: `Dockerfile`
- Locust: `locust/locustfile.py`
- Kubernetes YAML: `k8s/`
- Monitoring JSON: `k8s/monitoring/*.json`

Рекомендуемые скриншоты (10+). Формат: команда/экран → что должно быть видно.
1) `./scripts/run-local.sh` + `curl -i http://127.0.0.1:8080/health` → 200 OK.
2) `curl -i -X POST http://127.0.0.1:8080/ingest ...` → 202 Accepted.
3) `curl -s http://127.0.0.1:8080/analyze` → JSON с `rps_avg`, `anomaly`.
4) `docker exec redis-test redis-cli keys '*'` → `last_metric`, `last_analysis`, `metrics`.
5) `docker images streaming-service:latest` → размер образа (< 300MB).
6) `kubectl get pods -n highload` → поды Redis и streaming-service.
7) `kubectl get svc -n highload` → сервисы Redis и streaming-service.
8) `kubectl get ingress -n highload` → host `streaming.local`.
9) `kubectl describe hpa streaming-service -n highload` → Target CPU 70%, min 2, max 5.
10) Prometheus Targets UI → streaming-service `UP`.
11) Grafana dashboard RPS → график `metrics_ingest_total`.
12) Grafana dashboard Latency → p95 `/ingest`.
13) Grafana dashboard Anomalies → `metrics_anomaly_total`.
14) Alertmanager UI → алерт `HighAnomalyRate` (при всплеске RPS).
15) Locust headless вывод или CSV (`locust/results_*`) → RPS/latency.
16) `kubectl get hpa -n highload -w` во время нагрузки → рост реплик.

## Скриншоты (шаблон для вставки в README)

Сложите изображения в `screenshots/` и вставьте ссылки ниже.

1) Окружение:
```
![01-env](screenshots/01-env.png)
```
2) Локальный запуск:
```
![02-run-local](screenshots/02-run-local.png)
```
3) `/health`:
```
![03-health](screenshots/03-health.png)
```
4) `/ingest`:
```
![04-ingest](screenshots/04-ingest.png)
```
5) `/analyze`:
```
![05-analyze](screenshots/05-analyze.png)
```
6) Redis keys:
```
![06-redis-keys](screenshots/06-redis-keys.png)
```
7) Docker image size:
```
![07-docker-image](screenshots/07-docker-image.png)
```
8) K8s pods:
```
![08-k8s-pods](screenshots/08-k8s-pods.png)
```
9) K8s services:
```
![09-k8s-svc](screenshots/09-k8s-svc.png)
```
10) K8s ingress:
```
![10-k8s-ingress](screenshots/10-k8s-ingress.png)
```
11) HPA:
```
![11-hpa](screenshots/11-hpa.png)
```
12) Ingress URL:
```
![12-ingress-url](screenshots/12-ingress-url.png)
```
13) Grafana RPS:
```
![13-grafana-rps](screenshots/13-grafana-rps.png)
```
14) Grafana Latency:
```
![14-grafana-latency](screenshots/14-grafana-latency.png)
```
15) Grafana Anomalies:
```
![15-grafana-anomalies](screenshots/15-grafana-anomalies.png)
```
16) Prometheus Targets:
```
![16-prom-targets](screenshots/16-prom-targets.png)
```
17) Alertmanager:
```
![17-alertmanager](screenshots/17-alertmanager.png)
```
18) Locust results:
```
![18-locust](screenshots/18-locust.png)
```
19) HPA scale:
```
![19-hpa-scale](screenshots/19-hpa-scale.png)
```
