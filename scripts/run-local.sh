#!/usr/bin/env bash
set -euo pipefail

if ! command -v docker >/dev/null 2>&1; then
  echo "docker not found" >&2
  exit 1
fi

if ! docker ps >/dev/null 2>&1; then
  echo "docker daemon not running" >&2
  exit 1
fi

port_in_use() {
  local port="$1"
  if command -v lsof >/dev/null 2>&1; then
    lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1
    return $?
  fi
  return 1
}

find_free_port() {
  local port="$1"
  if command -v lsof >/dev/null 2>&1; then
    while port_in_use "$port"; do
      port=$((port + 1))
    done
  fi
  echo "$port"
}

container_port() {
  docker port "$1" 6379/tcp 2>/dev/null | awk -F: 'NR==1{print $NF}'
}

redis_name="redis-test"
redis_port="6379"

listen_addr="${LISTEN_ADDR:-0.0.0.0:8080}"
listen_host="${listen_addr%:*}"
listen_port="${listen_addr##*:}"
case "$listen_port" in
  ''|*[!0-9]*)
    echo "LISTEN_ADDR must be host:port (example 0.0.0.0:8080)" >&2
    exit 1
    ;;
esac

if docker ps --format '{{.Names}}' | grep -q "^${redis_name}$"; then
  host_port=$(container_port "$redis_name")
  if [ -n "$host_port" ]; then
    redis_port="$host_port"
  fi
else
  if port_in_use "$redis_port"; then
    redis_port=$(find_free_port 6380)
    redis_name="redis-test-${redis_port}"
  fi

  if docker ps -a --format '{{.Names}}' | grep -q "^${redis_name}$"; then
    docker start "$redis_name" >/dev/null
  else
    docker run -d -p "${redis_port}:6379" --name "${redis_name}" redis:7 >/dev/null
  fi
fi

echo "using redis ${redis_name} on 127.0.0.1:${redis_port}"
if port_in_use "$listen_port"; then
  listen_port=$(find_free_port "$((listen_port + 1))")
  listen_addr="${listen_host}:${listen_port}"
fi

echo "using service ${listen_addr}"

cat <<EOF > .run-local.env
REDIS_ADDR=127.0.0.1:${redis_port}
LISTEN_ADDR=${listen_addr}
EOF

REDIS_ADDR=127.0.0.1:${redis_port} \
LISTEN_ADDR=${listen_addr} \
WINDOW_SIZE=50 \
Z_THRESHOLD=2.0 \
QUEUE_SIZE=10000 \
go run ./
