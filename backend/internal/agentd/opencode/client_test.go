package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCreateSessionSendsDirectory(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/session" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Error(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "ses_1", "directory": "/work/foo"})
	}))
	defer srv.Close()

	got, err := New(srv.URL, srv.Client()).CreateSession(context.Background(), "/work/foo")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "ses_1" {
		t.Fatalf("id = %q", got.ID)
	}
	location, _ := gotBody["location"].(map[string]any)
	if location["directory"] != "/work/foo" {
		t.Fatalf("body = %#v", gotBody)
	}
}

func TestListSessions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/session" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"id":"ses_1","directory":"/work/foo","title":"demo","updatedAt":"2026-11-01T10:00:00Z"}]`))
	}))
	defer srv.Close()

	got, err := New(srv.URL, srv.Client()).ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("sessions = %#v", got)
	}
	if got[0].ID != "ses_1" || got[0].Directory != "/work/foo" || got[0].Title != "demo" {
		t.Fatalf("session = %#v", got[0])
	}
	want := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	if !got[0].UpdatedAt.Equal(want) {
		t.Fatalf("updatedAt = %v, want %v", got[0].UpdatedAt, want)
	}
}

func TestPromptAndPermission(t *testing.T) {
	var promptBody map[string]any
	var replyBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/session/ses_1/prompt":
			if err := json.NewDecoder(r.Body).Decode(&promptBody); err != nil {
				t.Error(err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"receipt": "rcpt_1"})
		case r.Method == http.MethodPost && r.URL.Path == "/api/session/ses_1/permission/per_1/reply":
			if err := json.NewDecoder(r.Body).Decode(&replyBody); err != nil {
				t.Error(err)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := New(srv.URL, srv.Client())
	ctx := context.Background()

	receipt, err := client.Prompt(ctx, "ses_1", "hi")
	if err != nil {
		t.Fatal(err)
	}
	if receipt != "rcpt_1" {
		t.Fatalf("receipt = %q", receipt)
	}
	if len(promptBody) != 1 || promptBody["text"] != "hi" {
		t.Fatalf("prompt body = %#v", promptBody)
	}

	if err := client.ReplyPermission(ctx, "ses_1", "per_1", "once", ""); err != nil {
		t.Fatal(err)
	}
	if len(replyBody) != 1 || replyBody["decision"] != "once" {
		t.Fatalf("reply body without message = %#v", replyBody)
	}

	if err := client.ReplyPermission(ctx, "ses_1", "per_1", "always", "approved"); err != nil {
		t.Fatal(err)
	}
	if len(replyBody) != 2 || replyBody["decision"] != "always" || replyBody["message"] != "approved" {
		t.Fatalf("reply body with message = %#v", replyBody)
	}
}

func TestMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/session/ses_1/message" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"id":"msg_1","role":"assistant","parts":[{"type":"text","text":"done"}]}]`))
	}))
	defer srv.Close()

	got, err := New(srv.URL, srv.Client()).Messages(context.Background(), "ses_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("messages = %#v", got)
	}
	if got[0].ID != "msg_1" || got[0].Role != "assistant" {
		t.Fatalf("message = %#v", got[0])
	}
	if len(got[0].Parts) != 1 || got[0].Parts[0].Type != "text" || got[0].Parts[0].Text != "done" {
		t.Fatalf("parts = %#v", got[0].Parts)
	}
}

func TestHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/health" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// A trailing slash on the base URL must not produce a double slash.
	if err := New(srv.URL+"/", srv.Client()).Health(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestHealthReportsStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "still booting", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	err := New(srv.URL, srv.Client()).Health(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "status 503") {
		t.Fatalf("err = %v", err)
	}
}

// TestErrorBodyIsBounded ensures a chatty failure cannot flood logs with an
// unbounded response body.
func TestErrorBodyIsBounded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(strings.Repeat("x", 8<<10)))
	}))
	defer srv.Close()

	_, err := New(srv.URL, srv.Client()).ListSessions(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "opencode GET /api/session: status 500") {
		t.Fatalf("err = %v", err)
	}
	body := strings.TrimPrefix(err.Error(), "opencode GET /api/session: status 500: ")
	if len(body) > maxErrorBody {
		t.Fatalf("body length = %d, want <= %d", len(body), maxErrorBody)
	}
}

func TestSubscribeEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/event" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Accept"); got != "text/event-stream" {
			t.Errorf("Accept = %q", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "event: message\n"+
			`data: {"type":"session.text.delta","sessionID":"ses_1","data":{"text":"ok"}}`+"\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := New(srv.URL, srv.Client()).SubscribeEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}

	select {
	case ev, ok := <-events:
		if !ok {
			t.Fatal("channel closed before first event")
		}
		if ev.Type != "session.text.delta" {
			t.Fatalf("type = %q", ev.Type)
		}
		if ev.SessionID != "ses_1" {
			t.Fatalf("sessionID = %q", ev.SessionID)
		}
		var payload struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(ev.Data, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Text != "ok" {
			t.Fatalf("data = %s", ev.Data)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for event")
	}

	// The fake server returns after one event, so the stream reaches EOF.
	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("received a second event, want closed channel")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for closed channel")
	}
}

func TestSubscribeEventsClosesOnContextCancel(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(release)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "data: {\"type\":\"session.text.delta\",\"sessionID\":\"ses_1\"}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	events, err := New(srv.URL, srv.Client()).SubscribeEvents(ctx)
	if err != nil {
		cancel()
		t.Fatal(err)
	}

	select {
	case <-events:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("timed out waiting for event")
	}

	cancel()
	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("received an event after cancel")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("channel not closed after cancel")
	}
	<-release
}

func TestSubscribeEventsReportsStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()

	_, err := New(srv.URL, srv.Client()).SubscribeEvents(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "opencode GET /api/event: status 403") {
		t.Fatalf("err = %v", err)
	}
}

// TestClientNormalizesBaseURL covers the common case where the caller passes a
// base URL with a trailing slash.
func TestClientNormalizesBaseURL(t *testing.T) {
	client := New("http://127.0.0.1:4096/", nil)
	if client.baseURL != "http://127.0.0.1:4096" {
		t.Fatalf("baseURL = %q", client.baseURL)
	}
}
