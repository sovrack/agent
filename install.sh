#!/usr/bin/env bash
# ==============================================================================
# Sovrack Agent 1-Liner Automated Installer
# Website: https://sovrack.com | GitHub: https://github.com/sovrack/agent
# ==============================================================================
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
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
    *)
      shift
      ;;
  esac
done

if [ -z "$TOKEN" ]; then
  echo -e "${RED}[ERROR] Missing required --token parameter.${NC}"
  echo "Usage: curl -fsSL https://raw.githubusercontent.com/sovrack/agent/main/install.sh | sudo bash -s -- --token=<YOUR_AGENT_TOKEN> [--server=<URL>]"
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
apt-get update -qq && apt-get install -y -qq curl ca-certificates

echo -e "${CYAN}→ Configuring Sovrack Agent container service...${NC}"
mkdir -p /etc/sovrack

cat <<EOF > /etc/sovrack/agent.env
SOVRACK_SERVER_URL="${SERVER}"
SOVRACK_AGENT_TOKEN="${TOKEN}"
SOVRACK_DOCKER_HOST="unix:///var/run/docker.sock"
EOF
chmod 600 /etc/sovrack/agent.env

# Run via Docker or Systemd
if command -v docker &>/dev/null; then
  echo -e "${CYAN}→ Starting Sovrack Agent via Docker...${NC}"
  docker rm -f sovrack-agent 2>/dev/null || true
  docker run -d \
    --name sovrack-agent \
    --restart always \
    --net host \
    -v /var/run/docker.sock:/var/run/docker.sock \
    --env-file /etc/sovrack/agent.env \
    ghcr.io/sovrack/agent:latest
else
  echo -e "${RED}[WARNING] Docker not detected. Please install Docker first.${NC}"
fi

echo -e "${GREEN}✓ Sovrack Agent successfully installed and running!${NC}"
echo -e "${CYAN}The agent is now securely connected via outbound WebSocket to ${SERVER}${NC}"
