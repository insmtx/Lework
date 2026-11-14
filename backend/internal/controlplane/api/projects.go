package api

import (
	"encoding/json"
	"net/http"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/insmtx/lework/backend/internal/controlplane/store"
)

// projectNodeView 是项目在某个节点上的出现情况。
//
// 客户端用它解释项目卡为什么灰：Online 为 false 时，项目元数据来自索引，
// 会话列表不可读；LastSeen 用于排序与提示。
type projectNodeView struct {
	NodeID   string    `json:"node_id"`
	Online   bool      `json:"online"`
	LastSeen time.Time `json:"last_seen"`
}

// projectView 是浏览器看到的项目聚合视图。
//
// Key 是聚合键，不能直接当 ProjectID 用（无提交仓库的 id 都是 "global"）。
type projectView struct {
	Key           string            `json:"key"`
	ID            string            `json:"id"`
	Canonical     string            `json:"canonical"`
	VCS           string            `json:"vcs,omitempty"`
	PrimaryNodeID string            `json:"primary_node_id,omitempty"`
	Alias         string            `json:"alias,omitempty"`
	Nodes         []projectNodeView `json:"nodes"`
	LastSeen      time.Time         `json:"last_seen"`
}

// projectRecord 是 agent 的 project.list 返回项；只投影控制面需要的字段。
type projectRecord struct {
	ID        string `json:"id"`
	Canonical string `json:"canonical"`
	VCS       string `json:"vcs,omitempty"`
}

type projectListResponse struct {
	Projects []projectView `json:"projects"`
}

type nodeProjectsResponse struct {
	Projects []projectRecord `json:"projects"`
}

type setPrimaryNodeRequest struct {
	NodeID string `json:"node_id"`
}

type setPreferenceRequest struct {
	Value string `json:"value"`
}

type preferencesResponse struct {
	Values map[string]string `json:"values"`
}

// projectKeyOf 把 OpenCode 的 project id 转成控制面的聚合键。
//
// OpenCode 给所有无提交的 git 仓库都报 id "global"，直接聚合会把它们
// 混成一个项目；此时拼上 canonical 才能区分。
func projectKeyOf(id, canonical string) string {
	if id == "global" {
		return id + ":" + canonical
	}
	return id
}

// handleNodeProjects 透传单个节点的项目列表，供调试与离线诊断使用。
func (a *API) handleNodeProjects(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	if _, ok, err := a.findNode(r.Context(), nodeID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	} else if !ok {
		writeAPIError(w, http.StatusNotFound, "not_found", "node not found")
		return
	}

	raw, err := a.callAgent(r.Context(), nodeID, "project.list", nil)
	if err != nil {
		a.writeAgentError(w, err)
		return
	}
	projects := make([]projectRecord, 0)
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &projects); err != nil {
			writeAPIError(w, http.StatusBadGateway, "agent_error", "invalid project list from agent")
			return
		}
	}
	writeJSON(w, http.StatusOK, nodeProjectsResponse{Projects: projects})
}

