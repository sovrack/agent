package tunnel

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sovrack/agent/internal/config"
	"github.com/sovrack/agent/internal/executor"
	"github.com/sovrack/agent/internal/metrics"
)

const SocketIoNamespace = "/agent-tunnel"

type Client struct {
	cfg       *config.Config
	metrics   *metrics.Collector
	executor  *executor.Runner
	conn      *websocket.Conn
	connMu    sync.Mutex
	isClosed  bool
	closeChan chan struct{}
}

func NewClient(cfg *config.Config, m *metrics.Collector, e *executor.Runner) *Client {
	return &Client{
		cfg:       cfg,
		metrics:   m,
		executor:  e,
		closeChan: make(chan struct{}),
	}
}

func (c *Client) Start(ctx context.Context) {
	backoff := 1 * time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			c.Close()
			return
		case <-c.closeChan:
			return
		default:
			log.Printf("[Tunnel] Connecting to Sovrack gateway at %s...", c.cfg.ServerURL)
			err := c.connectAndServe(ctx)
			if err != nil {
				log.Printf("[Tunnel] Connection disconnected: %v. Retrying in %v...", err, backoff)
			}
			time.Sleep(backoff)
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

func (c *Client) buildDialURL() (string, error) {
	raw := c.cfg.ServerURL
	// Replace protocol for URL parsing if needed
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid server URL: %w", err)
	}

	scheme := "ws"
	if u.Scheme == "https" || u.Scheme == "wss" {
		scheme = "wss"
	}

	dialURL := fmt.Sprintf("%s://%s/socket.io/?EIO=4&transport=websocket&token=%s",
		scheme, u.Host, url.QueryEscape(c.cfg.AgentToken))
	return dialURL, nil
}

func (c *Client) connectAndServe(ctx context.Context) error {
	dialURL, err := c.buildDialURL()
	if err != nil {
		return err
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
	}
	if c.cfg.InsecureSkipTLS {
		dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+c.cfg.AgentToken)
	headers.Set("User-Agent", "sovrack-agent/1.0.0")

	conn, resp, err := dialer.DialContext(ctx, dialURL, headers)
	if err != nil {
		if resp != nil {
			return fmt.Errorf("handshake failed with HTTP status %d: %w", resp.StatusCode, err)
		}
		return fmt.Errorf("dial failed: %w", err)
	}

	c.connMu.Lock()
	c.conn = conn
	c.connMu.Unlock()
	defer func() {
		c.connMu.Lock()
		if c.conn != nil {
			c.conn.Close()
			c.conn = nil
		}
		c.connMu.Unlock()
	}()

	log.Printf("[Tunnel] WebSocket connected, initiating Socket.io handshake...")

	// 1. Wait for Engine.IO OPEN packet (type '0')
	_, rawMsg, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("failed to read Engine.IO open packet: %w", err)
	}

	msgStr := string(rawMsg)
	if !strings.HasPrefix(msgStr, "0") {
		return fmt.Errorf("unexpected handshake response: %s", msgStr)
	}

	// 2. Connect to namespace /agent-tunnel: packet "40/agent-tunnel,{"token":"..."}"
	connectPayload, _ := json.Marshal(map[string]string{"token": c.cfg.AgentToken})
	connectPacket := fmt.Sprintf("40%s,%s", SocketIoNamespace, string(connectPayload))
	if err := conn.WriteMessage(websocket.TextMessage, []byte(connectPacket)); err != nil {
		return fmt.Errorf("failed to send namespace connect packet: %w", err)
	}

	// 3. Wait for namespace CONNECT ACK: "40/agent-tunnel,"
	_, rawAck, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("failed to read namespace connect ACK: %w", err)
	}

	ackStr := string(rawAck)
	if !strings.HasPrefix(ackStr, "40"+SocketIoNamespace) && !strings.HasPrefix(ackStr, "42"+SocketIoNamespace) {
		return fmt.Errorf("namespace connect rejected: %s", ackStr)
	}

	log.Printf("[Tunnel] Authenticated and attached to %s namespace successfully!", SocketIoNamespace)

	// 4. Send initial AGENT_HELLO
	if err := c.sendHello(); err != nil {
		return fmt.Errorf("failed to send hello handshake: %w", err)
	}

	// 5. Start heartbeat and metrics loops
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go c.heartbeatLoop(childCtx)
	go c.metricsLoop(childCtx)

	// 6. Process incoming messages loop
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read error: %w", err)
		}

		s := string(raw)
		if s == "2" {
			// Engine.IO Ping -> respond with Pong "3"
			c.connMu.Lock()
			_ = conn.WriteMessage(websocket.TextMessage, []byte("3"))
			c.connMu.Unlock()
			continue
		}
		if s == "3" {
			// Engine.IO Pong ack
			continue
		}

		prefix := "42" + SocketIoNamespace + ","
		if strings.HasPrefix(s, prefix) {
			eventJSON := strings.TrimPrefix(s, prefix)
			go c.handleSocketIoEvent(childCtx, eventJSON)
		} else if strings.HasPrefix(s, "41"+SocketIoNamespace) {
			return fmt.Errorf("namespace disconnected by server")
		}
	}
}

func (c *Client) sendHello() error {
	hostInfo := c.metrics.GetHostInfo()
	payload := map[string]interface{}{
		"version": "1.0.0",
		"host":    hostInfo,
	}
	return c.emitEvent("AGENT_HELLO", payload)
}

func (c *Client) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(c.cfg.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.emitEvent("AGENT_PING", map[string]interface{}{"timestamp": time.Now().UnixMilli()}); err != nil {
				log.Printf("[Tunnel] Heartbeat ping failed: %v", err)
				return
			}
		}
	}
}

func (c *Client) metricsLoop(ctx context.Context) {
	ticker := time.NewTicker(c.cfg.MetricsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			data, err := c.metrics.Collect(ctx)
			if err != nil {
				continue
			}
			if err := c.emitEvent("METRICS_UPDATE", data); err != nil {
				return
			}
		}
	}
}

func (c *Client) handleSocketIoEvent(ctx context.Context, eventJSON string) {
	var rawArray []json.RawMessage
	if err := json.Unmarshal([]byte(eventJSON), &rawArray); err != nil || len(rawArray) < 2 {
		return
	}

	var eventName string
	if err := json.Unmarshal(rawArray[0], &eventName); err != nil {
		return
	}

	switch eventName {
	case "AGENT_PONG":
		// Heartbeat acknowledged

	case "CONNECTED_ACK":
		log.Printf("[Tunnel] Server ACK: authentication confirmed.")

	case "COMMAND_REQUEST":
		var req executor.CommandRequest
		if err := json.Unmarshal(rawArray[1], &req); err != nil {
			log.Printf("[Executor] Invalid command request: %v", err)
			return
		}

		log.Printf("[Executor] Executing command [%s]: %s %v", req.ID, req.Command, req.Args)
		res := c.executor.Execute(ctx, req, nil)

		_ = c.emitEvent("COMMAND_RESULT", res)
	}
}

func (c *Client) emitEvent(eventName string, payload interface{}) error {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.conn == nil {
		return fmt.Errorf("connection not established")
	}

	eventData, err := json.Marshal([]interface{}{eventName, payload})
	if err != nil {
		return err
	}

	packet := fmt.Sprintf("42%s,%s", SocketIoNamespace, string(eventData))
	return c.conn.WriteMessage(websocket.TextMessage, []byte(packet))
}

func (c *Client) Close() {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	if !c.isClosed {
		c.isClosed = true
		close(c.closeChan)
		if c.conn != nil {
			c.conn.Close()
		}
	}
}
