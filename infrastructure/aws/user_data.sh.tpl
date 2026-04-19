#!/bin/bash
set -euo pipefail

# --- System setup ---
apt-get update -y
apt-get install -y docker.io docker-compose-plugin postgresql-client-16 curl jq

systemctl enable docker
systemctl start docker

# Create zynd user
useradd -m -s /bin/bash zynd
usermod -aG docker zynd

# --- Pull and run the registry ---
mkdir -p /opt/zynd/config /opt/zynd/data

# Write config.toml
cat > /opt/zynd/config/config.toml <<'TOMLEOF'
[server]
port = 8080
mesh_port = 4001
node_name = "${node_name}"

[database]
dsn = "postgres://${db_user}:${db_password}@${db_host}:${db_port}/${db_name}?sslmode=require"

[heartbeat]
inactive_threshold_s = 300
sweep_interval_s = 60

[search]
backend = "hash"
bm25_min_score = 0.3

%{ if peer_address != "" ~}
[mesh]
bootstrap_peers = ["${peer_address}:4001"]
gossip_interval_s = 30
%{ endif ~}
TOMLEOF

chown -R zynd:zynd /opt/zynd

# --- Systemd service ---
cat > /etc/systemd/system/agentdns.service <<'SVCEOF'
[Unit]
Description=Agent DNS Registry
After=network-online.target
Wants=network-online.target

[Service]
User=zynd
ExecStart=/usr/local/bin/agentdns start --config /opt/zynd/config/config.toml
Restart=always
RestartSec=5
Environment=AGENTDNS_DATA_DIR=/opt/zynd/data

[Install]
WantedBy=multi-user.target
SVCEOF

# --- Download the latest release binary ---
# Replace with your actual binary URL or Docker pull
echo "TODO: Download agentdns binary or docker pull zynd/agentdns:latest"
echo "Node ${node_name} (${environment}) provisioned. DB: ${db_host}:${db_port}"
