package fetch

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type fetchRoundTripFunc func(*http.Request) (*http.Response, error)

func (f fetchRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func fetchResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
		Request:    request,
	}
}

func TestClawHubFetchSkillsListRetriesRetryableStatusThenSucceeds(t *testing.T) {
	attempts := 0
	source := NewClawHubSource()
	source.client = &http.Client{Transport: fetchRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return fetchResponse(r, http.StatusServiceUnavailable, ""), nil
		}
		if r.URL.Path != "/api/v1/skills" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		return fetchResponse(r, http.StatusOK, `{"items":[{"slug":"demo-skill","displayName":"Demo Skill","summary":"Demo summary","latestVersion":{"version":"1.0.0"}}]}`), nil
	})}

	items, err := source.fetchSkillsList(context.Background(), "http://clawhub.test/api/v1/skills?limit=10&sort=trending")
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
	source := NewClawHubSource()
	source.client = &http.Client{Transport: fetchRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		attempts++
		return fetchResponse(r, http.StatusBadGateway, ""), nil
	})}

	_, err := source.fetchSkillsList(context.Background(), "http://clawhub.test/api/v1/skills?limit=10&sort=trending")
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
	source := NewClawHubSource()
	source.client = &http.Client{Transport: fetchRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		attempts++
		return fetchResponse(r, http.StatusNotFound, ""), nil
	})}

	_, err := source.fetchSkillsList(context.Background(), "http://clawhub.test/api/v1/search?q=demo&limit=10")
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
