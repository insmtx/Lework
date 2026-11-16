package api

import (
	"net/http"
	"sort"

	"github.com/insmtx/lework/backend/internal/controlplane/authz"
	"github.com/insmtx/lework/backend/internal/controlplane/overview"
)

// overviewNodeView 是总览里的一个节点：派生会话状态 + 存储心跳推导的在线状态。
type overviewNodeView struct {
	NodeID    string             `json:"node_id"`
	Name      string             `json:"name"`
	Connected bool               `json:"connected"`
	Sessions  []overview.Session `json:"sessions"`
}

type overviewResponse struct {
	Nodes []overviewNodeView `json:"nodes"`
}

// handleOverview 返回当前身份可见的节点及其在跑会话。
//
// 节点集合取持久化记录：断线节点仍返回，但 connected=false——用户需要知道
// 断线前它在跑什么，而不是看到它凭空消失。会话来自派生索引，节点集合与
// 在线状态都只认存储里的心跳，避免把连在别的实例上的节点漏掉。
//
// 可见范围与 /api/nodes 共用 authz.VisibleNodeIDs：总览同样不能投影别人的
// 节点名与在线状态。
func (a *API) handleOverview(w http.ResponseWriter, r *http.Request) {
	if a.index == nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "overview is not configured")
		return
	}

	records, err := a.store.ListNodes(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	visible, err := authz.VisibleNodeIDs(r.Context(), a.store, a.principal(r))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	aliases, err := a.store.ListNodeAliasesFor(r.Context(), a.principal(r).UserID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}

	views := make(map[string]*overviewNodeView, len(records))
	for _, rec := range records {
		if _, ok := visible[rec.ID]; !ok {
			continue
		}
		name := rec.Name
		if alias := aliases[rec.ID]; alias != "" {
			name = alias
		}
		views[rec.ID] = &overviewNodeView{
			NodeID:    rec.ID,
			Name:      name,
			Connected: a.nodeConnected(rec),
			Sessions:  []overview.Session{},
		}
	}

	// 索引里可能有已从存储删除的节点留下的会话：节点集合以存储为准，
	// 这类残留直接忽略。
	for _, projected := range a.index.Snapshot() {
		view, ok := views[projected.NodeID]
		if !ok {
			continue
		}
		view.Sessions = projected.Sessions
	}

	// 断线节点的会话状态是断线前的快照，继续报 running 会让用户白等。
	// 这里统一覆盖成 offline，保留 last_activity 供人判断是什么时候停的。
	for _, view := range views {
		if view.Connected {
			continue
		}
		for i := range view.Sessions {
			view.Sessions[i].State = overview.StateOffline
		}
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
