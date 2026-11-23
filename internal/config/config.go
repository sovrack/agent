package config

import (
	"errors"
	"flag"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ServerURL         string
	AgentToken        string
	DockerHost        string
	HeartbeatInterval time.Duration
	MetricsInterval   time.Duration
	LogLevel          string
	InsecureSkipTLS   bool
}

func Load() (*Config, error) {
	cfg := &Config{
		ServerURL:         getEnv("SOVRACK_SERVER_URL", "ws://localhost:3001/agent-tunnel"),
		AgentToken:        getEnv("SOVRACK_AGENT_TOKEN", ""),
		DockerHost:        getEnv("SOVRACK_DOCKER_HOST", "unix:///var/run/docker.sock"),
		HeartbeatInterval: 15 * time.Second,
		MetricsInterval:   5 * time.Second,
		LogLevel:          strings.ToLower(getEnv("SOVRACK_LOG_LEVEL", "info")),
		InsecureSkipTLS:   getEnvBool("SOVRACK_INSECURE_SKIP_TLS", false),
	}

	// CLI Flags override environment variables
	serverFlag := flag.String("server", "", "Sovrack WebSocket Server URL (e.g. wss://app.sovrack.com/agent-tunnel)")
	tokenFlag := flag.String("token", "", "Sovrack Agent Authentication Token")
	logLevelFlag := flag.String("log-level", "", "Log level: debug, info, warn, error")
	insecureFlag := flag.Bool("insecure", false, "Skip TLS certificate verification")
	flag.Parse()

	if *serverFlag != "" {
		cfg.ServerURL = *serverFlag
	}
	if *tokenFlag != "" {
		cfg.AgentToken = *tokenFlag
	}
	if *logLevelFlag != "" {
		cfg.LogLevel = strings.ToLower(*logLevelFlag)
	}
	if *insecureFlag {
		cfg.InsecureSkipTLS = true
	}

	if cfg.AgentToken == "" {
		return nil, errors.New("agent authentication token is required (set via -token flag or SOVRACK_AGENT_TOKEN env var)")
	}

	// Normalize ServerURL so that http(s):// or raw hostnames are cleanly transformed to ws(s)://.../agent-tunnel
	serverURL := strings.TrimSpace(cfg.ServerURL)
	if strings.HasPrefix(serverURL, "http://") {
		serverURL = "ws://" + strings.TrimPrefix(serverURL, "http://")
	} else if strings.HasPrefix(serverURL, "https://") {
		serverURL = "wss://" + strings.TrimPrefix(serverURL, "https://")
	} else if !strings.HasPrefix(serverURL, "ws://") && !strings.HasPrefix(serverURL, "wss://") {
		serverURL = "wss://" + serverURL
	}
	if !strings.HasSuffix(serverURL, "/agent-tunnel") {
		serverURL = strings.TrimRight(serverURL, "/") + "/agent-tunnel"
	}
	cfg.ServerURL = serverURL

	return cfg, nil
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvBool(key string, defaultVal bool) bool {
	if val := os.Getenv(key); val != "" {
		parsed, err := strconv.ParseBool(val)
		if err == nil {
			return parsed
		}
	}
	return defaultVal
}
