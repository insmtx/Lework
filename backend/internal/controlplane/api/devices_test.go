package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRegisterDevice(t *testing.T) {
	env := newTestEnv(t, time.Second)
	rec := env.do(http.MethodPost, "/api/devices",
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

func TestRegisterDeviceIsIdempotentPerToken(t *testing.T) {
	env := newTestEnv(t, time.Second)
	env.do(http.MethodPost, "/api/devices", `{"name":"pixel","platform":"android","push_token":"tok-a"}`)
	rec := env.do(http.MethodPost, "/api/devices", `{"name":"pixel-2","platform":"android","push_token":"tok-a"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}

	list := env.do(http.MethodGet, "/api/devices", "")
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
		rec := env.do(http.MethodPost, "/api/devices", body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %s: status = %d, want 400", body, rec.Code)
		}
	}
}

func TestListAndDeleteDevices(t *testing.T) {
	env := newTestEnv(t, time.Second)
	created := env.do(http.MethodPost, "/api/devices",
		`{"name":"pixel","platform":"android","push_token":"tok-a"}`)
	var device deviceView
	if err := json.Unmarshal(created.Body.Bytes(), &device); err != nil {
		t.Fatalf("decode: %v", err)
	}

	rec := env.do(http.MethodGet, "/api/devices", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "tok-a") {
		t.Fatalf("list status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = env.do(http.MethodDelete, "/api/devices/"+device.ID, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", rec.Code)
	}

	rec = env.do(http.MethodDelete, "/api/devices/"+device.ID, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("second delete status = %d, want 404", rec.Code)
	}
}
