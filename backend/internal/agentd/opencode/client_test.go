package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCreateSessionSendsLocationAndDecodesEnvelope(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/session" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Error(err)
		}
		_, _ = w.Write([]byte(`{"data":{"id":"ses_1","projectID":"proj_1",` +
			`"location":{"directory":"/work/foo"},"title":"demo",` +
			`"time":{"created":1793529419920,"updated":1793529425262}}}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL, srv.Client()).CreateSession(context.Background(), "/work/foo")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "ses_1" || got.Directory != "/work/foo" || got.Title != "demo" {
		t.Fatalf("session = %#v", got)
	}
	if want := time.UnixMilli(1793529425262).UTC(); !got.UpdatedAt.Equal(want) {
		t.Fatalf("updatedAt = %v, want %v", got.UpdatedAt, want)
	}
	location, _ := gotBody["location"].(map[string]any)
	if location["directory"] != "/work/foo" {
		t.Fatalf("body = %#v", gotBody)
	}
	if _, topLevel := gotBody["directory"]; topLevel {
		t.Fatalf("body sent a top-level directory: %#v", gotBody)
	}
}

func TestListSessionsDecodesEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/session" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("directory"); got != "/work/foo" {
			t.Errorf("directory = %q, want /work/foo", got)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"ses_1","location":{"directory":"/work/foo"},` +
			`"title":"demo","time":{"created":1,"updated":1793529425262}}],"cursor":{}}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL, srv.Client()).ListSessions(context.Background(), "/work/foo")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("sessions = %#v", got)
	}
	if got[0].ID != "ses_1" || got[0].Directory != "/work/foo" || got[0].Title != "demo" {
		t.Fatalf("session = %#v", got[0])
	}
}

func TestRenameSessionSendsTitle(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/api/session/ses_1" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if err := New(srv.URL, srv.Client()).RenameSession(context.Background(), "ses_1", "new title"); err != nil {
		t.Fatal(err)
	}
	if body["title"] != "new title" {
		t.Fatalf("body = %#v, want title", body)
	}
}

func TestDeleteSessionUsesDelete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/session/ses_1" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if err := New(srv.URL, srv.Client()).DeleteSession(context.Background(), "ses_1"); err != nil {
		t.Fatal(err)
	}
}

func TestPromptReturnsAdmissionID(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/session/ses_1/prompt" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Error(err)
		}
		_, _ = w.Write([]byte(`{"data":{"id":"msg_admitted","sessionID":"ses_1",` +
			`"type":"user","payload":{"text":"hi"},"delivery":"steer"}}`))
	}))
	defer srv.Close()

	receipt, err := New(srv.URL, srv.Client()).Prompt(context.Background(), "ses_1", "hi")
	if err != nil {
		t.Fatal(err)
	}
	if receipt != "msg_admitted" {
		t.Fatalf("receipt = %q", receipt)
	}
	if len(gotBody) != 1 || gotBody["text"] != "hi" {
		t.Fatalf("prompt body = %#v", gotBody)
	}
}

func TestPromptRejectsMissingAdmissionID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"sessionID":"ses_1"}}`))
	}))
	defer srv.Close()

	if _, err := New(srv.URL, srv.Client()).Prompt(context.Background(), "ses_1", "hi"); err == nil {
		t.Fatal("expected error for a response without an admission id")
	}
}

