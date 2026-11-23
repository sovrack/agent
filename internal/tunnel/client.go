package tunnel

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sovrack/agent/internal/config"
	"github.com/sovrack/agent/internal/executor"
	"github.com/sovrack/agent/internal/metrics"
)

type MessageType string

const (
	TypeAgentHello     MessageType = "AGENT_HELLO"
	TypeAgentPing      MessageType = "AGENT_PING"
	TypeAgentPong      MessageType = "AGENT_PONG"
	TypeMetricsUpdate  MessageType = "METRICS_UPDATE"
	TypeCommandRequest MessageType = "COMMAND_REQUEST"
	TypeCommandOutput  MessageType = "COMMAND_OUTPUT"
	TypeCommandResult  MessageType = "COMMAND_RESULT"
	TypeWriteFile      MessageType = "WRITE_FILE"
	TypeWriteFileAck   MessageType = "WRITE_FILE_ACK"
)

type TunnelMessage struct {
	Type      MessageType     `json:"type"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Timestamp int64           `json:"timestamp"`
}

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

func (c *Client) connectAndServe(ctx context.Context) error {
	dialer := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
	}
	if c.cfg.InsecureSkipTLS {
		dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+c.cfg.AgentToken)
	headers.Set("User-Agent", "sovrack-agent/3.1.0")

	conn, resp, err := dialer.DialContext(ctx, c.cfg.ServerURL, headers)
	if err != nil {
		if resp != nil {
			return fmt.Errorf("handshake failed with status %d: %w", resp.StatusCode, err)
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

	log.Printf("[Tunnel] Connected successfully to Sovrack gateway!")

	// 1. Send AGENT_HELLO
	if err := c.sendHello(); err != nil {
		return fmt.Errorf("failed to send hello handshake: %w", err)
	}

	// 2. Start heartbeat and metrics loops
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go c.heartbeatLoop(childCtx)
	go c.metricsLoop(childCtx)

	// 3. Process incoming messages
	for {
		_, rawMsg, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read error: %w", err)
		}

		var msg TunnelMessage
		if err := json.Unmarshal(rawMsg, &msg); err != nil {
			log.Printf("[Tunnel] Invalid message payload: %v", err)
			continue
		}

		go c.handleMessage(childCtx, msg)
	}
}

func (c *Client) sendHello() error {
	hostInfo := c.metrics.GetHostInfo()
	payload, err := json.Marshal(map[string]interface{}{
		"version":  "3.1.0",
		"host":     hostInfo,
		"protocol": "sovrack-agent-v1",
	})
	if err != nil {
		return err
	}

	return c.sendMessage(TypeAgentHello, payload)
}

func (c *Client) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(c.cfg.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.sendMessage(TypeAgentPing, nil); err != nil {
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
			payload, err := json.Marshal(data)
			if err != nil {
				continue
			}
			if err := c.sendMessage(TypeMetricsUpdate, payload); err != nil {
				return
			}
		}
	}
}

func (c *Client) handleMessage(ctx context.Context, msg TunnelMessage) {
	switch msg.Type {
	case TypeAgentPing:
		_ = c.sendMessage(TypeAgentPong, nil)

	case TypeAgentPong:
		// Heartbeat ack received

	case TypeCommandRequest:
		var req executor.CommandRequest
		if err := json.Unmarshal(msg.Payload, &req); err != nil {
			log.Printf("[Executor] Invalid command request: %v", err)
			return
		}

		log.Printf("[Executor] Executing command [%s]: %s", req.ID, req.Command)
		res := c.executor.Execute(ctx, req, func(chunk string) {
			outputPayload, _ := json.Marshal(map[string]interface{}{
				"id":    req.ID,
				"chunk": chunk,
			})
			_ = c.sendMessage(TypeCommandOutput, outputPayload)
		})

		resPayload, _ := json.Marshal(res)
		_ = c.sendMessage(TypeCommandResult, resPayload)

	case TypeWriteFile:
		var writeReq executor.WriteFileRequest
		if err := json.Unmarshal(msg.Payload, &writeReq); err != nil {
			log.Printf("[Executor] Invalid write file request: %v", err)
			return
		}

		err := c.executor.WriteFile(writeReq)
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}

		ackPayload, _ := json.Marshal(map[string]string{
			"path":   writeReq.Path,
			"status": status,
			"error":  errMsg,
		})
		_ = c.sendMessage(TypeWriteFileAck, ackPayload)
	}
}

func (c *Client) sendMessage(msgType MessageType, payload []byte) error {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.conn == nil {
		return fmt.Errorf("connection not established")
	}

	msg := TunnelMessage{
		Type:      msgType,
		Payload:   payload,
		Timestamp: time.Now().Unix(),
	}

	bytes, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	return c.conn.WriteMessage(websocket.TextMessage, bytes)
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
