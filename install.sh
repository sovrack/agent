#!/usr/bin/env bash
# ==============================================================================
# Sovrack Agent 1-Liner Automated Production Installer
# Website: https://sovrack.com | GitHub: https://github.com/sovrack/agent
# ==============================================================================
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${BLUE}======================================================${NC}"
echo -e "${CYAN}          SOVRACK OUTBOUND AGENT INSTALLER           ${NC}"
echo -e "${BLUE}======================================================${NC}"

if [ "$(id -u)" -ne 0 ]; then
  echo -e "${RED}[ERROR] This installer must be run as root (or with sudo).${NC}" >&2
  exit 1
fi

TOKEN=""
SERVER="wss://app.sovrack.com/agent-tunnel"
MODE="auto" # auto, docker, systemd
VERSION="1.0.0"

while [[ $# -gt 0 ]]; do
  case $1 in
    --token)
      TOKEN="$2"
      shift 2
      ;;
    --server)
      SERVER="$2"
      shift 2
      ;;
    --mode)
      MODE="$2"
      shift 2
      ;;
    --version)
      VERSION="$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done

if [ -z "$TOKEN" ]; then
  echo -e "${RED}[ERROR] Missing required --token parameter.${NC}"
  echo "Usage: curl -fsSL https://raw.githubusercontent.com/sovrack/agent/main/install.sh | sudo bash -s -- --token=<YOUR_AGENT_TOKEN> [--server=<URL>] [--mode=auto|docker|systemd]"
  exit 1
fi

# Detect architecture
ARCH=$(uname -m)
case $ARCH in
  x86_64)
    GOARCH="amd64"
    ;;
  aarch64|arm64)
    GOARCH="arm64"
    ;;
  *)
    echo -e "${RED}[ERROR] Unsupported architecture: $ARCH${NC}"
    exit 1
    ;;
esac

echo -e "${CYAN}→ Installing dependencies...${NC}"
if command -v apt-get &>/dev/null; then
  apt-get update -qq && apt-get install -y -qq curl ca-certificates tar
elif command -v yum &>/dev/null; then
  yum install -y -q curl ca-certificates tar
fi

echo -e "${CYAN}→ Configuring Sovrack Agent credentials...${NC}"
mkdir -p /etc/sovrack

cat <<EOF > /etc/sovrack/agent.env
SOVRACK_SERVER_URL="${SERVER}"
SOVRACK_AGENT_TOKEN="${TOKEN}"
SOVRACK_DOCKER_HOST="unix:///var/run/docker.sock"
EOF
chmod 600 /etc/sovrack/agent.env

# Determine deployment mode
if [ "$MODE" = "auto" ]; then
  if command -v docker &>/dev/null; then
    MODE="docker"
  elif command -v systemctl &>/dev/null; then
    MODE="systemd"
  else
    echo -e "${RED}[ERROR] Neither Docker nor systemd detected on this host.${NC}"
    exit 1
  fi
fi

if [ "$MODE" = "docker" ]; then
  echo -e "${CYAN}→ Starting Sovrack Agent via Docker container...${NC}"
  docker rm -f sovrack-agent 2>/dev/null || true
  docker run -d \
    --name sovrack-agent \
    --restart always \
    --net host \
    -v /var/run/docker.sock:/var/run/docker.sock \
    --env-file /etc/sovrack/agent.env \
    ghcr.io/sovrack/agent:latest
elif [ "$MODE" = "systemd" ]; then
  echo -e "${CYAN}→ Installing native Sovrack Agent binary...${NC}"
  BINARY_URL="https://github.com/sovrack/agent/releases/download/v${VERSION}/sovrack-agent-linux-${GOARCH}"
  if ! curl -fsSL -o /usr/local/bin/sovrack-agent "$BINARY_URL" 2>/dev/null; then
    echo -e "${YELLOW}Warning: Direct download failed, falling back to local binary...${NC}"
  fi
  chmod +x /usr/local/bin/sovrack-agent || true

  echo -e "${CYAN}→ Registering systemd daemon service...${NC}"
  cat <<'EOF' > /etc/systemd/system/sovrack-agent.service
[Unit]
Description=Sovrack Outbound Tunnel Agent
After=network.target docker.service
Wants=docker.service

[Service]
Type=simple
User=root
EnvironmentFile=/etc/sovrack/agent.env
ExecStart=/usr/local/bin/sovrack-agent
Restart=always
RestartSec=5s
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
  systemctl enable --now sovrack-agent
fi

echo -e "${GREEN}✓ Sovrack Agent successfully installed and running!${NC}"
echo -e "${CYAN}The agent is now securely connected via outbound WebSocket to ${SERVER}${NC}"
