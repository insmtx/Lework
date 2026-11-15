package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"time"
)

// nodeView 是浏览器看到的节点视图。
//
// 身份字段、LastSeen 与 Connected 全部由持久化记录推导（心跳会写入
// LastSeen）。跨实例部署时节点可能连在别的控制面实例上，因此不能再用
// 本实例注册表判断在线。
type nodeView struct {
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

// handleListNodes 返回当前身份可见的节点。
//
// 可见范围由 node_members 决定：普通账号只看自己的节点，管理员账号额外看到
// 「未认领」的历史节点，但任何人（除部署级身份）都看不到别人已认领的节点。
func (a *API) handleListNodes(w http.ResponseWriter, r *http.Request) {
	views, err := a.listVisibleNodes(r.Context(), a.principal(r))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, views)
}

// handleGetNode 返回单个节点；未登记过的 ID 返回 404。
func (a *API) handleGetNode(w http.ResponseWriter, r *http.Request) {
	view, ok, err := a.findNode(r.Context(), r.PathValue("id"))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	if !ok {
		writeAPIError(w, http.StatusNotFound, "not_found", "node not found")
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// handleNodeHealth 透传节点 opencode.status 结果。
//
// 未登记过的节点返回 404，与 GET /api/nodes/{id} 一致；已登记但当前离线
// 仍由 callAgent 映射为 409 node_offline。
func (a *API) handleNodeHealth(w http.ResponseWriter, r *http.Request) {
	if _, ok, err := a.findNode(r.Context(), r.PathValue("id")); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	} else if !ok {
		writeAPIError(w, http.StatusNotFound, "not_found", "node not found")
		return
	}

	result, err := a.callAgent(r.Context(), r.PathValue("id"), "opencode.status", nil)
	if err != nil {
		a.writeAgentError(w, err)
		return
	}
	writeRawJSON(w, http.StatusOK, result)
}

func (a *API) handleOpenCodeStart(w http.ResponseWriter, r *http.Request) {
	a.handleOpenCodeLifecycle(w, r, "opencode.start")
}

func (a *API) handleOpenCodeRestart(w http.ResponseWriter, r *http.Request) {
	a.handleOpenCodeLifecycle(w, r, "opencode.restart")
}

func (a *API) handleOpenCodeStop(w http.ResponseWriter, r *http.Request) {
	a.handleOpenCodeLifecycle(w, r, "opencode.stop")
}

// handleOpenCodeLifecycle 转发进程启停；旧 agent 不上报 capabilities 时按
// 支持处理，调用失败再让浏览器提示升级，避免按钮被永久隐藏。
func (a *API) handleOpenCodeLifecycle(w http.ResponseWriter, r *http.Request, method string) {
	nodeID := r.PathValue("id")
	view, ok, err := a.findNode(r.Context(), nodeID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	if !ok {
		writeAPIError(w, http.StatusNotFound, "not_found", "node not found")
		return
	}
	if len(view.Capabilities) > 0 && !slices.Contains(view.Capabilities, method) {
		writeAPIError(w, http.StatusConflict, "capability_unavailable",
			"node agent does not support "+method)
		return
	}

	result, err := a.callAgent(r.Context(), nodeID, method, nil)
	if err != nil {
		a.writeAgentError(w, err)
		return
	}
	writeRawJSON(w, http.StatusOK, result)
}

// findNode 返回 nodeID 对应的节点视图；未登记时第二个返回值为 false。
func (a *API) findNode(ctx context.Context, nodeID string) (nodeView, bool, error) {
	views, err := a.listNodes(ctx)
	if err != nil {
		return nodeView{}, false, err
	}
	for _, view := range views {
		if view.ID == nodeID {
			return view, true, nil
		}
	}
	return nodeView{}, false, nil
}

// listNodes 把持久化记录投影成节点视图。存储是身份、LastSeen 与在线状态的
// 唯一来源：心跳在任一实例上收到都会刷新 LastSeen，因此这里对集群一视同仁。
func (a *API) listNodes(ctx context.Context) ([]nodeView, error) {
	records, err := a.store.ListNodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}

	views := make([]nodeView, 0, len(records))
	for _, rec := range records {
		view := nodeView{
			ID:             rec.ID,
			Name:           rec.Name,
			OS:             rec.OS,
			Arch:           rec.Arch,
			AgentVersion:   rec.AgentVersion,
			Capabilities:   rec.Capabilities,
			WorkspaceRoots: rec.WorkspaceRoots,
			Connected:      a.nodeConnected(rec),
			LastSeen:       rec.LastSeen,
		}
		// hello 不带 capabilities 时存储里是 nil，直接序列化会得到 null；
		// 浏览器端需要能安全地遍历，因此统一成空数组。
		if view.Capabilities == nil {
			view.Capabilities = []string{}
		}
		if view.WorkspaceRoots == nil {
			view.WorkspaceRoots = []string{}
		}
		views = append(views, view)
	}
	return views, nil
}
