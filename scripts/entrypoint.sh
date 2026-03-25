#!/bin/sh
set -e

# Auto-initialize identity if it doesn't exist yet
if [ ! -f "$HOME/.zynd/identity.json" ]; then
  echo "No identity found — running 'agentdns init'..."
  agentdns init
fi

exec "$@"