// handleListProjects 聚合全部节点的项目。
//
// 在线节点实时拉取，失败只降级该节点；离线节点与拉取失败的节点用项目
// 索引兜底，保证重启或断网后项目卡仍在。结果写回索引失败不影响响应——
// 读接口不应因为一次缓存写失败而 500。
func (a *API) handleListProjects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	nodes, err := a.listNodes(ctx)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	index, err := a.store.ListProjectIndex(ctx)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	prefs, err := a.store.ListProjectPreferences(ctx)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}

	type nodeResult struct {
		node     nodeView
		projects []projectRecord
		ok       bool
	}
	results := make([]nodeResult, len(nodes))
	var wg sync.WaitGroup
	for i, view := range nodes {
		results[i].node = view
		if !view.Connected || !supportsMethod(view, "project.list") {
			continue
		}
		wg.Add(1)
		go func(i int, nodeID string) {
			defer wg.Done()
			raw, callErr := a.callAgent(ctx, nodeID, "project.list", nil)
			if callErr != nil {
				return
			}
			var projects []projectRecord
			if len(raw) > 0 && string(raw) != "null" {
				if err := json.Unmarshal(raw, &projects); err != nil {
					return
				}
			}
			results[i].projects = projects
			results[i].ok = true
		}(i, view.ID)
	}
	wg.Wait()

	aggregated := make(map[string]*projectView)
	seenNode := make(map[string]bool)
	online := make(map[string]bool, len(nodes))
	for _, view := range nodes {
		online[view.ID] = view.Connected
	}

	now := time.Now()
	indexRows := make([]store.ProjectIndex, 0)
	for _, result := range results {
		if !result.ok {
			continue
		}
		seenNode[result.node.ID] = true
		for _, project := range result.projects {
			key := projectKeyOf(project.ID, project.Canonical)
			addProjectNode(aggregated, key, project, result.node.ID, true, result.node.LastSeen)
			indexRows = append(indexRows, store.ProjectIndex{
				ProjectKey: key,
				NodeID:     result.node.ID,
				ProjectID:  project.ID,
				Canonical:  project.Canonical,
				VCS:        project.VCS,
				LastSeen:   now,
			})
		}
	}

	// 索引兜底：只补在线节点本次没成功拉取的情形；在线且成功的节点以
	// 实时结果为准，避免已删除的项目被索引永久复活。
	for _, row := range index {
		if seenNode[row.NodeID] {
			continue
		}
		addProjectNode(aggregated, row.ProjectKey, projectRecord{
			ID:        row.ProjectID,
			Canonical: row.Canonical,
			VCS:       row.VCS,
		}, row.NodeID, online[row.NodeID], row.LastSeen)
	}

	for _, pref := range prefs {
		if view, ok := aggregated[pref.ProjectKey]; ok {
			view.PrimaryNodeID = pref.PrimaryNodeID
			view.Alias = pref.Alias
		}
	}

	projects := make([]projectView, 0, len(aggregated))
	for _, view := range aggregated {
		projects = append(projects, *view)
	}
	sort.Slice(projects, func(i, j int) bool {
		if !projects[i].LastSeen.Equal(projects[j].LastSeen) {
			return projects[i].LastSeen.After(projects[j].LastSeen)
		}
		return projects[i].Key < projects[j].Key
	})

	// 索引写回失败只影响下次离线时的展示，不阻断本次响应。
	_ = a.store.UpsertProjectIndex(ctx, indexRows)

	writeJSON(w, http.StatusOK, projectListResponse{Projects: projects})
}

// addProjectNode 把一个节点上的项目合并进聚合视图。
func addProjectNode(
	aggregated map[string]*projectView,
	key string,
	project projectRecord,
	nodeID string,
	online bool,
	lastSeen time.Time,
) {
	view, ok := aggregated[key]
	if !ok {
		view = &projectView{
			Key:       key,
			ID:        project.ID,
			Canonical: project.Canonical,
			VCS:       project.VCS,
			Nodes:     make([]projectNodeView, 0, 2),
		}
		aggregated[key] = view
	}
	if lastSeen.After(view.LastSeen) {
		view.LastSeen = lastSeen
	}
	for _, existing := range view.Nodes {
		if existing.NodeID == nodeID {
			return
		}
	}
	view.Nodes = append(view.Nodes, projectNodeView{
		NodeID:   nodeID,
		Online:   online,
		LastSeen: lastSeen,
	})
}

// handleSetProjectPrimaryNode 设置项目主节点。
//
// 未知项目与未知节点都回 404：前者说明聚合里还没见过这个项目，后者说明
// 客户端拿的是过期节点列表。
func (a *API) handleSetProjectPrimaryNode(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")

	var req setPrimaryNodeRequest
	if err := decodeRequest(w, r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	if req.NodeID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "node_id is required")
		return
	}
	if _, ok, err := a.findNode(r.Context(), req.NodeID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	} else if !ok {
		writeAPIError(w, http.StatusNotFound, "not_found", "node not found")
		return
	}

	index, err := a.store.ListProjectIndex(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	known := false
	for _, row := range index {
		if row.ProjectKey == key {
			known = true
			break
		}
	}
	if !known {
		writeAPIError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	if err := a.store.SetProjectPrimaryNode(r.Context(), key, req.NodeID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleListPreferences 返回全部用户偏好；客户端启动时整包同步。
func (a *API) handleListPreferences(w http.ResponseWriter, r *http.Request) {
	values, err := a.store.ListPreferences(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, preferencesResponse{Values: values})
}

// handleSetPreference 写入一条用户偏好。值允许为空（表示清空该偏好），
// 因此只校验 key 非空。
func (a *API) handleSetPreference(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if key == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "preference key is required")
		return
	}
	var req setPreferenceRequest
	if err := decodeRequest(w, r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	if err := a.store.SetPreference(r.Context(), key, req.Value); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// supportsMethod 判断节点是否声明了某个能力；能力列表为空表示旧 agent
// 没上报，按「可能支持」处理，调用失败再降级。
func supportsMethod(view nodeView, method string) bool {
	if len(view.Capabilities) == 0 {
		return true
	}
	return slices.Contains(view.Capabilities, method)
}
