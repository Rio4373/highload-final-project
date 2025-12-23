#!/usr/bin/env bash
set -euo pipefail

listen_port="8080"
if [ -f .run-local.env ]; then
  listen_addr=$(grep '^LISTEN_ADDR=' .run-local.env | cut -d= -f2)
  if [ -n "$listen_addr" ]; then
    listen_port="${listen_addr##*:}"
  fi
fi

PIDS=$(lsof -ti tcp:${listen_port} || true)
if [ -n "$PIDS" ]; then
  kill $PIDS
  echo "stopped service on :${listen_port}"
fi

redis_names=$(docker ps -a --format '{{.Names}}' | grep '^redis-test' || true)
if [ -n "$redis_names" ]; then
  for name in $redis_names; do
    docker rm -f "$name" >/dev/null
    echo "removed ${name} container"
  done
fi

if [ -f .run-local.env ]; then
  rm -f .run-local.env
fi
