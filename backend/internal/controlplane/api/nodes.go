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
// 身份字段与 LastSeen 以持久化存储为准（心跳会写入 LastSeen），
// Connected 则取实时注册表，避免把已断开节点显示为在线。
type nodeView struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	OS           string    `json:"os"`
	Arch         string    `json:"arch"`
	AgentVersion string    `json:"agent_version"`
	Capabilities []string  `json:"capabilities"`
	Connected    bool      `json:"connected"`
	LastSeen     time.Time `json:"last_seen"`
}

// handleListNodes 返回全部已登记节点。
func (a *API) handleListNodes(w http.ResponseWriter, r *http.Request) {
	views, err := a.listNodes(r.Context())
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

// listNodes 合并持久化记录与实时连接状态。持久化记录是身份与 LastSeen
// 的唯一来源，注册表只补充 Connected。
func (a *API) listNodes(ctx context.Context) ([]nodeView, error) {
	records, err := a.store.ListNodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}

	views := make([]nodeView, 0, len(records))
	for _, rec := range records {
		view := nodeView{
			ID:           rec.ID,
			Name:         rec.Name,
			OS:           rec.OS,
			Arch:         rec.Arch,
			AgentVersion: rec.AgentVersion,
			Capabilities: rec.Capabilities,
			LastSeen:     rec.LastSeen,
		}
		// hello 不带 capabilities 时存储里是 nil，直接序列化会得到 null；
		// 浏览器端需要能安全地遍历，因此统一成空数组。
		if view.Capabilities == nil {
			view.Capabilities = []string{}
		}
		if info, ok := a.registry.Get(rec.ID); ok {
			view.Connected = info.Connected
		}
		views = append(views, view)
	}
	return views, nil
}
