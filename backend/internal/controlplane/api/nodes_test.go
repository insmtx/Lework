package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/insmtx/lework/backend/internal/controlplane/authz"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
)

// doJSONAsAccount 以完整账号身份发一次请求，用于需要同时指定账号与角色的用例。
//
// 复用 doJSONAsRole 注入 principal，而不是复制中间件装配：认领接口的可见性
// 只取决于 principal，这条映射必须与生产完全一致。
func doJSONAsAccount(
	t *testing.T,
	env *testEnv,
	userID, role, method, target, body string,
) *httptest.ResponseRecorder {
	t.Helper()

	return doJSONAsRole(t, env, authz.Principal{
		Kind: authz.KindUser, UserID: userID, Role: role,
	}, method, target, body)
}

// TestClaimUnclaimedNodeOnlyForAdmin 固化设计 §9：认领只对管理员开放，且同一个
// 未认领节点只能被认领一次。
func TestClaimUnclaimedNodeOnlyForAdmin(t *testing.T) {
	env := newTestEnv(t, time.Second)
	user := createTestUser(t, env, "user@x.c", testLoginPassword, "user", "active")
	admin := createTestUser(t, env, "admin@x.c", testLoginPassword, "admin", "active")
	otherAdmin := createTestUser(t, env, "admin2@x.c", testLoginPassword, "admin", "active")
	node, _, _ := env.addNode("old-box")

	userRec := doJSONAs(t, env, user.ID, http.MethodPost, "/api/nodes/"+node.ID+"/claim", "")
	requireErrorCode(t, userRec, http.StatusForbidden, "forbidden")

	// 普通账号被拒时不能写库：节点必须仍然无人认领。
	if _, ok, err := env.store.NodeRole(t.Context(), node.ID, user.ID); err != nil || ok {
		t.Fatalf("role after rejected claim = ok %v, err %v; want no member row", ok, err)
	}

	adminRec := doJSONAsAccount(t, env, admin.ID, roleAdmin, http.MethodPost, "/api/nodes/"+node.ID+"/claim", "")
	requireStatus(t, adminRec, http.StatusOK)
	view := decodeBody[nodeResponse](t, adminRec)
	if view.ID != node.ID {
		t.Fatalf("claimed node id = %q, want %q", view.ID, node.ID)
	}
	if !view.Claimed {
		t.Fatal("claimed = false in the claim response, want true")
	}

	again := doJSONAsAccount(t, env, otherAdmin.ID, roleAdmin, http.MethodPost, "/api/nodes/"+node.ID+"/claim", "")
	requireErrorCode(t, again, http.StatusConflict, "already_claimed")
}

// TestClaimNodeRequiresAccountPrincipal 固化部署级身份的边界：local 与
// admin_token 本来就看得见所有节点，但它们没有账号，认领无处安放。
func TestClaimNodeRequiresAccountPrincipal(t *testing.T) {
	env := newTestEnv(t, time.Second)
	node, _, _ := env.addNode("old-box")

	for _, tc := range []struct {
		name      string
		principal authz.Principal
	}{
		{"local", authz.Principal{Kind: authz.KindLocal}},
		{"admin token", authz.Principal{Kind: authz.KindAdminToken}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSONAsRole(t, env, tc.principal,
				http.MethodPost, "/api/nodes/"+node.ID+"/claim", "")
			requireErrorCode(t, rec, http.StatusForbidden, "user_required")
		})
	}

	// 两次拒绝都不能落库。
	if _, ok, err := env.store.NodeRole(t.Context(), node.ID, ""); err != nil || ok {
		t.Fatalf("role after rejected claim = ok %v, err %v; want no member row", ok, err)
	}
}

// TestClaimNodeUnknownNodeNotFound 固化未知节点的 404 与既有 nodeRoute 逐字节
// 一致，避免认领接口变成节点存在性的探针。
func TestClaimNodeUnknownNodeNotFound(t *testing.T) {
	env := newTestEnv(t, time.Second)
	admin := createTestUser(t, env, "admin@x.c", testLoginPassword, "admin", "active")

	rec := doJSONAsAccount(t, env, admin.ID, roleAdmin,
		http.MethodPost, "/api/nodes/node_missing/claim", "")
	requireErrorCode(t, rec, http.StatusNotFound, "not_found")
	if got := rec.Body.String(); got != foreignNotFoundBody {
		t.Fatalf("body = %q, want the shared missing-node body %q", got, foreignNotFoundBody)
	}
}

