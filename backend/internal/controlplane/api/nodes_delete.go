package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/lework/backend/internal/controlplane/authz"
	"github.com/insmtx/lework/backend/internal/controlplane/event"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
	"github.com/insmtx/lework/backend/pkg/messaging"
)

// nodeMemberRevokedEventType 与 node.member.granted 对称：订阅者据此重算
// 可见集合。删除节点后这条事件让它立刻从列表里消失，而不是等下一次刷新。
const nodeMemberRevokedEventType = "node.member.revoked"

// handleDeleteNode 把节点从账号里摘掉，并清理它留下的全部衍生数据。
//
// nodeRoute 只保证「是成员」，这里再要求 owner：member 能使用节点但不能让
// 它从所有人的列表里消失。部署级身份（local / admin_token）没有账号可写
// 归属，同样拒绝——它的管理路径留给后续运维接口。
func (a *API) handleDeleteNode(w http.ResponseWriter, r *http.Request) {
	principal := a.principal(r)
	if principal.Kind != authz.KindUser || principal.UserID == "" {
		writeAPIError(w, http.StatusForbidden, "user_required", "delete requires an account")
		return
	}

	nodeID := r.PathValue("id")
	err := a.store.DeleteNodeForOwner(r.Context(), nodeID, principal.UserID)
	switch {
	case err == nil:
	case errors.Is(err, store.ErrNodeNotFound):
		writeAPIError(w, http.StatusNotFound, "not_found", "node not found")
		return
	case errors.Is(err, store.ErrNodeNotOwner):
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden")
		return
	default:
		logs.Warnf("api: delete node %q: %v", nodeID, err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}

	// 删除已经提交：从这里开始任何失败都不能回错误状态，否则客户端会以为
	// 没删成功而重试，拿到的是 404。
	a.closeNodeConnection(r.Context(), nodeID)
	a.hub.Publish(event.Event{
		NodeID: nodeID,
		Type:   nodeMemberRevokedEventType,
		Time:   time.Now(),
	})
	w.WriteHeader(http.StatusNoContent)
}

// closeNodeConnection 尽力让这台机器立刻掉线。
//
// 本实例持有的连接直接关；其他实例靠 Core NATS 控制消息（见
// messaging.ControlPlaneNodeCloseSubject）关闭。即使消息丢失，凭证已经从
// nodes 表里消失，agent 重连也进不来——「尽力即时、必然失效」。
func (a *API) closeNodeConnection(ctx context.Context, nodeID string) {
	if a.registry != nil {
		if conn, ok := a.registry.Connection(nodeID); ok {
			if err := conn.Close(); err != nil {
				logs.Warnf("api: close node %q connection: %v", nodeID, err)
			}
		}
	}
	if a.core == nil {
		return
	}
	subject, err := messaging.ControlPlaneNodeCloseSubject(nodeID)
	if err != nil {
		logs.Warnf("api: close subject for node %q: %v", nodeID, err)
		return
	}
	pubCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), claimNotificationTimeout)
	defer cancel()
	if err := a.core.PublishCore(pubCtx, subject, []byte(nodeID)); err != nil {
		logs.Warnf("api: publish node close for %q: %v", nodeID, err)
	}
}
