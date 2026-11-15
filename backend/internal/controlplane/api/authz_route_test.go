package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/insmtx/lework/backend/internal/controlplane/authz"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
)

// doJSONAs 以账号身份发一次请求，模拟中间件已经解析出 user principal。
//
// 角色固定为普通用户：需要管理员角色或部署级身份的用例用 doJSONAsRole 显式
// 声明，避免「测试里随便一个账号就能看到全部节点」把授权断言架空。
func doJSONAs(
	t *testing.T,
	env *testEnv,
	userID, method, target, body string,
) *httptest.ResponseRecorder {
	t.Helper()

	return env.doAs(
		authz.Principal{Kind: authz.KindUser, UserID: userID, Role: roleUser},
		method, target, body,
	)
}

// doJSONAsRole 按完整 principal 发请求，用于覆盖 local 与 admin_token。
func doJSONAsRole(
	t *testing.T,
	env *testEnv,
	principal authz.Principal,
	method, target, body string,
) *httptest.ResponseRecorder {
	t.Helper()

	return env.doAs(principal, method, target, body)
}

// mustAddMember 写一条节点归属行；失败直接终止用例，避免授权断言在空归属下
// 变成「谁都不能访问」的假通过。
func mustAddMember(t *testing.T, env *testEnv, nodeID, userID, role string) {
	t.Helper()

	if err := env.store.AddNodeMember(context.Background(), store.NodeMember{
		NodeID: nodeID,
		UserID: userID,
		Role:   role,
	}); err != nil {
		t.Fatalf("add member %q to node %q: %v", userID, nodeID, err)
	}
}

// foreignNotFoundBody 是不存在节点与无权节点共用的 404 响应体。
const foreignNotFoundBody = "{\"code\":\"not_found\",\"message\":\"node not found\"}\n"

// TestNodeRouteHidesForeignNodesAsNotFound 固化设计 §9 的核心约束：非成员看到
// 的 404 必须与「节点不存在」逐字节一致，否则状态码/文案就成了节点存在性的
// 探测器。
func TestNodeRouteHidesForeignNodesAsNotFound(t *testing.T) {
	env := newTestEnv(t, time.Second)
	owner := createTestUser(t, env, "u1@x.c", testLoginPassword, "user", "active")
	stranger := createTestUser(t, env, "u2@x.c", testLoginPassword, "user", "active")
	node, agent, _ := env.addNode("n1")
	mustAddMember(t, env, node.ID, owner.ID, store.NodeRoleOwner)

	foreign := doJSONAs(t, env, stranger.ID, http.MethodGet, "/api/nodes/"+node.ID, "")
	requireStatus(t, foreign, http.StatusNotFound)
	if got := foreign.Body.String(); got != foreignNotFoundBody {
		t.Fatalf("foreign node body = %q, want %q", got, foreignNotFoundBody)
	}

	// 与「真的不存在」的节点对比：两者必须不可区分。
	missing := doJSONAsRole(t, env, authz.Principal{Kind: authz.KindLocal},
		http.MethodGet, "/api/nodes/node_missing", "")
	requireStatus(t, missing, http.StatusNotFound)
	if missing.Body.String() != foreign.Body.String() {
		t.Fatalf("foreign body %q differs from missing body %q",
			foreign.Body.String(), missing.Body.String())
	}

	ok := doJSONAs(t, env, owner.ID, http.MethodGet, "/api/nodes/"+node.ID, "")
	requireStatus(t, ok, http.StatusOK)

	if len(agent.frames) != 0 {
		t.Fatalf("agent received %d requests for a hidden node, want none", len(agent.frames))
	}
}

