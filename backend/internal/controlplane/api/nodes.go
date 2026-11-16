package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/insmtx/lework/backend/internal/controlplane/authz"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
)

// maxNodeAliasRunes 限制单个别名长度：它会被写进偏好表并跟随每次节点列表
// 响应返回，必须挡住无上限的请求体。
const maxNodeAliasRunes = 64

// nodeRenameRequest 是 PATCH /api/nodes/{id} 的请求体；空名表示清除别名。
type nodeRenameRequest struct {
	Name string `json:"name"`
}

// nodeView 是浏览器看到的节点视图。
//
// 身份字段、LastSeen 与 Connected 全部由持久化记录推导（心跳会写入
// LastSeen）。跨实例部署时节点可能连在别的控制面实例上，因此不能再用
// 本实例注册表判断在线。
//
// Claimed 区分「已归属某个账号」与「还没有成员行的历史节点」：管理员视图
// 同时包含两类节点，界面只有靠它才能决定是否显示「认领」按钮。
type nodeView struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	OS             string    `json:"os"`
	Arch           string    `json:"arch"`
	AgentVersion   string    `json:"agent_version"`
	Capabilities   []string  `json:"capabilities"`
	WorkspaceRoots []string  `json:"workspace_roots"`
	Claimed        bool      `json:"claimed"`
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

// handleClaimNode 把无人归属的历史节点认领到当前管理员账号。
//
// 只对可归属的账号开放：部署级身份（本机直通、共享 enrollment token）本来就
// 看得见所有节点，但没有账号可供写入归属行，因此回 user_required 而不是造一条
// 没有主人的成员行。非管理员在查库之前就被拒绝，且拒绝体固定不随节点是否
// 存在变化——认领接口不能成为节点存在性的探针。
//
// 成功后返回刚认领的节点视图：成员行已经写入，同一个身份紧接着走
// nodeRoute 访问该节点会立即通过。
func (a *API) handleClaimNode(w http.ResponseWriter, r *http.Request) {
	principal := a.principal(r)
	if !principal.IsAdmin() {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden")
		return
	}
	if principal.Kind != authz.KindUser || principal.UserID == "" {
		writeAPIError(w, http.StatusForbidden, "user_required", "claim requires an account")
		return
	}

	nodeID := r.PathValue("id")
	err := a.store.ClaimNodeToUser(r.Context(), nodeID, principal.UserID)
	switch {
	case err == nil:
	case errors.Is(err, store.ErrNodeNotFound):
		writeAPIError(w, http.StatusNotFound, "not_found", "node not found")
		return
	case errors.Is(err, store.ErrNodeAlreadyClaimed):
		writeAPIError(w, http.StatusConflict, "already_claimed", "node already claimed")
		return
	default:
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}

	// 认领已经提交，这里再读一次是为了返回完整的节点视图（名称、在线状态等）。
	// 读不到只可能是并发删除，按内部错误处理而不是假装 404。
	view, ok, err := a.findNodeViewFor(r.Context(), principal.UserID, nodeID)
	if err != nil || !ok {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// handleGetNode 返回单个节点；未登记过的 ID 返回 404。
func (a *API) handleGetNode(w http.ResponseWriter, r *http.Request) {
	view, ok, err := a.findNodeViewFor(r.Context(), a.principal(r).UserID, r.PathValue("id"))
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

// handleRenameNode 设置调用者自己的节点展示名。
//
// 别名存在调用者的偏好命名空间里，不碰 nodes.name：agent 每次 hello 都会用
// 主机名覆盖展示字段，节点级改名无法长期保留，而「每人看到自己的名字」也
// 不应对其他共享成员生效。空名清除别名，回退到 agent 上报的主机名。
func (a *API) handleRenameNode(w http.ResponseWriter, r *http.Request) {
	var req nodeRenameRequest
	if err := decodeRequest(w, r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if utf8.RuneCountInString(name) > maxNodeAliasRunes {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "name is too long")
		return
	}

	principal := a.principal(r)
	nodeID := r.PathValue("id")
	if err := a.store.SetNodeAliasFor(r.Context(), principal.UserID, nodeID, name); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}

	view, ok, err := a.findNodeViewFor(r.Context(), principal.UserID, nodeID)
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

// findNodeViewFor 在 findNode 之上套用某个账号的展示名别名。
//
// 单节点响应、认领响应与改名响应必须和列表视图一致，否则用户会看到「列表里
// 是别名、点开又变回主机名」的分叉。
func (a *API) findNodeViewFor(ctx context.Context, userID, nodeID string) (nodeView, bool, error) {
	view, ok, err := a.findNode(ctx, nodeID)
	if err != nil || !ok {
		return view, ok, err
	}
	alias, ok, err := a.store.GetNodeAliasFor(ctx, userID, nodeID)
	if err != nil {
		return nodeView{}, false, err
	}
	if ok {
		view.Name = alias
	}
	return view, true, nil
}

// applyNodeAliases 把某账号的节点别名写进一批视图；没有别名的节点保留
// agent 上报的主机名。
func (a *API) applyNodeAliases(ctx context.Context, userID string, views []nodeView) error {
	aliases, err := a.store.ListNodeAliasesFor(ctx, userID)
	if err != nil {
		return err
	}
	for i := range views {
		if alias := aliases[views[i].ID]; alias != "" {
			views[i].Name = alias
		}
	}
	return nil
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
		views = append(views, a.nodeViewFromRecord(rec))
	}
	return views, nil
}

// nodeViewFromRecord 把一条持久化记录投影成浏览器视图。
//
// 节点列表、单节点查询与认领确认必须共用同一份字段映射：任何一处手写第二个
// 投影，迟早会出现「列表里有 claimed、认领响应里没有」这类前端无法解释的
// 分叉。
func (a *API) nodeViewFromRecord(rec store.Node) nodeView {
	view := nodeView{
		ID:             rec.ID,
		Name:           rec.Name,
		OS:             rec.OS,
		Arch:           rec.Arch,
		AgentVersion:   rec.AgentVersion,
		Capabilities:   rec.Capabilities,
		WorkspaceRoots: rec.WorkspaceRoots,
		Claimed:        !rec.ClaimedAt.IsZero(),
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
	return view
}
