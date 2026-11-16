package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/insmtx/lework/backend/internal/controlplane/store"
)

// TestRenameNodeAliasOnlyAffectsCaller 固化「改名只管自己」：PATCH 后调用者
// 在列表、单节点与总览里看到新名字，同一节点的其他成员仍看到 agent 主机名。
func TestRenameNodeAliasOnlyAffectsCaller(t *testing.T) {
	env := newTestEnv(t, time.Second)
	owner := createTestUser(t, env, "owner@example.com", testLoginPassword, roleUser, statusActive)
	member := createTestUser(t, env, "member@example.com", testLoginPassword, roleUser, statusActive)
	rec, _, _ := env.addNode("host-a")
	mustAddMember(t, env, rec.ID, owner.ID, store.NodeRoleOwner)
	mustAddMember(t, env, rec.ID, member.ID, store.NodeRoleMember)

	patched := doJSONAsAccount(t, env, owner.ID, roleUser, http.MethodPatch,
		"/api/nodes/"+rec.ID, `{"name":"客厅的电脑"}`)
	requireStatus(t, patched, http.StatusOK)
	if got := decodeBody[nodeResponse](t, patched).Name; got != "客厅的电脑" {
		t.Fatalf("patched name = %q, want the alias", got)
	}

	ownerList := decodeBody[[]nodeResponse](t, doJSONAsAccount(t, env, owner.ID, roleUser,
		http.MethodGet, "/api/nodes", ""))
	if len(ownerList) != 1 || ownerList[0].Name != "客厅的电脑" {
		t.Fatalf("owner list = %+v, want aliased node", ownerList)
	}

	ownerNode := decodeBody[nodeResponse](t, doJSONAsAccount(t, env, owner.ID, roleUser,
		http.MethodGet, "/api/nodes/"+rec.ID, ""))
	if ownerNode.Name != "客厅的电脑" {
		t.Fatalf("owner node name = %q, want alias", ownerNode.Name)
	}

	memberList := decodeBody[[]nodeResponse](t, doJSONAsAccount(t, env, member.ID, roleUser,
		http.MethodGet, "/api/nodes", ""))
	if len(memberList) != 1 || memberList[0].Name != "host-a" {
		t.Fatalf("member list = %+v, want the reported hostname", memberList)
	}

	ownerOverview := decodeBody[overviewResponse](t, doJSONAsAccount(t, env, owner.ID, roleUser,
		http.MethodGet, "/api/overview", ""))
	if len(ownerOverview.Nodes) != 1 || ownerOverview.Nodes[0].Name != "客厅的电脑" {
		t.Fatalf("owner overview = %+v, want aliased node", ownerOverview.Nodes)
	}
	memberOverview := decodeBody[overviewResponse](t, doJSONAsAccount(t, env, member.ID, roleUser,
		http.MethodGet, "/api/overview", ""))
	if len(memberOverview.Nodes) != 1 || memberOverview.Nodes[0].Name != "host-a" {
		t.Fatalf("member overview = %+v, want the reported hostname", memberOverview.Nodes)
	}
}

// TestRenameNodeBlankRestoresHostname 保证留空提交表示清掉自己的别名。
func TestRenameNodeBlankRestoresHostname(t *testing.T) {
	env := newTestEnv(t, time.Second)
	owner := createTestUser(t, env, "owner@example.com", testLoginPassword, roleUser, statusActive)
	rec, _, _ := env.addNode("host-a")
	mustAddMember(t, env, rec.ID, owner.ID, store.NodeRoleOwner)

	doJSONAsAccount(t, env, owner.ID, roleUser, http.MethodPatch,
		"/api/nodes/"+rec.ID, `{"name":"客厅的电脑"}`)

	cleared := doJSONAsAccount(t, env, owner.ID, roleUser, http.MethodPatch,
		"/api/nodes/"+rec.ID, `{"name":"  "}`)
	requireStatus(t, cleared, http.StatusOK)
	if got := decodeBody[nodeResponse](t, cleared).Name; got != "host-a" {
		t.Fatalf("name after clear = %q, want reported hostname", got)
	}
}

// TestRenameNodeRejectsInvalidRequests 覆盖长度与鉴权边界：过长名字是调用方
// 错误，非成员与不存在的节点共用逐字节一致的 404。
func TestRenameNodeRejectsInvalidRequests(t *testing.T) {
	env := newTestEnv(t, time.Second)
	owner := createTestUser(t, env, "owner@example.com", testLoginPassword, roleUser, statusActive)
	stranger := createTestUser(t, env, "stranger@example.com", testLoginPassword, roleUser, statusActive)
	rec, _, _ := env.addNode("host-a")
	mustAddMember(t, env, rec.ID, owner.ID, store.NodeRoleOwner)

	tooLong := `{"name":"` + strings.Repeat("a", 65) + `"}`
	requireErrorCode(t, doJSONAsAccount(t, env, owner.ID, roleUser, http.MethodPatch,
		"/api/nodes/"+rec.ID, tooLong), http.StatusBadRequest, "invalid_request")

	requireErrorCode(t, doJSONAsAccount(t, env, stranger.ID, roleUser, http.MethodPatch,
		"/api/nodes/"+rec.ID, `{"name":"stolen"}`), http.StatusNotFound, "not_found")
}
