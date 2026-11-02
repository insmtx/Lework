package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/insmtx/Lework/backend/internal/api/connectors"
	eventbus "github.com/insmtx/Lework/backend/internal/infra/mq"
	"github.com/ygpkg/yg-go/logs"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// 确保 Connector 实现了 connectors.Connector 接口
var _ connectors.Connector = (*Connector)(nil)

// Connection represents a single WebSocket connection
type Connection struct {
	conn     *websocket.Conn
	send     chan ServerMessage
	clientID string
}

// Connector handles client WebSocket connections
type Connector struct {
	publisher   eventbus.Publisher
	connections map[*Connection]bool
	broadcast   chan ServerMessage
	register    chan *Connection
	unregister  chan *Connection
	mu          sync.RWMutex
}

// Messenger provides interface to send messages to clients
type Messenger struct {
	connector *Connector
}

func NewConnector(publisher eventbus.Publisher) *Connector {
	connector := &Connector{
		publisher:   publisher,
		connections: make(map[*Connection]bool),
		broadcast:   make(chan ServerMessage),
		register:    make(chan *Connection),
		unregister:  make(chan *Connection),
	}

	go connector.run()
	return connector
}

func (c *Connector) ChannelCode() string {
	return ChannelCodeValue
}

func (c *Connector) RegisterRoutes(r gin.IRouter) {
	r.GET("/ws/client", c.handleWebSocket)
	r.GET("/api/client/status", c.getClientStatus)
}

// RegisterWebSocketRoutes 注册WebSocket路由(便捷函数)
func RegisterWebSocketRoutes(r gin.IRouter, publisher eventbus.Publisher) {
	connector := NewConnector(publisher)
	connector.RegisterRoutes(r)
	GetManager().SetConnector(connector)
}

func (c *Connector) handleWebSocket(ctx *gin.Context) {
	conn, err := upgrader.Upgrade(ctx.Writer, ctx.Request, nil)
	if err != nil {
		logs.ErrorContextf(ctx, "Failed to upgrade connection to WebSocket: %v", err)
		return
	}
	defer conn.Close()

	clientID := ctx.Query("client_id")
	if clientID == "" {
		clientID = fmt.Sprintf("client_%d", time.Now().Unix())
	}

	connection := &Connection{
		conn:     conn,
		send:     make(chan ServerMessage, 256),
		clientID: clientID,
	}

	c.register <- connection

	go c.readPump(connection)
	go c.writePump(connection)
}

func (c *Connector) readPump(conn *Connection) {
	defer func() {
		c.unregister <- conn
		conn.conn.Close()
	}()

	conn.conn.SetReadLimit(512 * 1024)
	conn.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.conn.SetPongHandler(func(string) error {
		conn.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := conn.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logs.Errorf("WebSocket error for client %s: %v", conn.clientID, err)
			}
			break
		}

		var clientMsg Message
		if err := json.Unmarshal(message, &clientMsg); err != nil {
			logs.Errorf("Failed to unmarshal client message: %v", err)
			continue
		}

		switch clientMsg.Type {
		case "ping":
			response := ServerMessage{
				Type:      "pong",
				Payload:   map[string]interface{}{"message": "pong"},
				ID:        clientMsg.ID,
				Timestamp: time.Now(),
			}
			select {
			case conn.send <- response:
			default:
				logs.Warnf("Dropping message for client %s due to full send buffer", conn.clientID)
			}
		case "user_command":
			err := c.forwardUserCommand(clientMsg, conn.clientID)
			if err != nil {
				logs.Errorf("Failed to forward user command: %v", err)

				errorResponse := ServerMessage{
					Type:      "error",
					Payload:   map[string]interface{}{"error": err.Error()},
					ID:        clientMsg.ID,
					Timestamp: time.Now(),
				}
				select {
				case conn.send <- errorResponse:
				default:
					logs.Warnf("Dropping message for client %s due to full send buffer", conn.clientID)
				}
			}
		case "subscribe_to_agent":
			taskID, ok := clientMsg.Payload["task_id"].(string)
			if !ok {
				logs.Warn("Missing task_id in subscribe_to_agent message")
				continue
			}
			c.subscribeToAgentActivity(taskID, conn)
		default:
			logs.Warnf("Unknown client message type: %s", clientMsg.Type)
		}
	}
}

