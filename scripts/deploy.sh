#!/bin/bash
set -e

REPO_DIR=~/AgentDNS
BINARY=~/agentdns
CONFIG=~/.zynd/config.toml
LOG=~/agentdns.log

echo "==> Pulling latest code..."
cd "$REPO_DIR"
git pull origin main

echo "==> Building..."
go build -o "$BINARY" ./cmd/agentdns/

echo "==> Stopping old process..."
pkill -9 agentdns 2>/dev/null || true
sleep 1

echo "==> Starting..."
nohup "$BINARY" start --config "$CONFIG" > "$LOG" 2>&1 &
sleep 2

if pgrep -x agentdns > /dev/null; then
    echo "==> Running (PID $(pgrep -x agentdns))"
    tail -5 "$LOG"
else
    echo "==> FAILED to start. Log:"
    tail -20 "$LOG"
    exit 1
fi
