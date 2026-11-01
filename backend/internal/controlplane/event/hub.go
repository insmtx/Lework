// Package event 在控制面进程内把节点事件扇出给多个订阅者。
//
// Hub 只做非阻塞转发：慢订阅者会被丢弃而不是拖住发布方，因此节点网关
// 的读循环可以放心地持续 Publish。Event 中的 Data 是不透明负载，Hub
// 不解析也不拼接，机密信息由上游在发布前裁剪。
package event

import (
	"encoding/json"
	"sync"
	"time"
)

// Event 是转发给浏览器订阅者的节点事件。
//
// Data 保持原始 JSON，避免 Hub 为不同事件类型引入业务解析逻辑。
type Event struct {
	NodeID    string          `json:"node_id"`
	SessionID string          `json:"session_id,omitempty"`
	Seq       int64           `json:"seq"`
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data,omitempty"`
	Time      time.Time       `json:"time"`
}

// subscriber 是一个订阅者的发送端与关闭状态。
//
// closeOnce 保证 Publish 淘汰订阅者和调用方取消订阅这两条路径不会
// 重复关闭通道。
type subscriber struct {
	ch        chan Event
	closeOnce sync.Once
}

// close 幂等地关闭订阅通道。
func (s *subscriber) close() {
	s.closeOnce.Do(func() { close(s.ch) })
}

// Hub 是进程内的事件扇出中心。
//
// mu 同时保护订阅表与发送：所有发送都在锁内以非阻塞方式完成，缓冲满时
// 立即删除并关闭该订阅者，因此 Publish 不会等待任何消费者。
type Hub struct {
	mu sync.Mutex

	// nextID 单调递增，保证订阅者 ID 在 Hub 生命周期内唯一。
	nextID uint64
	subs   map[uint64]*subscriber
}

// NewHub 创建空的事件中心。
func NewHub() *Hub {
	return &Hub{subs: make(map[uint64]*subscriber)}
}

// Subscribe 注册订阅者并返回事件通道与取消函数。
//
// buffer 为通道缓冲：0 表示需要接收方就绪才能投递，负数按 0 处理。
// 取消函数幂等，关闭通道并从 Hub 移除订阅者；调用方不应再读取取消后的
// 通道。
func (h *Hub) Subscribe(buffer int) (<-chan Event, func()) {
	if buffer < 0 {
		buffer = 0
	}

	sub := &subscriber{ch: make(chan Event, buffer)}

	h.mu.Lock()
	if h.subs == nil {
		h.subs = make(map[uint64]*subscriber)
	}
	h.nextID++
	id := h.nextID
	h.subs[id] = sub
	h.mu.Unlock()

	cancel := func() {
		h.mu.Lock()
		defer h.mu.Unlock()

		delete(h.subs, id)
		sub.close()
	}
	return sub.ch, cancel
}

// Publish 把事件非阻塞地投递给所有订阅者。
//
// 缓冲已满的订阅者被移除并关闭通道，表现为客户端需要重新订阅；这样单个
// 慢消费者不会阻塞发布方或其他订阅者。
func (h *Hub) Publish(ev Event) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for id, sub := range h.subs {
		select {
		case sub.ch <- ev:
		default:
			// 通道已满：淘汰该订阅者，避免发布路径被慢消费者拖住。
			delete(h.subs, id)
			sub.close()
		}
	}
}
