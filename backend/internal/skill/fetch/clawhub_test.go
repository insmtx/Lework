package fetch

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClawHubFetchSkillsListRetriesRetryableStatusThenSucceeds(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if r.URL.Path != "/api/v1/skills" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"items":[{"slug":"demo-skill","displayName":"Demo Skill","summary":"Demo summary","latestVersion":{"version":"1.0.0"}}]}`)
	}))
	defer server.Close()

	source := NewClawHubSource()

	items, err := source.fetchSkillsList(context.Background(), server.URL+"/api/v1/skills?limit=10&sort=trending")
	if err != nil {
		t.Fatalf("fetch skills list: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	if len(items) != 1 || items[0].Slug != "demo-skill" || items[0].LatestVersion == nil || items[0].LatestVersion.Version != "1.0.0" {
		t.Fatalf("unexpected items: %#v", items)
	}
}

func TestClawHubFetchSkillsListRetriesRetryableStatusUntilFailure(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	source := NewClawHubSource()

	_, err := source.fetchSkillsList(context.Background(), server.URL+"/api/v1/skills?limit=10&sort=trending")
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "clawhub returned status 502" {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != clawhubSearchMaxAttempts {
		t.Fatalf("expected %d attempts, got %d", clawhubSearchMaxAttempts, attempts)
	}
}

func TestClawHubFetchSkillsListDoesNotRetryNonRetryableStatus(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	source := NewClawHubSource()

	_, err := source.fetchSkillsList(context.Background(), server.URL+"/api/v1/search?q=demo&limit=10")
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "clawhub returned status 404" {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt, got %d", attempts)
	}
}
