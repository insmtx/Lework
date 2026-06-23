package events

import (
	"strings"
	"testing"
	"time"
)

func TestNewArtifactDeclaredPayloadCarriesPersistenceFields(t *testing.T) {
	event := NewArtifactDeclared(ArtifactPayload{
		ArtifactID:   "art_123",
		Title:        "Report",
		Filename:     "report.md",
		Description:  "final report",
		MimeType:     "text/markdown",
		ArtifactType: "file",
		StorageKey:   "projects/1/prj/repo/report.md",
		Sha256:       "abc123",
		CreatedAt:    nowForArtifactTest(),
	})
	if event.Type != EventArtifactDeclared {
		t.Fatalf("event type = %q, want %q", event.Type, EventArtifactDeclared)
	}
	if string(event.Payload) == "" {
		t.Fatal("expected payload")
	}
	payload := string(event.Payload)
	for _, expected := range []string{"artifact_id", "description", "storage_key", "sha256", "created_at"} {
		if !strings.Contains(payload, expected) {
			t.Fatalf("payload should contain %q: %s", expected, payload)
		}
	}
	for _, forbidden := range []string{"tool_call_id", "download_url", "is_final"} {
		if strings.Contains(payload, forbidden) {
			t.Fatalf("payload should not contain %q: %s", forbidden, payload)
		}
	}
}

func nowForArtifactTest() time.Time {
	return time.Date(2026, 6, 22, 15, 0, 0, 0, time.FixedZone("CST", 8*3600))
}