// TestNodeRoutesAuthorizeEveryRouteFamily 覆盖所有节点维度路由族：非成员一律
// 404，且请求不会到达节点（agent 一帧都不该收到）。
func TestNodeRoutesAuthorizeEveryRouteFamily(t *testing.T) {
	env := newTestEnv(t, time.Second)
	owner := createTestUser(t, env, "owner@x.c", testLoginPassword, "user", "active")
	stranger := createTestUser(t, env, "stranger@x.c", testLoginPassword, "user", "active")
	node, agent, conn := env.addNode("n1")
	mustAddMember(t, env, node.ID, owner.ID, store.NodeRoleOwner)

	routes := []struct {
		method string
		target string
	}{
		{http.MethodGet, "/api/nodes/" + node.ID},
		{http.MethodGet, "/api/nodes/" + node.ID + "/health"},
		{http.MethodPost, "/api/nodes/" + node.ID + "/opencode/start"},
		{http.MethodPost, "/api/nodes/" + node.ID + "/opencode/restart"},
		{http.MethodPost, "/api/nodes/" + node.ID + "/opencode/stop"},
		{http.MethodGet, "/api/nodes/" + node.ID + "/files?directory=%2Fwork"},
		{http.MethodGet, "/api/nodes/" + node.ID + "/files/content?directory=%2Fwork&path=a.txt"},
		{http.MethodGet, "/api/nodes/" + node.ID + "/projects"},
		{http.MethodPatch, "/api/sessions/" + node.ID + "/ses_1"},
		{http.MethodDelete, "/api/sessions/" + node.ID + "/ses_1"},
		{http.MethodGet, "/api/sessions/" + node.ID + "/ses_1/messages"},
		{http.MethodPost, "/api/sessions/" + node.ID + "/ses_1/prompt"},
		{http.MethodGet, "/api/sessions/" + node.ID + "/ses_1/diff?directory=%2Fwork"},
		{http.MethodPost, "/api/sessions/" + node.ID + "/ses_1/interrupt"},
		{http.MethodPost, "/api/sessions/" + node.ID + "/ses_1/pty"},
		{http.MethodDelete, "/api/sessions/" + node.ID + "/ses_1/pty/pty_1"},
		{http.MethodPost, "/api/sessions/" + node.ID + "/ses_1/permissions/perm_1/reply"},
		{http.MethodGet, "/api/sessions/" + node.ID + "/ses_1/permissions"},
	}

	for _, route := range routes {
		t.Run(route.method+" "+route.target, func(t *testing.T) {
			rec := doJSONAs(t, env, stranger.ID, route.method, route.target, "")
			requireStatus(t, rec, http.StatusNotFound)
			if got := rec.Body.String(); got != foreignNotFoundBody {
				t.Fatalf("body = %q, want %q", got, foreignNotFoundBody)
			}
		})
	}

	if len(agent.frames) != 0 {
		t.Fatalf("agent received %d requests for hidden routes, want none", len(agent.frames))
	}

	// 成员（owner 与 member）都能用同一批路由：拒绝是因为没有归属，而不是
	// 因为路由本身对账号永久关闭。
	member := createTestUser(t, env, "member@x.c", testLoginPassword, "user", "active")
	mustAddMember(t, env, node.ID, member.ID, store.NodeRoleMember)

	for _, userID := range []string{owner.ID, member.ID} {
		rec := doJSONAs(t, env, userID, http.MethodGet, "/api/nodes/"+node.ID, "")
		requireStatus(t, rec, http.StatusOK)
	}

	got := replyOK(t, agent, conn, `[{"id":"ses_1"}]`)
	memberMessages := doJSONAs(t, env, member.ID, http.MethodGet,
		"/api/sessions/"+node.ID+"/ses_1/messages", "")
	requireStatus(t, memberMessages, http.StatusOK)
	waitFrame(t, got)
}

// TestNodeRoutesFailClosedWithoutPrincipal 固化「缺身份不等于管理员」：直接调用
// handler（没有中间件）时零值 principal 必须被拒绝，而不是退化成匿名放行。
func TestNodeRoutesFailClosedWithoutPrincipal(t *testing.T) {
	env := newTestEnv(t, time.Second)
	node, _, _ := env.addNode("n1")

	rec := doJSONAsRole(t, env, authz.Principal{}, http.MethodGet, "/api/nodes/"+node.ID, "")
	requireErrorCode(t, rec, http.StatusNotFound, "not_found")
}

