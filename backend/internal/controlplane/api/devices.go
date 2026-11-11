package api

import (
	"net/http"
	"strings"

	"github.com/insmtx/lework/backend/internal/controlplane/store"
)

// registerDeviceRequest 是 POST /api/devices 的请求体。
type registerDeviceRequest struct {
	Name      string `json:"name"`
	Platform  string `json:"platform"`
	PushToken string `json:"push_token"`
}

// handleRegisterDevice 登记一个推送目标。同一 push_token 重复注册会被
// UpsertDevice 合并，客户端因此不必在重装后先删除旧记录。
func (a *API) handleRegisterDevice(w http.ResponseWriter, r *http.Request) {
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

	device, err := a.store.UpsertDevice(r.Context(), store.Device{
		Name:      strings.TrimSpace(req.Name),
		Platform:  req.Platform,
		PushToken: strings.TrimSpace(req.PushToken),
	})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "store_error", "failed to store device")
		return
	}
	writeJSON(w, http.StatusCreated, device)
}

// handleListDevices 返回全部推送目标。
func (a *API) handleListDevices(w http.ResponseWriter, r *http.Request) {
	devices, err := a.store.ListDevices(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "store_error", "failed to list devices")
		return
	}
	writeJSON(w, http.StatusOK, devices)
}

// handleDeleteDevice 删除一个推送目标；不存在时回 404，避免客户端以为撤销成功。
func (a *API) handleDeleteDevice(w http.ResponseWriter, r *http.Request) {
	if err := a.store.DeleteDevice(r.Context(), r.PathValue("id")); err != nil {
		writeAPIError(w, http.StatusNotFound, "device_not_found", "device not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
