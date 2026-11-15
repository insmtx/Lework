package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/insmtx/lework/backend/internal/controlplane/authz"
)

func TestRegisterDevice(t *testing.T) {
	env := newTestEnv(t, time.Second)
	rec := doJSONAs(t, env, "u1", http.MethodPost, "/api/devices",
		`{"name":"pixel","platform":"android","push_token":"tok-a"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}

	var device deviceView
	if err := json.Unmarshal(rec.Body.Bytes(), &device); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if device.ID == "" || device.Name != "pixel" || device.PushToken != "tok-a" {
		t.Fatalf("device = %+v", device)
	}
	if !strings.Contains(rec.Body.String(), `"push_token":"tok-a"`) {
		t.Fatalf("body = %s, want snake_case push_token", rec.Body.String())
	}
}

// TestRegisterDeviceStoresOwner 固化设备归属：注册请求里的身份就是设备主人，
// 推送据此判断「这台手机是不是该节点的成员」。
func TestRegisterDeviceStoresOwner(t *testing.T) {
	env := newTestEnv(t, time.Second)
	rec := doJSONAs(t, env, "u1", http.MethodPost, "/api/devices",
		`{"name":"pixel","platform":"android","push_token":"tok-a"}`)
	requireStatus(t, rec, http.StatusCreated)

	device := decodeBody[deviceView](t, rec)
	stored, err := env.store.ListDevicesForUser(context.Background(), "u1")
	if err != nil {
		t.Fatalf("ListDevicesForUser: %v", err)
	}
	if len(stored) != 1 || stored[0].ID != device.ID || stored[0].UserID != "u1" {
		t.Fatalf("stored devices = %+v, want the row owned by u1", stored)
	}
}

// TestRegisterDeviceRequiresAccount 证明部署级身份不能登记设备：local 与
// admin_token 没有账号，造出来的设备永远收不到推送。
func TestRegisterDeviceRequiresAccount(t *testing.T) {
	env := newTestEnv(t, time.Second)
	for _, principal := range []authz.Principal{
		{Kind: authz.KindLocal, Role: roleAdmin},
		{Kind: authz.KindAdminToken},
	} {
		rec := doJSONAsRole(t, env, principal, http.MethodPost, "/api/devices",
			`{"name":"pixel","platform":"android","push_token":"tok-a"}`)
		requireErrorCode(t, rec, http.StatusForbidden, "user_required")

		var body apiError
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode error body: %v", err)
		}
		if body.Message != deviceUserRequiredMessage {
			t.Fatalf("message = %q, want the fixed %q", body.Message, deviceUserRequiredMessage)
		}
	}

	devices, err := env.store.ListDevices(context.Background())
	if err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	if len(devices) != 0 {
		t.Fatalf("devices = %+v, want none after rejected registrations", devices)
	}
}

func TestRegisterDeviceIsIdempotentPerToken(t *testing.T) {
	env := newTestEnv(t, time.Second)
	doJSONAs(t, env, "u1", http.MethodPost, "/api/devices",
		`{"name":"pixel","platform":"android","push_token":"tok-a"}`)
	rec := doJSONAs(t, env, "u1", http.MethodPost, "/api/devices",
		`{"name":"pixel-2","platform":"android","push_token":"tok-a"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}

	list := doJSONAs(t, env, "u1", http.MethodGet, "/api/devices", "")
	var devices []deviceView
	if err := json.Unmarshal(list.Body.Bytes(), &devices); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(devices) != 1 || devices[0].Name != "pixel-2" {
		t.Fatalf("devices = %+v, want one updated row", devices)
	}
}

func TestRegisterDeviceRejectsMissingFields(t *testing.T) {
	env := newTestEnv(t, time.Second)
	for _, body := range []string{
		`{"platform":"android","push_token":"t"}`,
		`{"name":"pixel","push_token":"t"}`,
		`{"name":"pixel","platform":"android"}`,
	} {
		rec := doJSONAs(t, env, "u1", http.MethodPost, "/api/devices", body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %s: status = %d, want 400", body, rec.Code)
		}
	}
}

// TestListDevicesScopedToCaller 固化列表口径：普通账号只看到自己的设备，
// 管理员（含 local/admin_token）才看到全部。
func TestListDevicesScopedToCaller(t *testing.T) {
	env := newTestEnv(t, time.Second)
	doJSONAs(t, env, "u1", http.MethodPost, "/api/devices",
		`{"name":"pixel","platform":"android","push_token":"tok-a"}`)
	doJSONAs(t, env, "u2", http.MethodPost, "/api/devices",
		`{"name":"iphone","platform":"ios","push_token":"tok-b"}`)

	own := decodeBody[[]deviceView](t, doJSONAs(t, env, "u1", http.MethodGet, "/api/devices", ""))
	if len(own) != 1 || own[0].PushToken != "tok-a" {
		t.Fatalf("devices of u1 = %+v, want only tok-a", own)
	}

	other := decodeBody[[]deviceView](t, doJSONAs(t, env, "u2", http.MethodGet, "/api/devices", ""))
	if len(other) != 1 || other[0].PushToken != "tok-b" {
		t.Fatalf("devices of u2 = %+v, want only tok-b", other)
	}

	admin := decodeBody[[]deviceView](t, doJSONAsRole(t, env,
		authz.Principal{Kind: authz.KindUser, UserID: "admin-1", Role: roleAdmin},
		http.MethodGet, "/api/devices", ""))
	if len(admin) != 2 {
		t.Fatalf("admin devices = %+v, want both", admin)
	}

	local := decodeBody[[]deviceView](t, env.do(http.MethodGet, "/api/devices", ""))
	if len(local) != 2 {
		t.Fatalf("local devices = %+v, want both", local)
	}
}

func TestListAndDeleteDevices(t *testing.T) {
	env := newTestEnv(t, time.Second)
	created := doJSONAs(t, env, "u1", http.MethodPost, "/api/devices",
		`{"name":"pixel","platform":"android","push_token":"tok-a"}`)
	var device deviceView
	if err := json.Unmarshal(created.Body.Bytes(), &device); err != nil {
		t.Fatalf("decode: %v", err)
	}

	rec := doJSONAs(t, env, "u1", http.MethodGet, "/api/devices", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "tok-a") {
		t.Fatalf("list status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = doJSONAs(t, env, "u1", http.MethodDelete, "/api/devices/"+device.ID, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", rec.Code)
	}

	rec = doJSONAs(t, env, "u1", http.MethodDelete, "/api/devices/"+device.ID, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("second delete status = %d, want 404", rec.Code)
	}
}

// TestDeleteDeviceRefusesForeignDevice 证明删除别人的设备得到 404 而不是 403：
// 响应既不能成功，也不能透露这个 id 是否属于另一个账号。
func TestDeleteDeviceRefusesForeignDevice(t *testing.T) {
	env := newTestEnv(t, time.Second)
	created := decodeBody[deviceView](t, doJSONAs(t, env, "u1", http.MethodPost, "/api/devices",
		`{"name":"pixel","platform":"android","push_token":"tok-a"}`))

	rec := doJSONAs(t, env, "u2", http.MethodDelete, "/api/devices/"+created.ID, "")
	requireErrorCode(t, rec, http.StatusNotFound, "device_not_found")

	still := decodeBody[[]deviceView](t, doJSONAs(t, env, "u1", http.MethodGet, "/api/devices", ""))
	if len(still) != 1 || still[0].ID != created.ID {
		t.Fatalf("devices of u1 = %+v, want the row untouched", still)
	}

	// 管理员可以清理任意账号的设备（换机、手机丢失等场景）。
	adminDelete := doJSONAsRole(t, env,
		authz.Principal{Kind: authz.KindUser, UserID: "admin-1", Role: roleAdmin},
		http.MethodDelete, "/api/devices/"+created.ID, "")
	requireStatus(t, adminDelete, http.StatusNoContent)
}
