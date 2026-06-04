package server

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/insmtx/Leros/backend/internal/worker"
	"github.com/insmtx/Leros/backend/internal/worker/wsproto"
	"github.com/ygpkg/yg-go/logs"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type WorkerManager struct {
	workers   map[string]*WorkerConnection
	mu        sync.RWMutex
	scheduler worker.WorkerScheduler
}

func NewServer(scheduler worker.WorkerScheduler) *WorkerManager {
	return &WorkerManager{
		workers:   make(map[string]*WorkerConnection),
		scheduler: scheduler,
	}
}

func (s *WorkerManager) RegisterRoutes(r gin.IRouter) {
	r.GET("/ws/worker", s.handleWorkerWebSocket)
	r.POST("/ListWorkers", s.listWorkers)
	r.POST("/GetWorkerInfo", s.getWorkerInfo)
	r.POST("/ShutdownWorker", s.shutdownWorker)
	r.POST("/CreateWorker", s.createWorker)

}

func (s *WorkerManager) handleWorkerWebSocket(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logs.Errorf("Failed to upgrade WebSocket: %v", err)
		return
	}
	defer conn.Close()

	ctx := c.Request.Context()

	var workerID string
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			logs.Errorf("Failed to read registration message: %v", err)
			return
		}

		var msg wsproto.WSMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			logs.Errorf("Failed to parse message: %v", err)
			continue
		}

		if msg.Type == wsproto.MsgTypeWorkerRegister {
			var payload wsproto.RegisterPayload
			if err := msg.GetPayload(&payload); err == nil {
				workerID = payload.WorkerID
				break
			}
		}
	}

	w := &WorkerConnection{
		ID:         workerID,
		Conn:       conn,
		Send:       make(chan *wsproto.WSMessage, 256),
		Status:     "active",
		Registered: time.Now(),
		LastSeen:   time.Now(),
	}

	s.mu.Lock()
	s.workers[workerID] = w
	s.mu.Unlock()

	logs.Infof("Worker %s registered", workerID)

	welcomeMsg, err := wsproto.NewPayload(wsproto.MsgTypeWelcome, wsproto.WelcomePayload{
		Message:  "Connected to Leros worker server",
		WorkerID: workerID,
	})
	if err != nil {
		logs.Errorf("Failed to create welcome payload: %v", err)
		return
	}
	if err := w.SendWSMessage(welcomeMsg); err != nil {
		logs.Errorf("Failed to send welcome message: %v", err)
		return
	}

	go s.readPump(w)
	go s.writePump(w)
	go s.heartbeatChecker(w)

	<-ctx.Done()
}

func (s *WorkerManager) readPump(w *WorkerConnection) {
	defer func() {
		s.unregisterWorker(w.ID)
		w.Conn.Close()
	}()

	w.Conn.SetReadLimit(512 * 1024)
	w.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	w.Conn.SetPongHandler(func(string) error {
		w.mu.Lock()
		w.LastSeen = time.Now()
		w.mu.Unlock()
		w.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := w.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logs.Errorf("Worker %s WebSocket error: %v", w.ID, err)
			}
			break
		}

		var msg wsproto.WSMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			logs.Errorf("Failed to unmarshal message from worker %s: %v", w.ID, err)
			continue
		}

		s.handleWorkerMessage(w, &msg)
	}
}

func (s *WorkerManager) writePump(w *WorkerConnection) {
	ticker := time.NewTicker(54 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case message, ok := <-w.Send:
			if !ok {
				w.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w.mu.RLock()
			err := w.SendWSMessage(message)
			w.mu.RUnlock()

			if err != nil {
				logs.Errorf("Failed to write to worker %s: %v", w.ID, err)
				return
			}
		case <-ticker.C:
			w.mu.RLock()
			w.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			err := w.Conn.WriteMessage(websocket.PingMessage, nil)
			w.mu.RUnlock()

			if err != nil {
				return
			}
		}
	}
}

func (s *WorkerManager) handleWorkerMessage(w *WorkerConnection, msg *wsproto.WSMessage) {
	switch msg.Type {
	case wsproto.MsgTypeHeartbeat:
		var payload wsproto.HeartbeatPayload
		if err := msg.GetPayload(&payload); err == nil {
			w.mu.Lock()
			w.LastSeen = time.Unix(payload.Timestamp, 0)
			w.Status = "active"
			w.mu.Unlock()
		}

		ack, err := wsproto.NewPayload(wsproto.MsgTypeHeartbeatAck, wsproto.HeartbeatAckPayload{
			Timestamp: time.Now().Unix(),
		})
		if err == nil {
			select {
			case w.Send <- ack:
			default:
				logs.Warnf("Heartbeat ack dropped for worker %s", w.ID)
			}
		}

	case wsproto.MsgTypeWorkerStatus:
		var payload wsproto.WorkerStatusPayload
		if err := msg.GetPayload(&payload); err == nil {
			w.mu.Lock()
			w.Status = payload.Status
			w.mu.Unlock()
		}

	case wsproto.MsgTypeGetConfig:
		s.handleGetConfig(w, msg)
	}
}

