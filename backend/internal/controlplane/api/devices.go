package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/insmtx/lework/backend/internal/controlplane/store"
)

// deviceUserRequiredMessage 是「部署级身份不能登记设备」的固定文案。
//
// 写成常量并原样回显，是为了让客户端能按 message 提示、也避免不同请求返回
// 不同措辞后无法断言。文案不包含任何身份细节。
const deviceUserRequiredMessage = "device registration requires a user account"

// deviceView 是浏览器看到的设备视图。
//
// 与节点视图一致使用 snake_case，且不暴露任何内部字段；push_token 是
// 客户端自己提交的，回显它是为了让客户端确认注册成功。
type deviceView struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Platform  string    `json:"platform"`
	PushToken string    `json:"push_token"`
	LastSeen  time.Time `json:"last_seen"`
	CreatedAt time.Time `json:"created_at"`
}

// toDeviceView 把存储记录转成浏览器视图。
func toDeviceView(device store.Device) deviceView {
	return deviceView{
		ID:        device.ID,
		Name:      device.Name,
		Platform:  device.Platform,
		PushToken: device.PushToken,
		LastSeen:  device.LastSeen,
		CreatedAt: device.CreatedAt,
	}
}

// registerDeviceRequest 是 POST /api/devices 的请求体。
type registerDeviceRequest struct {
	Name      string `json:"name"`
	Platform  string `json:"platform"`
	PushToken string `json:"push_token"`
}

// handleRegisterDevice 把一台终端登记为当前账号的推送目标。
//
// 同一 push_token 重复注册会被 UpsertDeviceForUser 合并，客户端因此不必在
// 重装后先删除旧记录；换账号登录后重新注册会把令牌改绑到新账号。
//
// local 与 admin_token 没有账号，登记出来的设备永远收不到推送（推送按节点
// 成员筛选），因此直接 403 而不是默默写入一条无人认领的记录。
func (a *API) handleRegisterDevice(w http.ResponseWriter, r *http.Request) {
	principal, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	if principal.UserID == "" {
		writeAPIError(w, http.StatusForbidden, "user_required", deviceUserRequiredMessage)
		return
	}

	var req registerDeviceRequest
	if err := decodeRequest(w, r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.PushToken) == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "name and push_token are required")
		return
	}
	if req.Platform != "android" && req.Platform != "ios" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "platform must be android or ios")
		return
	}

	device, err := a.store.UpsertDeviceForUser(r.Context(), store.Device{
		Name:      strings.TrimSpace(req.Name),
		Platform:  req.Platform,
		PushToken: strings.TrimSpace(req.PushToken),
	}, principal.UserID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "store_error", "failed to store device")
		return
	}
	writeJSON(w, http.StatusCreated, toDeviceView(device))
}

// handleListDevices 返回当前身份可见的推送目标。
//
// 普通账号只看自己的设备；管理员与部署级身份看全部，便于换机或清理遗留
// 记录。设备列表本身不含会话事实，放宽到管理员不会扩大节点数据的可见范围。
func (a *API) handleListDevices(w http.ResponseWriter, r *http.Request) {
	principal, ok := a.requireUser(w, r)
	if !ok {
		return
	}

	var (
		devices []store.Device
		err     error
	)
	if principal.IsAdmin() {
		devices, err = a.store.ListDevices(r.Context())
	} else {
		devices, err = a.store.ListDevicesForUser(r.Context(), principal.UserID)
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "store_error", "failed to list devices")
		return
	}

	views := make([]deviceView, 0, len(devices))
	for _, device := range devices {
		views = append(views, toDeviceView(device))
	}
	writeJSON(w, http.StatusOK, views)
}

// handleDeleteDevice 删除一个推送目标；不存在或不属于当前账号时回 404。
//
// 两种失败共用 404，避免调用方靠状态码判断「这个 id 是否存在，只是不是你的」。
// 管理员（含 local/admin_token）可以删除任意设备，换手机与丢失设备的场景需要它。
func (a *API) handleDeleteDevice(w http.ResponseWriter, r *http.Request) {
	principal, ok := a.requireUser(w, r)
	if !ok {
		return
	}

	userID := ""
	if !principal.IsAdmin() {
		if principal.UserID == "" {
			// 理论上解析不出「非管理员的空账号」，这里仍显式拒绝：把空 userID
			// 传给 DeleteDeviceForUser 会退化成不限制归属的删除。
			writeAPIError(w, http.StatusForbidden, "user_required", deviceUserRequiredMessage)
			return
		}
		userID = principal.UserID
	}

	if err := a.store.DeleteDeviceForUser(r.Context(), r.PathValue("id"), userID); err != nil {
		writeAPIError(w, http.StatusNotFound, "device_not_found", "device not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
