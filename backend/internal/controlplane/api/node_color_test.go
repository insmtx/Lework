package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/insmtx/lework/backend/internal/controlplane/store"
)

// TestUpdateNodeColorOnlyAffectsCaller 固化「颜色只管自己」：PATCH 后调用者
// 在列表与单节点里看到颜色，同一节点的其他成员仍是无色。
func TestUpdateNodeColorOnlyAffectsCaller(t *testing.T) {
	env := newTestEnv(t, time.Second)
	owner := createTestUser(t, env, "owner@example.com", testLoginPassword, roleUser, statusActive)
	member := createTestUser(t, env, "member@example.com", testLoginPassword, roleUser, statusActive)
	rec, _, _ := env.addNode("host-a")
	mustAddMember(t, env, rec.ID, owner.ID, store.NodeRoleOwner)
	mustAddMember(t, env, rec.ID, member.ID, store.NodeRoleMember)

	patched := doJSONAsAccount(t, env, owner.ID, roleUser, http.MethodPatch,
		"/api/nodes/"+rec.ID, `{"color":"  Blue  "}`)
	requireStatus(t, patched, http.StatusOK)
	if got := decodeBody[nodeResponse](t, patched).Color; got != "blue" {
		t.Fatalf("patched color = %q, want normalized blue", got)
	}

	ownerList := decodeBody[[]nodeResponse](t, doJSONAsAccount(t, env, owner.ID, roleUser,
		http.MethodGet, "/api/nodes", ""))
	if len(ownerList) != 1 || ownerList[0].Color != "blue" {
		t.Fatalf("owner list = %+v, want colored node", ownerList)
	}

	ownerNode := decodeBody[nodeResponse](t, doJSONAsAccount(t, env, owner.ID, roleUser,
		http.MethodGet, "/api/nodes/"+rec.ID, ""))
	if ownerNode.Color != "blue" {
		t.Fatalf("owner node color = %q, want blue", ownerNode.Color)
	}

	memberList := decodeBody[[]nodeResponse](t, doJSONAsAccount(t, env, member.ID, roleUser,
		http.MethodGet, "/api/nodes", ""))
	if len(memberList) != 1 || memberList[0].Color != "" || memberList[0].Name != "host-a" {
		t.Fatalf("member list = %+v, want uncolored reported hostname", memberList)
	}
}

// TestUpdateNodeKeepsNameAndColorIndependent 保证两个字段互不覆盖：
// 只提颜色不丢别名，只提名字不丢颜色，空颜色可清除。
func TestUpdateNodeKeepsNameAndColorIndependent(t *testing.T) {
	env := newTestEnv(t, time.Second)
	owner := createTestUser(t, env, "owner@example.com", testLoginPassword, roleUser, statusActive)
	rec, _, _ := env.addNode("host-a")
	mustAddMember(t, env, rec.ID, owner.ID, store.NodeRoleOwner)

	doJSONAsAccount(t, env, owner.ID, roleUser, http.MethodPatch,
		"/api/nodes/"+rec.ID, `{"name":"客厅的电脑","color":"blue"}`)

	afterColor := decodeBody[nodeResponse](t, doJSONAsAccount(t, env, owner.ID, roleUser,
		http.MethodPatch, "/api/nodes/"+rec.ID, `{"color":"teal"}`))
	if afterColor.Name != "客厅的电脑" || afterColor.Color != "teal" {
		t.Fatalf("after color update = %+v, want alias kept and teal applied", afterColor)
	}

	afterName := decodeBody[nodeResponse](t, doJSONAsAccount(t, env, owner.ID, roleUser,
		http.MethodPatch, "/api/nodes/"+rec.ID, `{"name":"书房"}`))
	if afterName.Name != "书房" || afterName.Color != "teal" {
		t.Fatalf("after name update = %+v, want color kept and name applied", afterName)
	}

	cleared := decodeBody[nodeResponse](t, doJSONAsAccount(t, env, owner.ID, roleUser,
		http.MethodPatch, "/api/nodes/"+rec.ID, `{"color":""}`))
	if cleared.Color != "" || cleared.Name != "书房" {
		t.Fatalf("after clear = %+v, want empty color and kept name", cleared)
	}
}

// TestUpdateNodeRejectsInvalidColor 覆盖格式、长度与空请求；同时验证非法
// 颜色不会让同一次请求里的名字被部分写入。
func TestUpdateNodeRejectsInvalidColor(t *testing.T) {
	env := newTestEnv(t, time.Second)
	owner := createTestUser(t, env, "owner@example.com", testLoginPassword, roleUser, statusActive)
	rec, _, _ := env.addNode("host-a")
	mustAddMember(t, env, rec.ID, owner.ID, store.NodeRoleOwner)

	for _, body := range []string{
		`{"color":"blue;drop"}`,
		`{"color":"` + strings.Repeat("a", 25) + `"}`,
		`{}`,
	} {
		requireErrorCode(t, doJSONAsAccount(t, env, owner.ID, roleUser, http.MethodPatch,
			"/api/nodes/"+rec.ID, body), http.StatusBadRequest, "invalid_request")
	}

	requireErrorCode(t, doJSONAsAccount(t, env, owner.ID, roleUser, http.MethodPatch,
		"/api/nodes/"+rec.ID, `{"name":"客厅的电脑","color":"bad!"}`),
		http.StatusBadRequest, "invalid_request")

	view := decodeBody[nodeResponse](t, doJSONAsAccount(t, env, owner.ID, roleUser,
		http.MethodGet, "/api/nodes/"+rec.ID, ""))
	if view.Name != "host-a" {
		t.Fatalf("name after rejected request = %q, want reported hostname", view.Name)
	}
}
