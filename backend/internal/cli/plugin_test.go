package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/insmtx/Leros/backend/internal/api/contract"
)

func TestListOrganizationPluginsSendsSkillFilters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/plugins" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		query := r.URL.Query()
		if query.Get("kind") != "skill" || query.Get("status") != "active" ||
			query.Get("keyword") != "bid" || query.Get("offset") != "2" || query.Get("limit") != "5" {
			t.Fatalf("query = %v", query)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"plugins":[{"code":"bid-review","kind":"skill","name":"Bid Review","status":"active","origin":"org","current_revision":1}]}}`))
	}))
	defer server.Close()

	result, err := ListOrganizationPlugins(
		context.Background(),
		strings.TrimPrefix(server.URL, "http://"),
		"token",
		&contract.ListPluginsRequest{Kind: "skill", Status: "active", Keyword: "bid", Offset: 2, Limit: 5},
	)
	if err != nil {
		t.Fatalf("ListOrganizationPlugins() error = %v", err)
	}
	if len(result.Plugins) != 1 || result.Plugins[0].Code != "bid-review" {
		t.Fatalf("plugins = %#v", result.Plugins)
	}
}

func TestAddProjectPluginSendsPluginCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/AddProjectPlugin" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		var req contract.UpdateProjectPluginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.PublicID != "project-1" || req.PluginCode != "bid-review" || req.Kind != "skill" || req.PluginID != "" {
			t.Fatalf("request = %#v", req)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"project_id":"project-1","plugin_code":"bid-review","kind":"skill","associated":true,"changed":true}}`))
	}))
	defer server.Close()

	result, err := AddProjectPlugin(
		context.Background(),
		strings.TrimPrefix(server.URL, "http://"),
		"token",
		&contract.UpdateProjectPluginRequest{PublicID: "project-1", PluginCode: "bid-review", Kind: "skill"},
	)
	if err != nil {
		t.Fatalf("AddProjectPlugin() error = %v", err)
	}
	if result.PluginCode != "bid-review" || !result.Associated || !result.Changed {
		t.Fatalf("result = %#v", result)
	}
}