func TestReplyPermission(t *testing.T) {
	var replyBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/session/ses_1/permission/per_1/reply" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&replyBody); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := New(srv.URL, srv.Client())
	ctx := context.Background()

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

func TestListPermissionsDecodesPendingRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/permission/request" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[` +
			`{"id":"per_1","sessionID":"ses_1","action":"shell",` +
			`"resources":["git status"],"message":"approve"},` +
			`{"id":"per_2","sessionID":"ses_2","action":"edit","resources":["a.go"]}` +
			`]}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL, srv.Client()).ListPermissions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("permissions = %#v", got)
	}
	if got[0].ID != "per_1" || got[0].SessionID != "ses_1" || got[0].Action != "shell" {
		t.Fatalf("first permission = %#v", got[0])
	}
	if len(got[0].Resources) != 1 || got[0].Resources[0] != "git status" {
		t.Fatalf("resources = %#v", got[0].Resources)
	}
	if got[1].Message != "" {
		t.Fatalf("message = %q, want empty", got[1].Message)
	}
}

func TestMessagesPaginatesNewestFirstAndReverses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/session/ses_1/message" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		query := r.URL.Query()
		if got := query.Get("order"); got != "desc" {
			t.Errorf("order = %q, want desc", got)
		}
		if got := query.Get("limit"); got != "50" {
			t.Errorf("limit = %q, want 50", got)
		}
		if got := query.Get("cursor"); got != "cursor-1" {
			t.Errorf("cursor = %q, want cursor-1", got)
		}
		// 服务端按最新优先返回；客户端负责反转成时间顺序。
		_, _ = w.Write([]byte(`{"data":[` +
			`{"id":"msg_3","type":"assistant","agent":"build","content":[` +
			`{"type":"text","text":"O"},{"type":"text","text":"K"}],"finish":"stop"},` +
			`{"id":"msg_2","type":"user","text":"hi"},` +
			`{"id":"msg_1","type":"idle","outcome":"succeeded"}` +
			`],"cursor":{"previous":"prev","next":"older"}}`))
	}))
	defer srv.Close()

	page, err := New(srv.URL, srv.Client()).Messages(context.Background(), "ses_1", 50, "cursor-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 3 {
		t.Fatalf("messages = %#v", page.Messages)
	}
	if page.Messages[0].ID != "msg_1" || page.Messages[1].ID != "msg_2" ||
		page.Messages[2].ID != "msg_3" {
		t.Fatalf("messages order = %#v, want chronological", page.Messages)
	}
	if page.Messages[2].Role != "assistant" || page.Messages[2].Text != "OK" {
		t.Fatalf("assistant turn = %#v", page.Messages[2])
	}
	if page.Previous != "prev" || page.Next != "older" {
		t.Fatalf("cursor = %#v", page)
	}
}

func TestMessagesDefaultsLimitAndCarriesStructuredError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("limit"); got != "200" {
			t.Errorf("limit = %q, want 200", got)
		}
		if got := r.URL.Query().Get("order"); got != "desc" {
			t.Errorf("order = %q, want desc", got)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"msg_1","type":"idle","outcome":"failed",` +
			`"error":{"type":"provider.quota","message":"limit reached","status":429}}],"cursor":{}}`))
	}))
	defer srv.Close()

	page, err := New(srv.URL, srv.Client()).Messages(context.Background(), "ses_1", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if page.Messages[0].Error == nil || page.Messages[0].Error.Type != "provider.quota" ||
		page.Messages[0].Error.Status != 429 {
		t.Fatalf("error = %#v", page.Messages[0].Error)
	}
}

func TestHealthUsesInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/info" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"version":"2.0.32","pid":1,"urls":[],"paths":{"tmp":"/tmp"}}`))
	}))
	defer srv.Close()

	// A trailing slash on the base URL must not produce a double slash.
	if err := New(srv.URL+"/", srv.Client()).Health(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestHealthRejectsUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"_tag":"UnauthorizedError","message":"Authentication required"}`))
	}))
	defer srv.Close()

	err := New(srv.URL, srv.Client(), WithPassword("wrong")).Health(context.Background())
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
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
	if errors.Is(err, ErrUnauthorized) {
		t.Fatalf("503 reported as unauthorized: %v", err)
	}
	if !strings.Contains(err.Error(), "status 503") {
		t.Fatalf("err = %v", err)
	}
}

func TestWaitReturnsOnNoContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/experimental/session/ses_1/wait" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if err := New(srv.URL, srv.Client()).Wait(context.Background(), "ses_1"); err != nil {
		t.Fatal(err)
	}
}

func TestWaitReportsUnavailableEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"_tag":"RouteNotFoundError","message":"not found"}`))
	}))
	defer srv.Close()

	err := New(srv.URL, srv.Client()).Wait(context.Background(), "ses_1")
	if !errors.Is(err, ErrExperimentalUnavailable) {
		t.Fatalf("err = %v, want ErrExperimentalUnavailable", err)
	}
}

func TestWaitReportsServerFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	err := New(srv.URL, srv.Client()).Wait(context.Background(), "ses_1")
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, ErrExperimentalUnavailable) {
		t.Fatalf("503 reported as unavailable: %v", err)
	}
	if !strings.Contains(err.Error(), "status 503") {
		t.Fatalf("err = %v", err)
	}
}

