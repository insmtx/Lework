package api

import (
	"net/http"

	"github.com/insmtx/lework/backend/internal/controlplane/overview"
)

type pendingPermissionsResponse struct {
	Permissions []overview.Permission `json:"permissions"`
}

// handlePendingPermissions 返回某会话当前待审批项。
//
// 数据来自控制面的派生索引，只是可丢弃的提示：索引重启后会短暂为空，
// 客户端应把它当作“打开页面时的初始卡片”，回复仍以 permission ID 为准。
func (a *API) handlePendingPermissions(w http.ResponseWriter, r *http.Request) {
	permissions := []overview.Permission{}
	if a.index != nil {
		permissions = a.index.PendingPermissions(r.PathValue("nodeID"), r.PathValue("sessionID"))
	}
	writeJSON(w, http.StatusOK, pendingPermissionsResponse{Permissions: permissions})
}
