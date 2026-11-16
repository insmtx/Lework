package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/insmtx/lework/backend/internal/controlplane/authz"
)

// nodeRoute 注册一个节点维度端点，并统一挂上成员授权。
//
// 所有 /api/nodes/{id}/...、/api/sessions/{nodeID}/...、/api/pty/{nodeID}/...
// 都从这里注册，而不是在各自 handler 里重复判定：漏掉一处就是一个能读写别人
// 节点的口子。非成员与不存在的节点返回逐字节相同的 404，节点存在性因此不会
// 从状态码或错误文案里泄漏。
func (a *API) nodeRoute(mux *http.ServeMux, pattern string, h http.HandlerFunc) {
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		nodeID := r.PathValue("id")
		if nodeID == "" {
			nodeID = r.PathValue("nodeID")
		}
		if !a.allowNode(w, r, nodeID) {
			return
		}
		h(w, r)
	})
}

// principal 返回鉴权中间件放进请求的身份；没有经过中间件时是零值。
//
// 零值在 authz.NodeAllowed 里会被拒绝，因此这里不区分「缺身份」与「未知
// 身份」，也不提供一个方便调试的默认管理员。
func (a *API) principal(r *http.Request) authz.Principal {
	p, _ := authz.PrincipalFrom(r.Context())
	return p
}

// allowNode 判定当前身份能否操作 nodeID；拒绝时写 404 并返回 false。
//
// 供 nodeRoute 与「节点 ID 只在 body/query 里」的端点共用，保证两条路径的
// 状态码、错误码与文案完全一致——任意一处写法不同都会变成存在性旁路。
func (a *API) allowNode(w http.ResponseWriter, r *http.Request, nodeID string) bool {
	allowed, err := authz.NodeAllowed(r.Context(), a.store, a.principal(r), nodeID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return false
	}
	if !allowed {
		writeAPIError(w, http.StatusNotFound, "not_found", "node not found")
		return false
	}
	return true
}

// listVisibleNodes 返回当前身份可见的节点视图，保持 listNodes 的顺序。
//
// 与 listNodes 分开是有意的：findNode 与项目聚合仍需看到全部节点（项目过滤
// 是显式延后的一项），而节点列表与总览只投影可见节点。
func (a *API) listVisibleNodes(ctx context.Context, p authz.Principal) ([]nodeView, error) {
	views, err := a.listNodes(ctx)
	if err != nil {
		return nil, err
	}

	visible, err := authz.VisibleNodeIDs(ctx, a.store, p)
	if err != nil {
		return nil, fmt.Errorf("authorize nodes: %w", err)
	}

	filtered := make([]nodeView, 0, len(views))
	for _, view := range views {
		if _, ok := visible[view.ID]; !ok {
			continue
		}
		filtered = append(filtered, view)
	}
	if err := a.applyNodeAliases(ctx, p.UserID, filtered); err != nil {
		return nil, fmt.Errorf("apply node aliases: %w", err)
	}
	return filtered, nil
}
