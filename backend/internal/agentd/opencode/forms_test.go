package opencode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEventFormCreatedFallsBackToFormSession(t *testing.T) {
	var ev Event
	raw := `{"id":"evt_1","type":"form.created","data":{"form":{"id":"frm_1","sessionID":"ses_1","title":"Questions","fields":[]}}}`
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		t.Fatal(err)
	}
	if ev.SessionID != "ses_1" {
		t.Fatalf("SessionID = %q, want ses_1", ev.SessionID)
	}
}

func TestEventFormCreatedKeepsGlobalSession(t *testing.T) {
	var ev Event
	raw := `{"type":"form.created","data":{"form":{"id":"frm_2","sessionID":"global","fields":[]}}}`
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		t.Fatal(err)
	}
	if ev.SessionID != "global" {
		t.Fatalf("SessionID = %q, want global", ev.SessionID)
	}
}

func TestEventTopLevelSessionStillWins(t *testing.T) {
	var ev Event
	raw := `{"type":"form.created","data":{"sessionID":"ses_top","form":{"sessionID":"ses_form"}}}`
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		t.Fatal(err)
	}
	if ev.SessionID != "ses_top" {
		t.Fatalf("SessionID = %q, want ses_top", ev.SessionID)
	}
}

func TestListFormsDecodesEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/session/ses_1/form" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"frm_1","sessionID":"ses_1","title":"Questions","fields":[{"key":"q0","type":"string","required":true,"options":[{"value":"a","label":"A"}],"custom":true}]}]}`))
	}))
	defer srv.Close()

	forms, err := New(srv.URL, srv.Client()).ListForms(context.Background(), "ses_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(forms) != 1 || forms[0].ID != "frm_1" || len(forms[0].Fields) != 1 {
		t.Fatalf("forms = %#v", forms)
	}
	if forms[0].Fields[0].Options[0].Value != "a" || !forms[0].Fields[0].Custom {
		t.Fatalf("field = %#v", forms[0].Fields[0])
	}
}

func TestListLocationFormsUsesBracketQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/form" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("location[directory]"); got != "/work/foo" {
			t.Errorf("location[directory] = %q", got)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"frm_1","sessionID":"global"},{"id":"frm_2","sessionID":"ses_1"}]}`))
	}))
	defer srv.Close()

	forms, err := New(srv.URL, srv.Client()).ListLocationForms(context.Background(), "/work/foo")
	if err != nil {
		t.Fatal(err)
	}
	if len(forms) != 2 {
		t.Fatalf("forms = %#v", forms)
	}
}

func TestReplyFormSendsAnswerAndDirectoryHeader(t *testing.T) {
	var body map[string]any
	var header string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/session/global/form/frm_1/reply" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		header = r.Header.Get("x-opencode-directory")
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	err := New(srv.URL, srv.Client()).ReplyForm(context.Background(), "global", "frm_1", "/work/foo",
		map[string]any{"q0": "a", "q1": true})
	if err != nil {
		t.Fatal(err)
	}
	if header != "%2Fwork%2Ffoo" {
		t.Fatalf("header = %q", header)
	}
	answer, _ := body["answer"].(map[string]any)
	if answer["q0"] != "a" || answer["q1"] != true {
		t.Fatalf("body = %#v", body)
	}
}

func TestReplyFormOmitsDirectoryHeaderWithoutDirectory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-opencode-directory"); got != "" {
			t.Errorf("header = %q, want empty", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if err := New(srv.URL, srv.Client()).ReplyForm(context.Background(), "ses_1", "frm_1", "",
		map[string]any{"q0": "a"}); err != nil {
		t.Fatal(err)
	}
}

func TestCancelFormUsesDeleteAndOptionalMessage(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s", r.Method)
		}
		paths = append(paths, r.URL.RequestURI())
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := New(srv.URL, srv.Client())
	if err := client.CancelForm(context.Background(), "ses_1", "frm_1", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := client.CancelForm(context.Background(), "ses_1", "frm_1", "", "不需要了"); err != nil {
		t.Fatal(err)
	}
	if paths[0] != "/api/session/ses_1/form/frm_1" {
		t.Fatalf("path = %q", paths[0])
	}
	if !strings.Contains(paths[1], "message=") {
		t.Fatalf("path = %q, want message query", paths[1])
	}
}