// TODO: worker与server交互时重新实现 handleGetConfig
func (s *WorkerManager) handleGetConfig(w *WorkerConnection, msg *wsproto.WSMessage) {
	logs.Infof("Config request for worker %s - pending implementation", w.ID)
}

func (s *WorkerManager) unregisterWorker(workerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if w, ok := s.workers[workerID]; ok {
		delete(s.workers, workerID)
		close(w.Send)
		logs.Infof("Worker %s unregistered", workerID)
	}
}

func (s *WorkerManager) heartbeatChecker(w *WorkerConnection) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		w.mu.RLock()
		lastSeen := w.LastSeen
		w.mu.RUnlock()

		if time.Since(lastSeen) > 90*time.Second {
			logs.Warnf("Worker %s heartbeat timeout", w.ID)
			s.unregisterWorker(w.ID)
			w.Conn.Close()
			return
		}
	}
}

type ListWorkersResponse struct {
	Workers []WorkerInfo `json:"workers"`
	Total   int          `json:"total"`
}

type WorkerInfo struct {
	ID         string    `json:"id"`
	Status     string    `json:"status"`
	Registered time.Time `json:"registered"`
	LastSeen   time.Time `json:"last_seen"`
}

func (s *WorkerManager) listWorkers(c *gin.Context) {
	var req struct{}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	s.mu.RLock()
	workers := make([]WorkerInfo, 0, len(s.workers))
	for _, w := range s.workers {
		w.mu.RLock()
		workers = append(workers, WorkerInfo{
			ID:         w.ID,
			Status:     w.Status,
			Registered: w.Registered,
			LastSeen:   w.LastSeen,
		})
		w.mu.RUnlock()
	}
	s.mu.RUnlock()

	c.JSON(http.StatusOK, ListWorkersResponse{
		Workers: workers,
		Total:   len(workers),
	})
}

type GetWorkerInfoRequest struct {
	WorkerID string `json:"worker_id" binding:"required"`
}

func (s *WorkerManager) getWorkerInfo(c *gin.Context) {
	var req GetWorkerInfoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	s.mu.RLock()
	w, ok := s.workers[req.WorkerID]
	s.mu.RUnlock()

	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "worker not found"})
		return
	}

	w.mu.RLock()
	info := WorkerInfo{
		ID:         w.ID,
		Status:     w.Status,
		Registered: w.Registered,
		LastSeen:   w.LastSeen,
	}
	w.mu.RUnlock()

	c.JSON(http.StatusOK, info)
}

type ShutdownWorkerRequest struct {
	WorkerID string `json:"worker_id" binding:"required"`
}

func (s *WorkerManager) shutdownWorker(c *gin.Context) {
	var req ShutdownWorkerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	s.mu.RLock()
	w, ok := s.workers[req.WorkerID]
	s.mu.RUnlock()

	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "worker not found"})
		return
	}

	shutdownMsg, err := wsproto.NewPayload(wsproto.MsgTypeShutdown, wsproto.ShutdownPayload{
		Timestamp: time.Now().Unix(),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create shutdown payload"})
		return
	}

	select {
	case w.Send <- shutdownMsg:
		c.JSON(http.StatusOK, gin.H{"message": "shutdown command sent"})
	default:
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "worker send buffer full"})
	}
}

type CreateWorkerRequest struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	EnvType     string            `json:"env_type"`
	Image       string            `json:"image"`
	Command     []string          `json:"command"`
	Args        []string          `json:"args"`
	Env         map[string]string `json:"env"`
	WorkingDir  string            `json:"working_dir"`
}

func (s *WorkerManager) createWorker(c *gin.Context) {
	var req CreateWorkerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if s.scheduler == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "worker scheduler not initialized"})
		return
	}

	spec := &worker.WorkerSpec{
		ID:          req.ID,
		Name:        req.Name,
		Labels:      req.Labels,
		Annotations: req.Annotations,
		EnvType:     worker.WorkerEnvType(req.EnvType),
		Image:       req.Image,
		Command:     req.Command,
		Args:        req.Args,
		Env:         req.Env,
		WorkingDir:  req.WorkingDir,
	}

	spec.EnvType = worker.WorkerEnvProcess

	instance, err := s.scheduler.Start(c.Request.Context(), spec)
	if err != nil {
		logs.Errorf("Failed to create worker: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, instance)
}

