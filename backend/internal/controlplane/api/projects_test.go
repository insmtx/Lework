package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/insmtx/lework/backend/internal/controlplane/authz"
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

// TestListProjectsKeepsCanonicalPerNode 固化「同一 git 项目在不同机器上
// 检出路径不同」：客户端新建会话必须用目标节点自己的 canonical。
func TestListProjectsKeepsCanonicalPerNode(t *testing.T) {
	env := newTestEnv(t, 2*time.Second)
	recA, agentA, connA := env.addNode("node-a")
	recB, agentB, connB := env.addNode("node-b")

	first := replyOK(t, agentA, connA, `[{"id":"abc","canonical":"/home/me/repo","vcs":"git"}]`)
	second := replyOK(t, agentB, connB, `[{"id":"abc","canonical":"/srv/repo","vcs":"git"}]`)

	rec := env.do(http.MethodGet, "/api/projects", "")
	requireStatus(t, rec, http.StatusOK)
	waitFrame(t, first)
	waitFrame(t, second)

	body := decodeBody[projectListResponse](t, rec)
	if len(body.Projects) != 1 {
		t.Fatalf("projects = %#v, want one aggregated project", body.Projects)
	}
	canonicalByNode := map[string]string{}
	for _, node := range body.Projects[0].Nodes {
		canonicalByNode[node.NodeID] = node.Canonical
	}
	if canonicalByNode[recA.ID] != "/home/me/repo" {
		t.Fatalf("node-a canonical = %q, want /home/me/repo", canonicalByNode[recA.ID])
	}
	if canonicalByNode[recB.ID] != "/srv/repo" {
		t.Fatalf("node-b canonical = %q, want /srv/repo", canonicalByNode[recB.ID])
	}
}