// TestListNodesFiltersByMembership 覆盖列表投影：普通账号只看到自己的节点，
// local 仍看到全部；过滤不能改变既有顺序。
func TestListNodesFiltersByMembership(t *testing.T) {
	env := newTestEnv(t, time.Second)
	mine := createTestUser(t, env, "mine@x.c", testLoginPassword, "user", "active")
	theirs := createTestUser(t, env, "theirs@x.c", testLoginPassword, "user", "active")

	ownA, _, _ := env.addNode("a-own")
	ownB, _, _ := env.addNode("b-own")
	foreign, _, _ := env.addNode("c-foreign")
	mustAddMember(t, env, ownA.ID, mine.ID, store.NodeRoleOwner)
	mustAddMember(t, env, ownB.ID, mine.ID, store.NodeRoleMember)
	mustAddMember(t, env, foreign.ID, theirs.ID, store.NodeRoleOwner)

	rec := doJSONAs(t, env, mine.ID, http.MethodGet, "/api/nodes", "")
	requireStatus(t, rec, http.StatusOK)
	nodes := decodeBody[[]nodeResponse](t, rec)
	if len(nodes) != 2 {
		t.Fatalf("nodes = %d, want the two member nodes", len(nodes))
	}
	if nodes[0].ID != ownA.ID || nodes[1].ID != ownB.ID {
		t.Fatalf("nodes = %s, %s; want %s, %s", nodes[0].ID, nodes[1].ID, ownA.ID, ownB.ID)
	}

	// 没有任何归属的账号必须拿到 []，而不是全量列表。
	stranger := createTestUser(t, env, "new@x.c", testLoginPassword, "user", "active")
	empty := doJSONAs(t, env, stranger.ID, http.MethodGet, "/api/nodes", "")
	requireStatus(t, empty, http.StatusOK)
	if got := strings.TrimSpace(empty.Body.String()); got != "[]" {
		t.Fatalf("empty visible list body = %q, want []", got)
	}

	deployment := doJSONAsRole(t, env, authz.Principal{Kind: authz.KindLocal},
		http.MethodGet, "/api/nodes", "")
	requireStatus(t, deployment, http.StatusOK)
	if all := decodeBody[[]nodeResponse](t, deployment); len(all) != 3 {
		t.Fatalf("local nodes = %d, want all 3", len(all))
	}
}

// TestAdminRoleSeesUnclaimedButNotForeignClaimed 固化设计 §9 的边界：管理员角色
// 可以列出「未认领」的历史节点，但永远看不到别人已认领的节点。
func TestAdminRoleSeesUnclaimedButNotForeignClaimed(t *testing.T) {
	env := newTestEnv(t, time.Second)
	admin := createTestUser(t, env, "admin@x.c", testLoginPassword, "admin", "active")
	other := createTestUser(t, env, "other@x.c", testLoginPassword, "user", "active")

	unclaimed, _, _ := env.addNode("a-free")
	own, _, _ := env.addNode("b-mine")
	theirs, _, _ := env.addNode("c-theirs")
	mustAddMember(t, env, own.ID, admin.ID, store.NodeRoleOwner)
	mustAddMember(t, env, theirs.ID, other.ID, store.NodeRoleOwner)

	rec := doJSONAsRole(t, env, authz.Principal{
		Kind: authz.KindUser, UserID: admin.ID, Role: roleAdmin,
	}, http.MethodGet, "/api/nodes", "")
	requireStatus(t, rec, http.StatusOK)
	nodes := decodeBody[[]nodeResponse](t, rec)
	if len(nodes) != 2 {
		t.Fatalf("admin nodes = %d, want own + unclaimed only", len(nodes))
	}
	if nodes[0].ID != unclaimed.ID || nodes[1].ID != own.ID {
		t.Fatalf("admin nodes = %s, %s; want %s, %s",
			nodes[0].ID, nodes[1].ID, unclaimed.ID, own.ID)
	}

	foreign := doJSONAsRole(t, env, authz.Principal{
		Kind: authz.KindUser, UserID: admin.ID, Role: roleAdmin,
	}, http.MethodGet, "/api/nodes/"+theirs.ID, "")
	requireErrorCode(t, foreign, http.StatusNotFound, "not_found")
}

