package api

import (
	"context"
	"fmt"
	"net/http"
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
	views, err := a.listNodes(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}

	id := r.PathValue("id")
	for _, view := range views {
		if view.ID == id {
			writeJSON(w, http.StatusOK, view)
			return
		}
	}
	writeAPIError(w, http.StatusNotFound, "not_found", "node not found")
}

// handleNodeHealth 透传节点 opencode.status 结果。
func (a *API) handleNodeHealth(w http.ResponseWriter, r *http.Request) {
	result, err := a.callAgent(r.Context(), r.PathValue("id"), "opencode.status", nil)
	if err != nil {
		a.writeAgentError(w, err)
		return
	}
	writeRawJSON(w, http.StatusOK, result)
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
		if info, ok := a.registry.Get(rec.ID); ok {
			view.Connected = info.Connected
		}
		views = append(views, view)
	}
	return views, nil
}