func TestClientSendsBasicAuth(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		user, password, ok := r.BasicAuth()
		if !ok || user != "opencode" || password != "secret" {
			t.Errorf("basic auth = %q/%q/%v", user, password, ok)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/api/event" {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `data: {"id":"evt_1","type":"server.connected","data":{}}`+"\n\n")
			return
		}
		_, _ = w.Write([]byte(`{"data":{"id":"ses_1","location":{"directory":"/work"}}}`))
	}))
	defer srv.Close()

	client := New(srv.URL, srv.Client(), WithPassword("secret"))
	if _, err := client.CreateSession(context.Background(), "/work"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := client.SubscribeEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-events:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for event")
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
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

	_, err := New(srv.URL, srv.Client()).ListSessions(context.Background(), "")
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
		_, _ = fmt.Fprint(w, ": heartbeat\n\n"+
			`data: {"id":"evt_1","created":1793529424244,"type":"session.execution.started",`+
			`"data":{"sessionID":"ses_1"},"durable":{"aggregateID":"ses_1","seq":2,"version":1}}`+"\n\n")
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
		if ev.ID != "evt_1" || ev.Type != "session.execution.started" {
			t.Fatalf("event = %#v", ev)
		}
		if ev.SessionID != "ses_1" {
			t.Fatalf("sessionID = %q", ev.SessionID)
		}
		var payload struct {
			SessionID string `json:"sessionID"`
		}
		if err := json.Unmarshal(ev.Data, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.SessionID != "ses_1" {
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
		_, _ = fmt.Fprint(w, `data: {"id":"evt_1","type":"server.connected","data":{}}`+"\n\n")
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

func TestSessionDiffSendsQueryAndDecodesEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/session/ses_1/diff" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("from") != "msg_a" || q.Get("to") != "msg_b" || q.Get("context") != "3" {
			t.Errorf("query = %v", q)
		}
		_, _ = w.Write([]byte(`{"data":[{"file":"a.txt","patch":"@@","additions":1,"deletions":0,"status":"modified"}]}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL, srv.Client()).SessionDiff(context.Background(), "ses_1", "msg_a", "msg_b", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].File != "a.txt" || got[0].Additions != 1 || got[0].Status != "modified" {
		t.Fatalf("diff = %#v", got)
	}
}

func TestSessionDiffOmitsEmptyQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Errorf("raw query = %q, want empty", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	if _, err := New(srv.URL, srv.Client()).SessionDiff(context.Background(), "ses_1", "", "", 0); err != nil {
		t.Fatal(err)
	}
}

func TestInterruptSendsPostWithoutBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/session/ses_1/interrupt" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("resume") != "true" {
			t.Errorf("resume = %q", r.URL.Query().Get("resume"))
		}
		body, _ := io.ReadAll(r.Body)
		if len(body) != 0 {
			t.Errorf("body = %q, want empty", body)
		}
		_, _ = w.Write([]byte(`{"interrupted":true}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL, srv.Client()).Interrupt(context.Background(), "ses_1", true)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Interrupted {
		t.Fatal("interrupted = false, want true")
	}
}

func TestListFilesDecodesEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/fs/list" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("location[directory]") != "/work" || q.Get("path") != "src" {
			t.Errorf("query = %v", q)
		}
		_, _ = w.Write([]byte(`{"location":{"directory":"/work"},"data":[{"path":"a.txt","type":"file"},{"path":"sub/","type":"directory"}]}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL, srv.Client()).ListFiles(context.Background(), "/work", "src")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Path != "a.txt" || got[0].Type != "file" || got[1].Type != "directory" {
		t.Fatalf("files = %#v", got)
	}
}

func TestReadFileUsesRangeAndReportsTruncation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/fs/read/a.txt" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Range"); got != "bytes=0-3" {
			t.Errorf("range = %q", got)
		}
		if got := r.URL.Query().Get("location[directory]"); got != "/work" {
			t.Errorf("directory = %q", got)
		}
		w.Header().Set("Content-Range", "bytes 0-3/10")
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("abcd"))
	}))
	defer srv.Close()

	got, err := New(srv.URL, srv.Client()).ReadFile(context.Background(), "/work", "a.txt", 4)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Data) != "abcd" || got.Size != 10 || !got.Truncated {
		t.Fatalf("content = %#v", got)
	}
	if got.ContentType != "text/plain" {
		t.Fatalf("content type = %q", got.ContentType)
	}
}

func TestReadFileRejectsErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "missing", http.StatusNotFound)
	}))
	defer srv.Close()

	if _, err := New(srv.URL, srv.Client()).ReadFile(context.Background(), "/work", "a.txt", 4); err == nil {
		t.Fatal("expected error")
	}
}

func TestEscapePathSegments(t *testing.T) {
	got := escapePathSegments("dir with space/a#b.txt")
	if got != "dir%20with%20space/a%23b.txt" {
		t.Fatalf("escaped = %q", got)
	}
}

func TestMessageDecodesToolPart(t *testing.T) {
	var message Message
	err := json.Unmarshal([]byte(`{
		"id":"msg_1",
		"type":"assistant",
		"content":[
			{"type":"text","text":"running"},
			{"type":"tool","id":"call_1","name":"bash","state":{"status":"completed","input":{"command":"ls"},"content":[{"type":"text","text":"a.txt"}]}}
		]
	}`), &message)
	if err != nil {
		t.Fatal(err)
	}
	if message.Text != "running" {
		t.Fatalf("text = %q", message.Text)
	}
	if len(message.Parts) != 2 || message.Parts[1].Tool != "bash" || message.Parts[1].CallID != "call_1" {
		t.Fatalf("parts = %#v", message.Parts)
	}
	var state struct {
		Status  string `json:"status"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(message.Parts[1].State, &state); err != nil {
		t.Fatal(err)
	}
	if state.Status != "completed" || len(state.Content) != 1 || state.Content[0].Text != "a.txt" {
		t.Fatalf("state = %#v", state)
	}
}