func (c *Connector) writePump(conn *Connection) {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		conn.conn.Close()
	}()

	for {
		select {
		case message, ok := <-conn.send:
			conn.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				conn.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := conn.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			messageBytes, err := json.Marshal(message)
			if err != nil {
				logs.Errorf("Failed to marshal server message: %v", err)
				return
			}
			if _, err := w.Write(messageBytes); err != nil {
				logs.Errorf("Failed to write message: %v", err)
				return
			}

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			conn.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Connector) run() {
	for {
		select {
		case conn := <-c.register:
			c.mu.Lock()
			c.connections[conn] = true
			c.mu.Unlock()

			welcomeMsg := ServerMessage{
				Type:      "welcome",
				Payload:   map[string]interface{}{"client_id": conn.clientID, "message": "Connected to Lework client service"},
				ID:        "",
				Timestamp: time.Now(),
			}
			select {
			case conn.send <- welcomeMsg:
			default:
				logs.Warnf("Failed to send welcome message to client %s", conn.clientID)
				close(conn.send)
			}

			logs.Infof("New client connected: %s (total: %d)", conn.clientID, len(c.connections))
		case conn := <-c.unregister:
			c.mu.Lock()
			if _, ok := c.connections[conn]; ok {
				delete(c.connections, conn)
				close(conn.send)
				logs.Infof("Client disconnected: %s (total: %d)", conn.clientID, len(c.connections))
			}
			c.mu.Unlock()
		case message := <-c.broadcast:
			c.mu.RLock()
			for conn := range c.connections {
				select {
				case conn.send <- message:
				default:
					logs.Debugf("Removing client %s due to send buffer overflow", conn.clientID)
					go func(connToUnreg *Connection) {
						c.unregister <- connToUnreg
					}(conn)
				}
			}
			c.mu.RUnlock()
		}
	}
}

func (c *Connector) forwardUserCommand(msg Message, clientID string) error {
	event := map[string]interface{}{
		"type":       "user_command",
		"client_id":  clientID,
		"payload":    msg.Payload,
		"message_id": msg.ID,
		"timestamp":  time.Now().Unix(),
	}

	err := c.publisher.Publish(context.Background(), "user.commands", event)
	if err != nil {
		return fmt.Errorf("failed to publish user command: %w", err)
	}

	return nil
}

func (c *Connector) subscribeToAgentActivity(taskID string, conn *Connection) {
	logs.Infof("Client %s subscribed to agent activity for task: %s", conn.clientID, taskID)
}

func (c *Connector) getClientStatus(ctx *gin.Context) {
	c.mu.RLock()
	status := map[string]interface{}{
		"connected_clients": len(c.connections),
		"status":            "active",
		"timestamp":         time.Now().Unix(),
	}
	c.mu.RUnlock()

	ctx.JSON(http.StatusOK, status)
}

func (c *Connector) GetAllClientIDs() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	ids := make([]string, 0, len(c.connections))
	for conn := range c.connections {
		ids = append(ids, conn.clientID)
	}
	return ids
}

func (c *Connector) SendMessageToClient(clientID string, message ServerMessage) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for conn := range c.connections {
		if conn.clientID == clientID {
			select {
			case conn.send <- message:
				return true
			default:
				logs.Warnf("Failed to send message to client %s: send buffer full", clientID)
				return false
			}
		}
	}

	logs.Warnf("Client with ID %s not found", clientID)
	return false
}

func (c *Connector) BroadcastSend(message ServerMessage) {
	select {
	case c.broadcast <- message:
	default:
		logs.Warn("Broadcast message dropped due to full broadcast channel")
	}
}

func (c *Connector) GetMessenger() *Messenger {
	return &Messenger{connector: c}
}

func (m *Messenger) SendMessage(dest MessageDestination, msgType string, payload map[string]interface{}) error {
	message := ServerMessage{
		Type:      msgType,
		Payload:   payload,
		Timestamp: time.Now(),
	}

	if dest.ClientID != "" {
		success := m.connector.SendMessageToClient(dest.ClientID, message)
		if !success {
			return fmt.Errorf("failed to send message to client %s", dest.ClientID)
		}
	} else {
		m.connector.BroadcastSend(message)
	}

	return nil
}

func (m *Messenger) SendAgentStatusUpdate(clientID, taskID, status, message string) error {
	payload := map[string]interface{}{
		"task_id":   taskID,
		"status":    status,
		"message":   message,
		"timestamp": time.Now().Unix(),
	}

	dest := MessageDestination{ClientID: clientID}
	return m.SendMessage(dest, "agent_status_update", payload)
}

func (m *Messenger) SendAgentStepUpdate(clientID, taskID, step, details string) error {
	payload := map[string]interface{}{
		"task_id":   taskID,
		"step":      step,
		"details":   details,
		"timestamp": time.Now().Unix(),
	}

	dest := MessageDestination{ClientID: clientID}
	return m.SendMessage(dest, "agent_step_update", payload)
}

func (m *Messenger) SendAgentResult(clientID, taskID, resultType, result string) error {
	payload := map[string]interface{}{
		"task_id":     taskID,
		"result_type": resultType,
		"result":      result,
		"timestamp":   time.Now().Unix(),
	}

	dest := MessageDestination{ClientID: clientID}
	return m.SendMessage(dest, "agent_result", payload)
}

func (m *Messenger) SendLogMessage(clientID, taskID, logLevel, message string) error {
	payload := map[string]interface{}{
		"task_id":   taskID,
		"log_level": logLevel,
		"message":   message,
		"timestamp": time.Now().Unix(),
	}

	dest := MessageDestination{ClientID: clientID}
	return m.SendMessage(dest, "agent_log", payload)
}
