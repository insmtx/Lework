package server

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/insmtx/Lework/backend/internal/worker/wsproto"
)

type WorkerConnection struct {
	ID         string
	Conn       *websocket.Conn
	Send       chan *wsproto.WSMessage
	Status     string
	Registered time.Time
	LastSeen   time.Time
	mu         sync.RWMutex
}

func (wc *WorkerConnection) SendWSMessage(msg *wsproto.WSMessage) error {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return wc.Conn.WriteMessage(websocket.TextMessage, data)
}