// TestListSessionsRejectsForeignNode 覆盖 node 只在 query 里的列表端点。
func TestListSessionsRejectsForeignNode(t *testing.T) {
	env := newTestEnv(t, time.Second)
	owner := createTestUser(t, env, "owner@x.c", testLoginPassword, "user", "active")
	stranger := createTestUser(t, env, "stranger@x.c", testLoginPassword, "user", "active")
	node, agent, conn := env.addNode("n1")
	mustAddMember(t, env, node.ID, owner.ID, store.NodeRoleOwner)

	rec := doJSONAs(t, env, stranger.ID, http.MethodGet, "/api/sessions?node="+node.ID, "")
	requireStatus(t, rec, http.StatusNotFound)
	if got := rec.Body.String(); got != foreignNotFoundBody {
		t.Fatalf("body = %q, want %q", got, foreignNotFoundBody)
	}
	if len(agent.frames) != 0 {
		t.Fatalf("agent received %d requests for a hidden node, want none", len(agent.frames))
	}

	// 缺少 node 仍是调用方错误，不能被授权分支吞成 404。
	missing := doJSONAs(t, env, stranger.ID, http.MethodGet, "/api/sessions", "")
	requireErrorCode(t, missing, http.StatusBadRequest, "invalid_request")

	got := replyOK(t, agent, conn, `[{"id":"ses_1"}]`)
	allowed := doJSONAs(t, env, owner.ID, http.MethodGet, "/api/sessions?node="+node.ID, "")
	requireStatus(t, allowed, http.StatusOK)
	waitFrame(t, got)
}

// TestCreateSessionRejectsForeignNode 覆盖 node 只在 body 里的创建端点。
func TestCreateSessionRejectsForeignNode(t *testing.T) {
	env := newTestEnv(t, time.Second)
	owner := createTestUser(t, env, "owner@x.c", testLoginPassword, "user", "active")
	stranger := createTestUser(t, env, "stranger@x.c", testLoginPassword, "user", "active")
	node, agent, conn := env.addNode("n1")
	mustAddMember(t, env, node.ID, owner.ID, store.NodeRoleOwner)

	rec := doJSONAs(t, env, stranger.ID, http.MethodPost, "/api/sessions",
		`{"node_id":"`+node.ID+`","directory":"/work"}`)
	requireStatus(t, rec, http.StatusNotFound)
	if got := rec.Body.String(); got != foreignNotFoundBody {
		t.Fatalf("body = %q, want %q", got, foreignNotFoundBody)
	}
	if len(agent.frames) != 0 {
		t.Fatalf("agent received %d requests for a hidden node, want none", len(agent.frames))
	}

	got := replyOK(t, agent, conn, `{"id":"ses_new"}`)
	allowed := doJSONAs(t, env, owner.ID, http.MethodPost, "/api/sessions",
		`{"node_id":"`+node.ID+`","directory":"/work"}`)
	requireStatus(t, allowed, http.StatusOK)
	waitFrame(t, got)
}

// TestOverviewFiltersForeignNodes 保证总览不会把别人节点的名字与在线状态投影
// 给当前账号。
func TestOverviewFiltersForeignNodes(t *testing.T) {
	env := newTestEnv(t, time.Second)
	mine := createTestUser(t, env, "mine@x.c", testLoginPassword, "user", "active")
	theirs := createTestUser(t, env, "theirs@x.c", testLoginPassword, "user", "active")

	own, _, _ := env.addNode("a-own")
	foreign, _, _ := env.addNode("b-foreign")
	mustAddMember(t, env, own.ID, mine.ID, store.NodeRoleOwner)
	mustAddMember(t, env, foreign.ID, theirs.ID, store.NodeRoleOwner)

	rec := doJSONAs(t, env, mine.ID, http.MethodGet, "/api/overview", "")
	requireStatus(t, rec, http.StatusOK)
	body := decodeBody[overviewResponse](t, rec)
	if len(body.Nodes) != 1 || body.Nodes[0].NodeID != own.ID {
		t.Fatalf("overview nodes = %+v, want only %s", body.Nodes, own.ID)
	}

	deployment := doJSONAsRole(t, env, authz.Principal{Kind: authz.KindLocal},
		http.MethodGet, "/api/overview", "")
	requireStatus(t, deployment, http.StatusOK)
	if all := decodeBody[overviewResponse](t, deployment); len(all.Nodes) != 2 {
		t.Fatalf("local overview nodes = %+v, want both", all.Nodes)
	}
}
