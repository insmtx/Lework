package node

import (
	"cmp"
	"context"
	"encoding/json"
	"slices"
	"sync"
	"time"
)

// Info 是节点在控制面内存中的实时视图。Capabilities 描述节点能处理的
// 方法集合，Connected 只反映当前是否有活跃连接。
type Info struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	OS             string    `json:"os"`
	Arch           string    `json:"arch"`
	AgentVersion   string    `json:"agent_version"`
	Capabilities   []string  `json:"capabilities"`
	WorkspaceRoots []string  `json:"workspace_roots"`
	Connected      bool      `json:"connected"`
	LastSeen       time.Time `json:"last_seen"`
}

// Registry 跟踪每个节点当前活跃的连接。
//
// 读写锁只保护两张映射表；请求等待在锁外进行，避免慢节点阻塞其它节点的
// 注册、查询与请求。Info 与连接一一对应，Unregister 时一并移除。
type Registry struct {
	mu    sync.RWMutex
	infos map[string]Info
	conns map[string]*Connection
}

// NewRegistry 创建空的注册表。
func NewRegistry() *Registry {
	return &Registry{
		infos: make(map[string]Info),
		conns: make(map[string]*Connection),
	}
}

// Register 登记节点连接，覆盖同一节点 ID 的旧连接。
//
// 同一节点重连时旧连接必须失效，否则请求可能被路由到已关闭的写入端；
// 因此替换时关闭旧连接。重复注册同一个连接不会关闭它自身。
func (r *Registry) Register(info Info, conn *Connection) {
	if conn == nil {
		return
	}
	if info.ID == "" {
		info.ID = conn.NodeID()
	}
	info.Connected = true
	if info.LastSeen.IsZero() {
		info.LastSeen = time.Now()
	}
	// 复制能力切片，防止调用方后续修改影响注册表。
	info.Capabilities = slices.Clone(info.Capabilities)

	r.mu.Lock()
	previous := r.conns[info.ID]
	r.conns[info.ID] = conn
	r.infos[info.ID] = info
	r.mu.Unlock()

	// 关闭旧连接放在锁外：Close 会回调传输层，不应阻塞其它节点的操作。
	if previous != nil && previous != conn {
		_ = previous.Close()
	}
}

// Unregister 移除节点连接，只有仍是当前连接时才删除。
//
// 节点重连后旧连接的退出回调会晚于新连接注册，按指针比较可以避免把
// 新连接误删。
func (r *Registry) Unregister(nodeID string, conn *Connection) {
	r.mu.Lock()
	defer r.mu.Unlock()

	current, ok := r.conns[nodeID]
	if !ok || current != conn {
		return
	}
	delete(r.conns, nodeID)
	delete(r.infos, nodeID)
}

// Get 返回节点快照；第二个返回值表示节点当前是否有活跃连接。
func (r *Registry) Get(nodeID string) (Info, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	info, ok := r.infos[nodeID]
	if !ok {
		return Info{}, false
	}
	return cloneInfo(info), true
}

// List 返回按 ID 升序排列的节点快照。返回值与注册表内部状态完全隔离，
// 调用方可以随意修改。
func (r *Registry) List() []Info {
	r.mu.RLock()
	defer r.mu.RUnlock()

	list := make([]Info, 0, len(r.infos))
	for _, info := range r.infos {
		list = append(list, cloneInfo(info))
	}
	slices.SortFunc(list, func(a, b Info) int { return cmp.Compare(a.ID, b.ID) })
	return list
}

// Request 向指定节点发起方法调用。节点没有活跃连接时返回 ErrNodeOffline。
func (r *Registry) Request(ctx context.Context, nodeID, method string, params json.RawMessage) (json.RawMessage, error) {
	r.mu.RLock()
	conn := r.conns[nodeID]
	r.mu.RUnlock()

	if conn == nil {
		return nil, ErrNodeOffline
	}
	// 请求等待不持有注册表锁，慢节点不会拖住其它节点。
	return conn.Request(ctx, method, params)
}

// CloseAll 关闭全部连接并清空注册表，用于控制面退出。
func (r *Registry) CloseAll() {
	r.mu.Lock()
	conns := make([]*Connection, 0, len(r.conns))
	for _, conn := range r.conns {
		conns = append(conns, conn)
	}
	r.infos = make(map[string]Info)
	r.conns = make(map[string]*Connection)
	r.mu.Unlock()

	for _, conn := range conns {
		_ = conn.Close()
	}
}

// cloneInfo 复制 Info 及其能力切片，避免快照与内部状态共享底层数组。
func cloneInfo(info Info) Info {
	info.Capabilities = slices.Clone(info.Capabilities)
	info.WorkspaceRoots = slices.Clone(info.WorkspaceRoots)
	return info
}
