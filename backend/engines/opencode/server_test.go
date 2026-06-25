package opencode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendPermissionDecisionUsesLatestReplyEndpoint(t *testing.T) {
	var gotPath string
	var gotBody permissionDecision
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want %s", r.Method, http.MethodPost)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	srv := &OpenCodeServer{
		baseURL:    server.URL,
		httpClient: server.Client(),
	}

	if err := srv.SendPermissionDecision(context.Background(), "per_123", "once"); err != nil {
		t.Fatalf("send permission decision: %v", err)
	}
	if gotPath != "/permission/per_123/reply" {
		t.Fatalf("path = %q, want %q", gotPath, "/permission/per_123/reply")
	}
	if gotBody.Reply != "once" {
		t.Fatalf("reply = %q, want %q", gotBody.Reply, "once")
	}
	if gotBody.Message != "" {
		t.Fatalf("message = %q, want empty", gotBody.Message)
	}
}

func TestSendQuestionAnswerUsesLatestReplyEndpoint(t *testing.T) {
	var gotPath string
	var gotBody questionAnswerReq
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want %s", r.Method, http.MethodPost)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	srv := &OpenCodeServer{
		baseURL:    server.URL,
		httpClient: server.Client(),
	}

	answers := [][]string{{"Use latest endpoint"}}
	if err := srv.SendQuestionAnswer(context.Background(), "que_123", answers); err != nil {
		t.Fatalf("send question answer: %v", err)
	}
	if gotPath != "/question/que_123/reply" {
		t.Fatalf("path = %q, want %q", gotPath, "/question/que_123/reply")
	}
	if len(gotBody.Answers) != 1 || len(gotBody.Answers[0]) != 1 || gotBody.Answers[0][0] != answers[0][0] {
		t.Fatalf("answers = %#v, want %#v", gotBody.Answers, answers)
	}
}