// 索引兜底同样要保留每个节点自己的路径，否则离线时又退回跨节点单值。
func TestListProjectsIndexKeepsCanonicalPerNode(t *testing.T) {
	env := newTestEnv(t, 2*time.Second)
	recA, _, _ := env.addNode("node-a")
	recB, _, _ := env.addNode("node-b")

	if err := env.store.UpsertProjectIndex(context.Background(), []store.ProjectIndex{
		{ProjectKey: "abc", NodeID: recA.ID, ProjectID: "abc", Canonical: "/home/me/repo", LastSeen: time.Now()},
		{ProjectKey: "abc", NodeID: recB.ID, ProjectID: "abc", Canonical: "/srv/repo", LastSeen: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}
	env.markNodeOffline(recA.ID)
	env.markNodeOffline(recB.ID)

	rec := env.do(http.MethodGet, "/api/projects", "")
	requireStatus(t, rec, http.StatusOK)

	body := decodeBody[projectListResponse](t, rec)
	if len(body.Projects) != 1 || len(body.Projects[0].Nodes) != 2 {
		t.Fatalf("projects = %#v, want one project on two offline nodes", body.Projects)
	}
	canonicalByNode := map[string]string{}
	for _, node := range body.Projects[0].Nodes {
		canonicalByNode[node.NodeID] = node.Canonical
	}
	if canonicalByNode[recA.ID] != "/home/me/repo" || canonicalByNode[recB.ID] != "/srv/repo" {
		t.Fatalf("canonical by node = %#v", canonicalByNode)
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
	env.markNodeOffline(rec.ID)

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
	if project.Nodes[0].Canonical != "/w/repo" {
		t.Fatalf("canonical = %q, want the indexed path", project.Nodes[0].Canonical)
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

// TestPreferencesAreIsolatedPerAccount 固化 /api/preferences 的账号隔离：
// 一个账号写入的偏好不会出现在另一个账号的同步包，也不会落进本机管理员的
// 历史命名空间。
func TestPreferencesAreIsolatedPerAccount(t *testing.T) {
	env := newTestEnv(t, 2*time.Second)
	alice := createTestUser(t, env, "alice@x.c", testLoginPassword, "user", "active")
	bob := createTestUser(t, env, "bob@x.c", testLoginPassword, "user", "active")

	put := doJSONAs(t, env, alice.ID, http.MethodPut, "/api/preferences/last_dir", `{"value":"/a"}`)
	requireStatus(t, put, http.StatusNoContent)

	aliceBody := decodeBody[preferencesResponse](t,
		doJSONAs(t, env, alice.ID, http.MethodGet, "/api/preferences", ""))
	if aliceBody.Values["last_dir"] != "/a" {
		t.Fatalf("alice preferences = %#v", aliceBody.Values)
	}

	bobBody := decodeBody[preferencesResponse](t,
		doJSONAs(t, env, bob.ID, http.MethodGet, "/api/preferences", ""))
	if _, ok := bobBody.Values["last_dir"]; ok {
		t.Fatalf("bob saw alice's preference: %#v", bobBody.Values)
	}

	// local 身份使用未加前缀的历史命名空间，账号偏好不属于它。
	deployment := decodeBody[preferencesResponse](t, env.do(http.MethodGet, "/api/preferences", ""))
	if len(deployment.Values) != 0 {
		t.Fatalf("deployment preferences = %#v, want none", deployment.Values)
	}
}

// TestListProjectsHidesInvisibleIndexRows 固化 R7：项目索引是全局派生缓存，
// 但投影必须按成员过滤——别人的节点行既不能出现，也不能被这次请求改写。
func TestListProjectsHidesInvisibleIndexRows(t *testing.T) {
	env := newTestEnv(t, 2*time.Second)
	alice := createTestUser(t, env, "alice@x.c", testLoginPassword, "user", "active")
	theirs := createTestUser(t, env, "theirs@x.c", testLoginPassword, "user", "active")

	mine, _, _ := env.addNode("a-mine")
	foreign, _, _ := env.addNode("b-foreign")
	mustAddMember(t, env, mine.ID, alice.ID, store.NodeRoleOwner)
	mustAddMember(t, env, foreign.ID, theirs.ID, store.NodeRoleOwner)

	// 两台节点都离线：投影完全来自索引，避免实时拉取引入时序噪声。
	env.markNodeOffline(mine.ID)
	env.markNodeOffline(foreign.ID)

	ctx := context.Background()
	rows := []store.ProjectIndex{
		{ProjectKey: "mine-proj", NodeID: mine.ID, ProjectID: "mine-proj", Canonical: "/w/mine"},
		{ProjectKey: "foreign-proj", NodeID: foreign.ID, ProjectID: "foreign-proj", Canonical: "/w/foreign"},
	}
	if err := env.store.UpsertProjectIndex(ctx, rows); err != nil {
		t.Fatalf("prime index: %v", err)
	}

	rec := doJSONAs(t, env, alice.ID, http.MethodGet, "/api/projects", "")
	requireStatus(t, rec, http.StatusOK)
	body := decodeBody[projectListResponse](t, rec)
	if len(body.Projects) != 1 || body.Projects[0].Key != "mine-proj" {
		t.Fatalf("projects = %#v, want only the visible node's project", body.Projects)
	}
	if len(body.Projects[0].Nodes) != 1 || body.Projects[0].Nodes[0].NodeID != mine.ID {
		t.Fatalf("nodes = %#v, want %s", body.Projects[0].Nodes, mine.ID)
	}

	// 全局缓存里别人的行必须原样保留：可见性在读取时施加，不能顺手删数据。
	index, err := env.store.ListProjectIndex(ctx)
	if err != nil {
		t.Fatalf("list index: %v", err)
	}
	if len(index) != 2 {
		t.Fatalf("index rows = %d, want both rows kept", len(index))
	}

	// 部署级身份（local / admin_token）仍能看到全部节点的索引。
	for _, principal := range []authz.Principal{
		{Kind: authz.KindLocal, Role: roleAdmin},
		{Kind: authz.KindAdminToken, Role: roleAdmin},
	} {
		all := decodeBody[projectListResponse](t,
			doJSONAsRole(t, env, principal, http.MethodGet, "/api/projects", ""))
		keys := map[string]bool{}
		for _, project := range all.Projects {
			keys[project.Key] = true
		}
		if !keys["mine-proj"] || !keys["foreign-proj"] {
			t.Fatalf("%s projects = %#v, want both keys", principal.Kind, all.Projects)
		}
	}
}

// TestSetPrimaryNodeRejectsForeignNode 固化节点存在性不泄漏：给别人的节点设置
// 主节点，与设置一个不存在的节点必须得到逐字节相同的 404。
func TestSetPrimaryNodeRejectsForeignNode(t *testing.T) {
	env := newTestEnv(t, 2*time.Second)
	owner := createTestUser(t, env, "owner@x.c", testLoginPassword, "user", "active")
	stranger := createTestUser(t, env, "stranger@x.c", testLoginPassword, "user", "active")

	mine, _, _ := env.addNode("a-mine")
	foreign, _, _ := env.addNode("b-foreign")
	mustAddMember(t, env, mine.ID, owner.ID, store.NodeRoleOwner)
	mustAddMember(t, env, foreign.ID, stranger.ID, store.NodeRoleOwner)

	// 项目索引必须对调用方可见，否则拒绝原因会退化成「项目不存在」而测不到节点授权。
	ctx := context.Background()
	if err := env.store.UpsertProjectIndex(ctx, []store.ProjectIndex{{
		ProjectKey: "abc",
		NodeID:     mine.ID,
		ProjectID:  "abc",
		Canonical:  "/w/repo",
	}}); err != nil {
		t.Fatalf("prime index: %v", err)
	}

	foreignRec := doJSONAs(t, env, owner.ID, http.MethodPut, "/api/projects/abc/primary-node",
		`{"node_id":"`+foreign.ID+`"}`)
	requireStatus(t, foreignRec, http.StatusNotFound)
	if got := foreignRec.Body.String(); got != foreignNotFoundBody {
		t.Fatalf("foreign node body = %q, want %q", got, foreignNotFoundBody)
	}

	missingRec := doJSONAs(t, env, owner.ID, http.MethodPut, "/api/projects/abc/primary-node",
		`{"node_id":"node_missing"}`)
	requireStatus(t, missingRec, http.StatusNotFound)
	if missingRec.Body.String() != foreignRec.Body.String() {
		t.Fatalf("missing node body %q differs from foreign node body %q",
			missingRec.Body.String(), foreignRec.Body.String())
	}

	// 被拒绝的写入不能留下任何偏好行。
	if prefs, err := env.store.ListProjectPreferencesFor(ctx, owner.ID); err != nil || len(prefs) != 0 {
		t.Fatalf("preferences after rejection = %#v, %v", prefs, err)
	}
}

// TestSetPrimaryNodePersistsPerAccount 固化主节点偏好写进调用方自己的命名空间，
// 另一个账号的同名项目偏好不受影响。
func TestSetPrimaryNodePersistsPerAccount(t *testing.T) {
	env := newTestEnv(t, 2*time.Second)
	alice := createTestUser(t, env, "alice@x.c", testLoginPassword, "user", "active")
	bob := createTestUser(t, env, "bob@x.c", testLoginPassword, "user", "active")

	node, _, _ := env.addNode("shared")
	mustAddMember(t, env, node.ID, alice.ID, store.NodeRoleOwner)
	mustAddMember(t, env, node.ID, bob.ID, store.NodeRoleMember)

	ctx := context.Background()
	if err := env.store.UpsertProjectIndex(ctx, []store.ProjectIndex{{
		ProjectKey: "abc",
		NodeID:     node.ID,
		ProjectID:  "abc",
		Canonical:  "/w/repo",
	}}); err != nil {
		t.Fatalf("prime index: %v", err)
	}

	rec := doJSONAs(t, env, alice.ID, http.MethodPut, "/api/projects/abc/primary-node",
		`{"node_id":"`+node.ID+`"}`)
	requireStatus(t, rec, http.StatusNoContent)

	alicePrefs, err := env.store.ListProjectPreferencesFor(ctx, alice.ID)
	if err != nil {
		t.Fatalf("list alice preferences: %v", err)
	}
	if len(alicePrefs) != 1 || alicePrefs[0].ProjectKey != "abc" || alicePrefs[0].PrimaryNodeID != node.ID {
		t.Fatalf("alice preferences = %#v", alicePrefs)
	}

	bobPrefs, err := env.store.ListProjectPreferencesFor(ctx, bob.ID)
	if err != nil {
		t.Fatalf("list bob preferences: %v", err)
	}
	if len(bobPrefs) != 0 {
		t.Fatalf("bob preferences = %#v, want none", bobPrefs)
	}

	// local 的历史命名空间也不能被这次写入污染。
	legacy, err := env.store.ListProjectPreferences(ctx)
	if err != nil {
		t.Fatalf("list legacy preferences: %v", err)
	}
	if len(legacy) != 0 {
		t.Fatalf("legacy preferences = %#v, want none", legacy)
	}

	// 读路径同样按账号取偏好：bob 的聚合里不应出现 alice 设置的主节点。
	env.markNodeOffline(node.ID)
	bobView := decodeBody[projectListResponse](t,
		doJSONAs(t, env, bob.ID, http.MethodGet, "/api/projects", ""))
	if len(bobView.Projects) != 1 {
		t.Fatalf("bob projects = %#v, want one project", bobView.Projects)
	}
	if bobView.Projects[0].PrimaryNodeID != "" {
		t.Fatalf("bob saw a foreign primary node: %#v", bobView.Projects[0])
	}
}

func TestCreateProjectForwardsTrimmedName(t *testing.T) {
	env := newTestEnv(t, 2*time.Second)
	node, agent, conn := env.addNode("node-a")
	reply := replyOK(t, agent, conn, `{"canonical":"/w/新项目"}`)

	rec := env.do(http.MethodPost, "/api/nodes/"+node.ID+"/projects", `{"name":"  新项目  "}`)
	requireStatus(t, rec, http.StatusOK)

	frame := waitFrame(t, reply)
	if frame.Method != "project.create" {
		t.Fatalf("method = %q, want project.create", frame.Method)
	}
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(frame.Params, &params); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if params.Name != "新项目" {
		t.Fatalf("name = %q, want trimmed", params.Name)
	}

	body := decodeBody[struct {
		Canonical string `json:"canonical"`
	}](t, rec)
	if body.Canonical != "/w/新项目" {
		t.Fatalf("canonical = %q", body.Canonical)
	}
}

func TestCreateProjectRejectsEmptyName(t *testing.T) {
	env := newTestEnv(t, 2*time.Second)
	node, agent, _ := env.addNode("node-a")

	rec := env.do(http.MethodPost, "/api/nodes/"+node.ID+"/projects", `{"name":"   "}`)
	requireStatus(t, rec, http.StatusBadRequest)
	if len(agent.frames) != 0 {
		t.Fatalf("agent received %d requests, want none", len(agent.frames))
	}
}

func TestCreateProjectMapsAgentError(t *testing.T) {
	env := newTestEnv(t, 2*time.Second)
	node, agent, conn := env.addNode("node-a")
	reply := replyErr(t, agent, conn, "invalid_params", "project.create: directory already exists")

	rec := env.do(http.MethodPost, "/api/nodes/"+node.ID+"/projects", `{"name":"demo"}`)
	requireStatus(t, rec, http.StatusBadGateway)
	waitFrame(t, reply)

	body := decodeBody[apiError](t, rec)
	if body.Code != "agent_error" || body.Message != "project.create: directory already exists" {
		t.Fatalf("error = %#v", body)
	}
}
