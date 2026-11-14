package api

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/insmtx/lework/backend/internal/controlplane/store"
)

func TestListProjectsMergesSameIDAcrossNodes(t *testing.T) {
	env := newTestEnv(t, 2*time.Second)
	_, agent1, conn1 := env.addNode("node-a")
	_, agent2, conn2 := env.addNode("node-b")

	first := replyOK(t, agent1, conn1, `[{"id":"abc","canonical":"/w/repo","vcs":"git"}]`)
	second := replyOK(t, agent2, conn2, `[{"id":"abc","canonical":"/w/repo","vcs":"git"}]`)

	rec := env.do(http.MethodGet, "/api/projects", "")
	requireStatus(t, rec, http.StatusOK)

	if frame := waitFrame(t, first); frame.Method != "project.list" {
		t.Fatalf("method = %q, want project.list", frame.Method)
	}
	if frame := waitFrame(t, second); frame.Method != "project.list" {
		t.Fatalf("method = %q, want project.list", frame.Method)
	}

	body := decodeBody[projectListResponse](t, rec)
	if len(body.Projects) != 1 {
		t.Fatalf("projects = %#v, want one merged project", body.Projects)
	}
	project := body.Projects[0]
	if project.Key != "abc" || project.Canonical != "/w/repo" {
		t.Fatalf("project = %#v", project)
	}
	if len(project.Nodes) != 2 {
		t.Fatalf("nodes = %#v, want two entries", project.Nodes)
	}
	for _, node := range project.Nodes {
		if !node.Online {
			t.Fatalf("node = %#v, want online", node)
		}
	}
}

func TestListProjectsSeparatesGlobalSentinel(t *testing.T) {
	env := newTestEnv(t, 2*time.Second)
	_, agent, conn := env.addNode("node-a")

	replyOK(t, agent, conn, `[
		{"id":"global","canonical":"/w/one"},
		{"id":"global","canonical":"/w/two"}
	]`)

	rec := env.do(http.MethodGet, "/api/projects", "")
	requireStatus(t, rec, http.StatusOK)

	body := decodeBody[projectListResponse](t, rec)
	if len(body.Projects) != 2 {
		t.Fatalf("projects = %#v, want two entries for two uncommitted repos", body.Projects)
	}
	keys := map[string]bool{}
	for _, project := range body.Projects {
		keys[project.Key] = true
	}
	if !keys["global:/w/one"] || !keys["global:/w/two"] {
		t.Fatalf("keys = %#v, want canonical-qualified global keys", keys)
	}
}

func TestListProjectsKeepsOfflineFromIndex(t *testing.T) {
	env := newTestEnv(t, 2*time.Second)
	rec, _, _ := env.addNode("node-a")

	if err := env.store.UpsertProjectIndex(context.Background(), []store.ProjectIndex{{
		ProjectKey: "abc",
		NodeID:     rec.ID,
		ProjectID:  "abc",
		Canonical:  "/w/repo",
		VCS:        "git",
		LastSeen:   time.Now(),
	}}); err != nil {
		t.Fatal(err)
	}
	env.registry.CloseAll()

	httpRec := env.do(http.MethodGet, "/api/projects", "")
	requireStatus(t, httpRec, http.StatusOK)

	body := decodeBody[projectListResponse](t, httpRec)
	if len(body.Projects) != 1 {
		t.Fatalf("projects = %#v, want the indexed project", body.Projects)
	}
	project := body.Projects[0]
	if project.Key != "abc" || project.Canonical != "/w/repo" {
		t.Fatalf("project = %#v", project)
	}
	if len(project.Nodes) != 1 || project.Nodes[0].Online {
		t.Fatalf("nodes = %#v, want one offline entry", project.Nodes)
	}
}

func TestListProjectsSurvivesOneNodeFailure(t *testing.T) {
	env := newTestEnv(t, 2*time.Second)
	_, agent1, conn1 := env.addNode("node-a")
	_, agent2, conn2 := env.addNode("node-b")

	replyErr(t, agent1, conn1, "internal", "boom")
	replyOK(t, agent2, conn2, `[{"id":"abc","canonical":"/w/repo"}]`)

	rec := env.do(http.MethodGet, "/api/projects", "")
	requireStatus(t, rec, http.StatusOK)

	body := decodeBody[projectListResponse](t, rec)
	if len(body.Projects) != 1 || body.Projects[0].Key != "abc" {
		t.Fatalf("projects = %#v, want the healthy node's project", body.Projects)
	}
}

func TestSetPrimaryNodeRejectsUnknownNode(t *testing.T) {
	env := newTestEnv(t, 2*time.Second)
	rec, agent, conn := env.addNode("node-a")
	replyOK(t, agent, conn, `[{"id":"abc","canonical":"/w/repo"}]`)

	// 先做一次聚合，让项目进入索引。
	if httpRec := env.do(http.MethodGet, "/api/projects", ""); httpRec.Code != http.StatusOK {
		t.Fatalf("prime projects: %d %s", httpRec.Code, httpRec.Body.String())
	}

	unknownNode := env.do(http.MethodPut, "/api/projects/abc/primary-node", `{"node_id":"node_missing"}`)
	requireErrorCode(t, unknownNode, http.StatusNotFound, "not_found")

	unknownProject := env.do(http.MethodPut, "/api/projects/nope/primary-node",
		`{"node_id":"`+rec.ID+`"}`)
	requireErrorCode(t, unknownProject, http.StatusNotFound, "not_found")

	ok := env.do(http.MethodPut, "/api/projects/abc/primary-node", `{"node_id":"`+rec.ID+`"}`)
	requireStatus(t, ok, http.StatusNoContent)
}

func TestPreferencesRoundTrip(t *testing.T) {
	env := newTestEnv(t, 2*time.Second)

	put := env.do(http.MethodPut, "/api/preferences/"+url.PathEscape("inbox.dir.node_1"),
		`{"value":"/w/inbox"}`)
	requireStatus(t, put, http.StatusNoContent)

	rec := env.do(http.MethodGet, "/api/preferences", "")
	requireStatus(t, rec, http.StatusOK)

	body := decodeBody[preferencesResponse](t, rec)
	if body.Values["inbox.dir.node_1"] != "/w/inbox" {
		t.Fatalf("values = %#v", body.Values)
	}
}
