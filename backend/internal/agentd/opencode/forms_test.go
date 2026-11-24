package opencode

import (
	"encoding/json"
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
