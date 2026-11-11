package push

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExpoSenderPostsMessages(t *testing.T) {
	var got []Message
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode body: %v", err)
		}
		_, _ = w.Write([]byte(`{"data":[{"status":"ok","id":"1"},{"status":"ok","id":"2"}]}`))
	}))
	defer srv.Close()

	sender := NewExpoSenderWithEndpoint(srv.URL, srv.Client())
	err := sender.Send(context.Background(), []Message{
		{To: "tok-a", Title: "需要审批", Body: "bash", Tag: "ses_1", ChannelID: "permissions", Priority: "high"},
		{To: "tok-b", Title: "需要审批", Body: "bash", Tag: "ses_1", ChannelID: "permissions", Priority: "high"},
	})
	if err != nil {
		t.Fatalf("Send() failed: %v", err)
	}
	if len(got) != 2 || got[0].To != "tok-a" || got[1].Tag != "ses_1" {
		t.Fatalf("payload = %+v, want two messages", got)
	}
	if got[0].ChannelID != "permissions" || got[1].Priority != "high" {
		t.Fatalf("payload = %+v, want channelId/priority preserved", got)
	}
}

func TestExpoSenderRejectsErrorTicket(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"status":"error","message":"gone","details":{"error":"DeviceNotRegistered"}}]}`))
	}))
	defer srv.Close()

	err := NewExpoSenderWithEndpoint(srv.URL, srv.Client()).
		Send(context.Background(), []Message{{To: "tok"}})
	if err == nil || !strings.Contains(err.Error(), "DeviceNotRegistered") {
		t.Fatalf("err = %v, want DeviceNotRegistered", err)
	}
	if err != nil && strings.Contains(err.Error(), "tok") {
		t.Fatalf("err = %q leaked the push token", err)
	}
}

func TestExpoSenderReportsHTTPFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	err := NewExpoSenderWithEndpoint(srv.URL, srv.Client()).
		Send(context.Background(), []Message{{To: "tok"}})
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("err = %v, want status 500", err)
	}
}

func TestExpoSenderSkipsEmptyBatch(t *testing.T) {
	hit := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit++ }))
	defer srv.Close()

	if err := NewExpoSenderWithEndpoint(srv.URL, srv.Client()).Send(context.Background(), nil); err != nil {
		t.Fatalf("Send(nil) failed: %v", err)
	}
	if hit != 0 {
		t.Fatalf("server hit %d times, want 0", hit)
	}
}