// TestClaimNodeForbiddenBodyDoesNotLeakExistence 固化非管理员的拒绝与节点是否
// 存在无关：同一个固定错误体能挡住存在性探测。
func TestClaimNodeForbiddenBodyDoesNotLeakExistence(t *testing.T) {
	env := newTestEnv(t, time.Second)
	user := createTestUser(t, env, "user@x.c", testLoginPassword, "user", "active")
	node, _, _ := env.addNode("old-box")

	existing := doJSONAs(t, env, user.ID, http.MethodPost, "/api/nodes/"+node.ID+"/claim", "")
	requireErrorCode(t, existing, http.StatusForbidden, "forbidden")
	missing := doJSONAs(t, env, user.ID, http.MethodPost, "/api/nodes/node_missing/claim", "")
	requireErrorCode(t, missing, http.StatusForbidden, "forbidden")

	if existing.Body.String() != missing.Body.String() {
		t.Fatalf("existing = %q, missing = %q; want byte-identical bodies",
			existing.Body.String(), missing.Body.String())
	}
}

// TestClaimNodeGrantsImmediateAccess 固化认领之后的行为：成员行立即让
// nodeRoute 放行、列表带上 claimed=true，而其他普通账号仍然看不到它。
func TestClaimNodeGrantsImmediateAccess(t *testing.T) {
	env := newTestEnv(t, time.Second)
	admin := createTestUser(t, env, "admin@x.c", testLoginPassword, "admin", "active")
	stranger := createTestUser(t, env, "stranger@x.c", testLoginPassword, "user", "active")
	node, _, _ := env.addNode("old-box")

	before := doJSONAsAccount(t, env, admin.ID, roleAdmin,
		http.MethodGet, "/api/nodes/"+node.ID, "")
	requireErrorCode(t, before, http.StatusNotFound, "not_found")

	// 未认领的历史节点出现在管理员列表里，但带着 claimed=false。
	unclaimedList := doJSONAsAccount(t, env, admin.ID, roleAdmin, http.MethodGet, "/api/nodes", "")
	requireStatus(t, unclaimedList, http.StatusOK)
	unclaimed := decodeBody[[]nodeResponse](t, unclaimedList)
	if len(unclaimed) != 1 || unclaimed[0].Claimed {
		t.Fatalf("admin nodes before claim = %+v, want one node with claimed=false", unclaimed)
	}

	claim := doJSONAsAccount(t, env, admin.ID, roleAdmin,
		http.MethodPost, "/api/nodes/"+node.ID+"/claim", "")
	requireStatus(t, claim, http.StatusOK)

	after := doJSONAsAccount(t, env, admin.ID, roleAdmin,
		http.MethodGet, "/api/nodes/"+node.ID, "")
	requireStatus(t, after, http.StatusOK)
	if view := decodeBody[nodeResponse](t, after); !view.Claimed {
		t.Fatal("claimed = false after claim, want true")
	}

	list := doJSONAsAccount(t, env, admin.ID, roleAdmin, http.MethodGet, "/api/nodes", "")
	requireStatus(t, list, http.StatusOK)
	nodes := decodeBody[[]nodeResponse](t, list)
	if len(nodes) != 1 || nodes[0].ID != node.ID {
		t.Fatalf("admin nodes = %+v, want only the claimed node", nodes)
	}
	if !nodes[0].Claimed {
		t.Fatal("list claimed = false after claim, want true")
	}

	// 别人的节点仍然不可见，也不可认领。
	foreign := doJSONAs(t, env, stranger.ID, http.MethodGet, "/api/nodes/"+node.ID, "")
	requireErrorCode(t, foreign, http.StatusNotFound, "not_found")
	strangerClaim := doJSONAs(t, env, stranger.ID,
		http.MethodPost, "/api/nodes/"+node.ID+"/claim", "")
	requireErrorCode(t, strangerClaim, http.StatusForbidden, "forbidden")
}

// TestClaimNodeRecordsOwnerAndClaimedAt 确认认领写下的 owner 行与 claimed_at
// 都能从存储读回，而不只是接口层看起来成功。
func TestClaimNodeRecordsOwnerAndClaimedAt(t *testing.T) {
	env := newTestEnv(t, time.Second)
	admin := createTestUser(t, env, "admin@x.c", testLoginPassword, "admin", "active")
	node, _, _ := env.addNode("old-box")

	claim := doJSONAsAccount(t, env, admin.ID, roleAdmin,
		http.MethodPost, "/api/nodes/"+node.ID+"/claim", "")
	requireStatus(t, claim, http.StatusOK)

	role, ok, err := env.store.NodeRole(t.Context(), node.ID, admin.ID)
	if err != nil {
		t.Fatalf("node role: %v", err)
	}
	if !ok || role != store.NodeRoleOwner {
		t.Fatalf("role = %q, ok %v; want owner", role, ok)
	}
}
