package api

import (
	"net/http"
	"sort"

	"github.com/insmtx/lework/backend/internal/controlplane/overview"
)

// overviewNodeView 是总览里的一个节点：派生会话状态 + 注册表的实时连接信息。
type overviewNodeView struct {
	NodeID    string             `json:"node_id"`
	Name      string             `json:"name"`
	Connected bool               `json:"connected"`
	Sessions  []overview.Session `json:"sessions"`
}

type overviewResponse struct {
	Nodes []overviewNodeView `json:"nodes"`
}

// handleOverview 返回所有节点及其在跑会话。
//
// 节点集合取“索引 ∪ 注册表”：断线节点仍返回，但 connected=false——
// 用户需要知道断线前它在跑什么，而不是看到它凭空消失。
func (a *API) handleOverview(w http.ResponseWriter, r *http.Request) {
	if a.index == nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "overview is not configured")
		return
	}

	views := make(map[string]*overviewNodeView)
	for _, node := range a.index.Snapshot() {
		views[node.NodeID] = &overviewNodeView{
			NodeID:   node.NodeID,
			Name:     node.NodeID,
			Sessions: node.Sessions,
		}
	}

	for _, info := range a.registry.List() {
		view, ok := views[info.ID]
		if !ok {
			view = &overviewNodeView{NodeID: info.ID, Sessions: []overview.Session{}}
			views[info.ID] = view
		}
		view.Name = info.Name
		view.Connected = true
	}

	nodes := make([]overviewNodeView, 0, len(views))
	for _, view := range views {
		if view.Sessions == nil {
			view.Sessions = []overview.Session{}
		}
		nodes = append(nodes, *view)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].NodeID < nodes[j].NodeID })

	writeJSON(w, http.StatusOK, overviewResponse{Nodes: nodes})
}
