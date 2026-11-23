# Sovrack Agent 📡

**Lightweight Outbound WebSocket Tunnel & Telemetry Agent for Sovrack**

[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8.svg?logo=go)](https://go.dev/)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED.svg?logo=docker)](https://www.docker.com/)

**Sovrack Agent** is an ultra-lightweight (~18MB), high-performance client written in Go that runs on any target Linux server or VPS. It eliminates the need to expose port 22 (SSH) to the public internet by establishing a secure, bi-directional, outbound encrypted WebSocket tunnel (`WSS`) on port 443 directly to the Sovrack Orchestration Server.

---

## ⚡ Why Sovrack Agent?

| Feature | Classic SSH (Agentless) | Sovrack Outbound Agent |
| :--- | :---: | :---: |
| **Inbound Port Required** | **Port 22 (Must be open)** | **NONE (0 Open Ports)** |
| **Works behind NAT / 4G / Corporate Firewalls** | ❌ Blocked | ✅ Works Seamlessly |
| **Encrypted Outbound Tunnel** | ❌ No | ✅ WSS / mTLS over Port 443 |
| **Real-Time Container Telemetry** | Polled via SSH | Real-time streaming (2s intervals) |
| **Resource Footprint** | None | Minimal (< 20MB RAM, < 0.5% CPU) |

---

## 🚀 1-Minute Automated Install (Linux VPS)

Run this one-line command on your target server (Ubuntu, Debian, Fedora, Arch, CentOS):

```bash
curl -fsSL https://raw.githubusercontent.com/sovrack/agent/main/install.sh | sudo bash -s -- --token=<YOUR_SERVER_TOKEN> --server=wss://app.sovrack.com/agent-tunnel
```

---

## 🐳 Docker Run Installation

```bash
docker run -d \
  --name sovrack-agent \
  --restart always \
  --net host \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -e SOVRACK_SERVER_URL="wss://app.sovrack.com/agent-tunnel" \
  -e SOVRACK_AGENT_TOKEN="your-server-agent-token" \
  ghcr.io/sovrack/agent:latest
```

---

## ⚙️ Configuration Variables

| Variable | Default | Description |
| :--- | :--- | :--- |
| `SOVRACK_SERVER_URL` | `wss://app.sovrack.com/agent-tunnel` | Target Sovrack WebSocket Gateway |
| `SOVRACK_AGENT_TOKEN` | *Required* | Server pairing cryptographic token |
| `SOVRACK_DOCKER_HOST` | `unix:///var/run/docker.sock` | Path to Docker UNIX domain socket |
| `SOVRACK_LOG_LEVEL` | `info` | Logging verbosity (`debug`, `info`, `warn`, `error`) |

---

## 📄 License

Distributed under the **Apache License 2.0**. See `LICENSE` for details.
