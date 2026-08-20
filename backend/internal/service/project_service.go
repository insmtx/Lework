package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/ygpkg/storage-go"

	"code.gitea.io/sdk/gitea"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/adapter/account"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/consts"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
	"github.com/insmtx/Leros/backend/internal/infra/git"
	"github.com/insmtx/Leros/backend/internal/infra/mq"
	localmemory "github.com/insmtx/Leros/backend/internal/memory/local"
	"github.com/insmtx/Leros/backend/internal/workspace"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/encryptor/snowflake"
	"github.com/ygpkg/yg-go/logs"
)

const (
	createdAtMaxConcurrent = 8
	createdAtMaxPages      = 100
)

type projectActivityCursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        uint      `json:"id"`
}

type projectService struct {
	db                 *gorm.DB
	perm               *PermissionService
	inferrer           AssistantInferrer
	giteaClient        *gitea.Client
	giteaCfg           *config.GiteaConfig
	env                string
	publisher          mq.Publisher
	userRepo           account.UserRepository
	displayTranslation *SkillDisplayTranslationService
}

// fileTreeEntry 文件树 walk 阶段收集的扁平条目
type fileTreeEntry struct {
	absPath string
	isDir   bool
	size    int64
	modTime int64
}

// NewProjectService 创建项目服务实例
func NewProjectService(db *gorm.DB, perm *PermissionService, giteaClient *gitea.Client, giteaCfg *config.GiteaConfig, env string, userRepo account.UserRepository, displayTranslation *SkillDisplayTranslationService) contract.ProjectService {
	return &projectService{
		db:                 db,
		perm:               perm,
		giteaClient:        giteaClient,
		giteaCfg:           giteaCfg,
		env:                env,
		userRepo:           userRepo,
		displayTranslation: displayTranslation,
	}
}

func NewProjectServiceWithInferrer(db *gorm.DB, perm *PermissionService, inferrer AssistantInferrer, giteaClient *gitea.Client, giteaCfg *config.GiteaConfig, env string, userRepo account.UserRepository, displayTranslation *SkillDisplayTranslationService) contract.ProjectService {
	return &projectService{
		db:                 db,
		perm:               perm,
		inferrer:           inferrer,
		giteaClient:        giteaClient,
		giteaCfg:           giteaCfg,
		env:                env,
		userRepo:           userRepo,
		displayTranslation: displayTranslation,
	}
}

func NewProjectServiceWithInferrerAndPublisher(
	db *gorm.DB,
	perm *PermissionService,
	inferrer AssistantInferrer,
	giteaClient *gitea.Client,
	giteaCfg *config.GiteaConfig,
	env string,
	publisher mq.Publisher,
	userRepo account.UserRepository,
	displayTranslation *SkillDisplayTranslationService,
) contract.ProjectService {
	return &projectService{
		db:                 db,
		perm:               perm,
		inferrer:           inferrer,
		giteaClient:        giteaClient,
		giteaCfg:           giteaCfg,
		env:                env,
		publisher:          publisher,
		userRepo:           userRepo,
		displayTranslation: displayTranslation,
	}
}

// permWithDB returns a PermissionService bound to db. Inside an open transaction, pass tx
// so auth reads uncommitted bindings and avoids SQLite single-connection deadlocks.
func (s *projectService) permWithDB(db *gorm.DB) *PermissionService {
	return PermissionForDB(db, s.perm)
}

func (s *projectService) CreateProject(ctx context.Context, req *contract.CreateProjectRequest) (*contract.Project, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Name) == "" {
		return nil, errors.New("name is required")
	}

	publicID := generateProjectPublicID()

	project := &types.Project{
		OrgID:       caller.OrgID,
		PublicID:    publicID,
		OwnerID:     caller.Uin,
		Name:        strings.TrimSpace(req.Name),
		Description: strings.TrimSpace(req.Description),
		Objective:   strings.TrimSpace(req.Objective),
		Status:      "active",
	}
	if req.Metadata != nil {
		project.Metadata = types.ObjectMetadata{}
		if tags, ok := req.Metadata["tags"].([]interface{}); ok {
			for _, t := range tags {
				if s, ok := t.(string); ok {
					project.Metadata.Tags = append(project.Metadata.Tags, s)
				}
			}
		}
		if t, ok := req.Metadata["type"].(string); ok {
			project.Metadata.Type = t
		}
		if extra, ok := req.Metadata["extra"].(map[string]interface{}); ok {
			project.Metadata.Extra = extra
		}
	}

	project.GiteaDefaultBranch = "main"

	if s.giteaClient != nil && s.giteaCfg != nil && s.giteaCfg.Enabled {
		repoName := s.buildRepoName(caller.OrgID, publicID)
		repoInfo, err := git.CreateRepoWithRetry(ctx, s.giteaClient, gitea.CreateRepoOption{
			Name:        repoName,
			Description: strings.TrimSpace(req.Description),
			Private:     true,
			AutoInit:    true,
		})
		if err != nil {
			return nil, fmt.Errorf("create gitea repo: %w", err)
		}
		if repoInfo == nil || repoInfo.FullName == "" {
			return nil, fmt.Errorf("create gitea repo: incomplete response (project=%s repo=%s)", publicID, repoName)
		}
		project.GiteaRepoFullName = repoInfo.FullName
		project.GiteaRepoID = repoInfo.ID
	}

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := db.CreateProject(ctx, tx, project); err != nil {
			return err
		}
		resource := &types.Resource{
			OrgID: caller.OrgID,
			Uin:   caller.Uin,
			Type:  types.ResourceTypeProject,
			BizID: project.ID,
		}
		if err := db.CreateResource(ctx, tx, resource); err != nil {
			return fmt.Errorf("sync project resource: %w", err)
		}
		uin := caller.Uin
		binding := &types.ResourceBinding{
			OrgID:      caller.OrgID,
			Uin:        &uin,
			ResourceID: resource.ID,
			Role:       types.ResourceRoleOwner,
		}
		if err := db.CreateResourceBinding(ctx, tx, binding); err != nil {
			return err
		}
		participantsPayload, participantsChanged, err := s.bindProjectMembers(ctx, tx, project.ID, resource.ID, caller, req.Members)
		if err != nil {
			return err
		}

		activityTime := time.Now()
		if err := s.createProjectActivityAt(ctx, tx, project.PublicID, caller.Uin, types.ProjectActivityActionProjectCreated, types.ProjectActivityPayload{}, nil, activityTime); err != nil {
			return err
		}
		if participantsChanged {
			if err := s.createProjectActivityAt(ctx, tx, project.PublicID, caller.Uin, types.ProjectActivityActionParticipantsChanged, participantsPayload, nil, activityTime.Add(time.Millisecond)); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return nil, err
	}
	if project.GiteaRepoFullName != "" {
		if err := git.InitRepoStructure(ctx, s.giteaClient, project.GiteaRepoFullName); err != nil {
			logs.WarnContextf(ctx, "[project] init repo structure: %v", err)
		}
	}

	return convertToContractProject(project), nil
}

func (s *projectService) GetProject(ctx context.Context, publicID string) (*contract.Project, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(publicID) == "" {
		return nil, errors.New("public_id is required")
	}

	project, err := db.GetProjectByPublicID(ctx, s.db, caller.OrgID, publicID)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, errors.New("project not found")
	}
	return convertToContractProject(project), nil
}

func (s *projectService) UpdateProject(ctx context.Context, publicID string, req *contract.UpdateProjectRequest) (*contract.Project, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(publicID) == "" {
		return nil, errors.New("public_id is required")
	}

	var project *types.Project
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		project, err = db.GetProjectByPublicID(ctx, tx, caller.OrgID, publicID)
		if err != nil {
			return err
		}
		if project == nil {
			return errors.New("project not found")
		}

		if req.Name != nil {
			project.Name = strings.TrimSpace(*req.Name)
			if project.Name == "" {
				return errors.New("name cannot be empty")
			}
		}
		if req.Description != nil {
			project.Description = strings.TrimSpace(*req.Description)
		}
		if req.Objective != nil {
			project.Objective = strings.TrimSpace(*req.Objective)
		}
		if req.OwnerID != nil {
			project.OwnerID = *req.OwnerID
		}
		if req.Status != nil {
			project.Status = *req.Status
		}
		if req.Metadata != nil {
			if *req.Metadata != nil {
				newMeta := types.ObjectMetadata{}
				if tags, ok := (*req.Metadata)["tags"].([]interface{}); ok {
					for _, t := range tags {
						if s, ok := t.(string); ok {
							newMeta.Tags = append(newMeta.Tags, s)
						}
					}
				}
				if t, ok := (*req.Metadata)["type"].(string); ok {
					newMeta.Type = t
				}
				if extra, ok := (*req.Metadata)["extra"].(map[string]interface{}); ok {
					newMeta.Extra = extra
				}
				project.Metadata = newMeta
			}
		}
		if err := db.UpdateProject(ctx, tx, project); err != nil {
			return err
		}

		if req.Members != nil {
			payload, changed, err := s.syncProjectMembers(ctx, tx, project.ID, caller.OrgID, caller, req.Members)
			if err != nil {
				return err
			}
			if changed {
				if err := s.createProjectActivity(ctx, tx, project.PublicID, caller.Uin, types.ProjectActivityActionParticipantsChanged, payload, nil); err != nil {
					return err
				}
			}
		}

		return nil
	}); err != nil {
		return nil, err
	}
	return convertToContractProject(project), nil
}

// ListProjectPlugins returns active plugin bindings visible to the caller's organization.
func (s *projectService) ListProjectPlugins(ctx context.Context, req *contract.ListProjectPluginsRequest) ([]contract.ProjectPlugin, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || strings.TrimSpace(req.PublicID) == "" {
		return nil, errors.New("public_id is required")
	}
	project, err := db.GetProjectByPublicID(ctx, s.db, caller.OrgID, req.PublicID)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, errors.New("project not found")
	}
	if _, err := s.authorizeProjectPluginAction(ctx, s.db, caller, project, nil, types.ActionProjectView); err != nil {
		return nil, err
	}
	plugins, err := db.ListProjectPlugins(ctx, s.db, caller.OrgID, project.ID, req.Kind)
	if err != nil {
		return nil, err
	}
	plugins = filterProjectPlugins(plugins, req.Keyword, req.Offset, req.Limit)
	result := make([]contract.ProjectPlugin, 0, len(plugins))
	for _, plugin := range plugins {
		result = append(result, contract.ProjectPlugin{PublicID: plugin.PublicID, Code: plugin.Code, Kind: plugin.Kind, Name: plugin.Name, Description: plugin.Description, Status: plugin.Status, Origin: plugin.Origin, CurrentRevision: plugin.CurrentRevision})
	}
	if s.displayTranslation == nil {
		logs.WarnContextf(ctx, "Skill display translation not used: org=%d phase=metadata source_type=%s project=%s use=false reason=service_unavailable",
			caller.OrgID, types.PluginTranslationSourceOrganization, req.PublicID)
	} else if caller.OrgID == 0 {
		logs.WarnContextf(ctx, "Skill display translation not used: phase=metadata source_type=%s project=%s use=false reason=organization_missing",
			types.PluginTranslationSourceOrganization, req.PublicID)
	} else {
		pluginIDs := make([]uint, 0, len(plugins))
		for _, plugin := range plugins {
			if plugin.Kind == "skill" {
				pluginIDs = append(pluginIDs, plugin.ID)
			}
		}
		revisions, revisionErr := db.ListCurrentPluginRevisionsByPluginIDs(ctx, s.db, pluginIDs)
		if revisionErr != nil {
			logs.WarnContextf(ctx, "Skill display translation not used: org=%d phase=metadata source_type=%s project=%s use=false reason=revision_lookup_failed: %v",
				caller.OrgID, types.PluginTranslationSourceOrganization, req.PublicID, revisionErr)
		} else {
			sources := make([]skillTranslationSource, 0, len(plugins))
			positions := make([]int, 0, len(plugins))
			for index, plugin := range plugins {
				if plugin.Kind != "skill" {
					continue
				}
				revision, exists := revisions[plugin.ID]
				if !exists {
					logs.WarnContextf(ctx, "Skill display translation not used: org=%d phase=metadata source_type=%s source_id=%d revision_id=0 project=%s use=false reason=revision_unavailable",
						caller.OrgID, types.PluginTranslationSourceOrganization, plugin.ID, req.PublicID)
					continue
				}
				revisionCopy := revision
				sources = append(sources, skillTranslationSource{
					sourceType:  types.PluginTranslationSourceOrganization,
					sourceID:    plugin.ID,
					revision:    &revisionCopy,
					name:        plugin.Name,
					description: plugin.Description,
				})
				positions = append(positions, index)
			}
			translations := s.displayTranslation.translateMetadata(ctx, caller.OrgID, sources)
			for index, source := range sources {
				key := skillTranslationKey{
					sourceType: source.sourceType,
					sourceID:   source.sourceID,
					revisionID: source.revision.ID,
				}
				applyTranslatedProjectMetadata(&result[positions[index]], translations[key])
			}
		}
	}
	return result, nil
}

// AddProjectPlugin creates one active project plugin binding.
func (s *projectService) AddProjectPlugin(ctx context.Context, req *contract.UpdateProjectPluginRequest) (*contract.ProjectPluginMutationResult, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateUpdateProjectPluginRequest(req); err != nil {
		return nil, err
	}
	var result *contract.ProjectPluginMutationResult
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		project, err := db.GetProjectByPublicID(ctx, tx, caller.OrgID, req.PublicID)
		if err != nil {
			return err
		}
		if project == nil {
			return errors.New("project not found")
		}
		plugin, err := resolveProjectPlugin(ctx, tx, caller.OrgID, req)
		if err != nil {
			return err
		}
		deployment, err := s.authorizeProjectPluginAction(ctx, tx, caller, project, plugin, types.ActionProjectUpdate)
		if err != nil {
			return err
		}
		if caller.Kind != types.CallerKindWorker {
			if err := newPluginAccessManager(tx).RequireUse(ctx, caller.OrgID, caller.Uin, plugin); err != nil {
				return err
			}
		}
		result = newProjectPluginMutationResult(project, plugin, true, false)
		bound, err := db.ListProjectPlugins(ctx, tx, caller.OrgID, project.ID, plugin.Kind)
		if err != nil {
			return err
		}
		for _, item := range bound {
			if item.ID == plugin.ID {
				return nil
			}
		}
		if err := db.CreateProjectPluginBinding(ctx, tx, &types.ProjectPluginBinding{ProjectID: project.ID, PluginID: plugin.ID, Enabled: true, Config: []byte(`{}`), CreatedBy: caller.Uin, UpdatedBy: caller.Uin}); err != nil {
			return err
		}
		result.Changed = true
		action, payload := projectPluginActivity(plugin, true)
		return s.createProjectPluginActivity(ctx, tx, project.PublicID, caller, deployment, action, payload)
	})
	return result, err
}

// RemoveProjectPlugin removes one project plugin binding.
func (s *projectService) RemoveProjectPlugin(ctx context.Context, req *contract.UpdateProjectPluginRequest) (*contract.ProjectPluginMutationResult, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateUpdateProjectPluginRequest(req); err != nil {
		return nil, err
	}
	var result *contract.ProjectPluginMutationResult
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		project, err := db.GetProjectByPublicID(ctx, tx, caller.OrgID, req.PublicID)
		if err != nil {
			return err
		}
		if project == nil {
			return errors.New("project not found")
		}
		plugin, err := resolveProjectPlugin(ctx, tx, caller.OrgID, req)
		if err != nil {
			return err
		}
		deployment, err := s.authorizeProjectPluginAction(ctx, tx, caller, project, plugin, types.ActionProjectUpdate)
		if err != nil {
			return err
		}
		result = newProjectPluginMutationResult(project, plugin, false, false)
		removed, err := db.RemoveProjectPluginBinding(ctx, tx, project.ID, plugin.ID)
		if err != nil {
			return err
		}
		if !removed {
			return nil
		}
		result.Changed = true
		action, payload := projectPluginActivity(plugin, false)
		return s.createProjectPluginActivity(ctx, tx, project.PublicID, caller, deployment, action, payload)
	})
	return result, err
}

func validateUpdateProjectPluginRequest(req *contract.UpdateProjectPluginRequest) error {
	if req == nil || strings.TrimSpace(req.PublicID) == "" {
		return errors.New("public_id is required")
	}
	pluginID := strings.TrimSpace(req.PluginID)
	pluginCode := strings.TrimSpace(req.PluginCode)
	if pluginID == "" && pluginCode == "" {
		return errors.New("plugin_id or plugin_code is required")
	}
	if pluginID != "" && pluginCode != "" {
		return errors.New("plugin_id and plugin_code cannot be used together")
	}
	if pluginCode != "" && strings.TrimSpace(req.Kind) == "" {
		return errors.New("kind is required with plugin_code")
	}
	return nil
}

func resolveProjectPlugin(
	ctx context.Context,
	database *gorm.DB,
	orgID uint,
	req *contract.UpdateProjectPluginRequest,
) (*types.Plugin, error) {
	var (
		plugin *types.Plugin
		err    error
	)
	if pluginID := strings.TrimSpace(req.PluginID); pluginID != "" {
		plugin, err = db.GetPluginByPublicID(ctx, database, orgID, pluginID)
	} else {
		plugin, err = db.GetOrganizationPluginByIdentity(
			ctx,
			database,
			orgID,
			strings.TrimSpace(req.Kind),
			strings.TrimSpace(req.PluginCode),
		)
	}
	if err != nil {
		return nil, err
	}
	if plugin == nil || plugin.Status != types.PluginStatusActive {
		return nil, errors.New("plugin not found")
	}
	if kind := strings.TrimSpace(req.Kind); kind != "" && plugin.Kind != kind {
		return nil, errors.New("plugin not found")
	}
	return plugin, nil
}

func (s *projectService) authorizeProjectPluginAction(
	ctx context.Context,
	database *gorm.DB,
	caller *types.Caller,
	project *types.Project,
	plugin *types.Plugin,
	action types.Action,
) (*types.WorkerDeployment, error) {
	if caller.Kind != types.CallerKindWorker {
		return nil, s.permWithDB(database).RequireProject(ctx, FromTypeCaller(caller), project, action)
	}
	if caller.WorkerID == 0 {
		return nil, errors.New("permission denied: worker identity is required")
	}
	deployment, err := db.GetWorkerDeploymentByOrgWorkerID(ctx, database, caller.OrgID, caller.WorkerID)
	if err != nil {
		return nil, err
	}
	if deployment == nil || deployment.DigitalAssistantID == 0 {
		return nil, errors.New("permission denied: worker deployment not found")
	}
	bound, err := db.IsProjectAssistantBound(ctx, database, caller.OrgID, project.ID, deployment.DigitalAssistantID)
	if err != nil {
		return nil, err
	}
	if !bound {
		return nil, errors.New("permission denied: worker is not bound to project")
	}
	if action == types.ActionProjectUpdate && (plugin == nil || plugin.Kind != "skill") {
		return nil, errors.New("permission denied: worker may only manage project skills")
	}
	return deployment, nil
}

func newProjectPluginMutationResult(
	project *types.Project,
	plugin *types.Plugin,
	associated, changed bool,
) *contract.ProjectPluginMutationResult {
	return &contract.ProjectPluginMutationResult{
		ProjectID:  project.PublicID,
		PluginID:   plugin.PublicID,
		PluginCode: plugin.Code,
		Kind:       plugin.Kind,
		Associated: associated,
		Changed:    changed,
	}
}

func filterProjectPlugins(plugins []types.Plugin, keyword string, offset, limit int) []types.Plugin {
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	if keyword != "" {
		filtered := make([]types.Plugin, 0, len(plugins))
		for _, plugin := range plugins {
			if strings.Contains(strings.ToLower(plugin.Code), keyword) ||
				strings.Contains(strings.ToLower(plugin.Name), keyword) ||
				strings.Contains(strings.ToLower(plugin.Description), keyword) {
				filtered = append(filtered, plugin)
			}
		}
		plugins = filtered
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(plugins) {
		return []types.Plugin{}
	}
	plugins = plugins[offset:]
	if limit > 0 && limit < len(plugins) {
		plugins = plugins[:limit]
	}
	return plugins
}

func (s *projectService) createProjectPluginActivity(
	ctx context.Context,
	tx *gorm.DB,
	projectID string,
	caller *types.Caller,
	deployment *types.WorkerDeployment,
	actionType types.ProjectActivityAction,
	payload types.ProjectActivityPayload,
) error {
	operatorID := ""
	if caller.Kind == types.CallerKindWorker {
		if deployment == nil {
			return errors.New("worker deployment is required")
		}
		assistant, err := db.GetDigitalAssistantByID(ctx, tx, deployment.DigitalAssistantID)
		if err != nil {
			return err
		}
		if assistant == nil || assistant.OrgID != caller.OrgID || strings.TrimSpace(assistant.PublicID) == "" {
			return errors.New("worker assistant not found")
		}
		operatorID = assistant.PublicID
	} else {
		var err error
		operatorID, err = s.publicIDForUser(ctx, caller.Uin)
		if err != nil {
			return err
		}
	}
	payload = normalizeProjectActivityPayload(payload)
	return db.CreateProjectActivity(ctx, tx, &types.ProjectActivity{
		ProjectID:  projectID,
		OperatorID: operatorID,
		ActionType: actionType,
		Payload:    payload,
		Version:    1,
		CreatedAt:  time.Now(),
	})
}

func projectPluginActivity(
	plugin *types.Plugin,
	added bool,
) (types.ProjectActivityAction, types.ProjectActivityPayload) {
	payload := types.ProjectActivityPayload{}
	if plugin != nil && strings.EqualFold(plugin.Kind, "mcp") {
		if added {
			payload.AddedMCPIDs = []string{plugin.PublicID}
		} else {
			payload.RemovedMCPIDs = []string{plugin.PublicID}
		}
		return types.ProjectActivityActionMCPsChanged, payload
	}
	if plugin != nil {
		if added {
			payload.AddedSkillIDs = []string{plugin.PublicID}
		} else {
			payload.RemovedSkillIDs = []string{plugin.PublicID}
		}
	}
	return types.ProjectActivityActionSkillsChanged, payload
}

func (s *projectService) DeleteProject(ctx context.Context, publicID string) error {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(publicID) == "" {
		return errors.New("public_id is required")
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		project, err := db.GetProjectByPublicID(ctx, tx, caller.OrgID, publicID)
		if err != nil {
			return err
		}
		if project == nil {
			return errors.New("project not found")
		}
		if err := db.DeleteTasksByProjectID(ctx, tx, caller.OrgID, project.ID); err != nil {
			return err
		}
		return db.DeleteProject(ctx, tx, project.ID)
	})
}

func (s *projectService) LeaveProject(ctx context.Context, publicID string) error {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return err
	}
	if caller.Uin == 0 {
		return errors.New("only user principals can leave project")
	}
	if strings.TrimSpace(publicID) == "" {
		return errors.New("public_id is required")
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		project, err := db.GetProjectByPublicID(ctx, tx, caller.OrgID, publicID)
		if err != nil {
			return err
		}
		if project == nil {
			return errors.New("project not found")
		}

		resource, err := db.GetResourceByBizID(ctx, tx, caller.OrgID, types.ResourceTypeProject, project.ID)
		if err != nil {
			return fmt.Errorf("get project resource: %w", err)
		}
		if resource == nil {
			return errors.New("project resource not found")
		}

		binding, err := db.GetResourceBindingByUin(ctx, tx, resource.ID, caller.Uin)
		if err != nil {
			return fmt.Errorf("get resource binding: %w", err)
		}
		if binding == nil {
			return errors.New("not a project member")
		}
		return db.DeleteResourceBinding(ctx, tx, binding.ID)
	})
}

func (s *projectService) ListProjects(ctx context.Context, req *contract.ListProjectsRequest) (*contract.ProjectList, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	req.Fill()

	projectIDs, err := db.ListProjectIDsByUser(ctx, s.db, caller.OrgID, caller.Uin)
	if err != nil {
		return nil, err
	}
	if len(projectIDs) == 0 {
		return &contract.ProjectList{
			Total:  0,
			Offset: req.Offset,
			Limit:  req.Limit,
			Items:  []contract.Project{},
		}, nil
	}

	opt := types.NewPageQuery(*caller, req.Offset, req.Limit)
	opt.ProjectIDs = projectIDs
	opt.ListAll = req.ListAll
	if req.Keyword != nil && *req.Keyword != "" {
		opt.AddFilter("name", *req.Keyword)
	}
	if req.Status != nil && *req.Status != "" {
		opt.AddFilter("status", *req.Status)
	}

	projects, total, err := db.ListProjects(ctx, s.db, opt)
	if err != nil {
		return nil, err
	}

	projIDsForCount := make([]uint, 0, len(projects))
	for _, project := range projects {
		projIDsForCount = append(projIDsForCount, project.ID)
	}
	var taskCountMap map[uint]int64
	if len(projIDsForCount) > 0 {
		taskCountMap, err = db.CountTasksByProjectIDs(ctx, s.db, caller.OrgID, projIDsForCount)
		if err != nil {
			return nil, err
		}
	}

	items := make([]contract.Project, 0, len(projects))
	for _, project := range projects {
		item := convertToContractProject(project)
		item.TaskCount = taskCountMap[project.ID]
		items = append(items, *item)
	}
	return &contract.ProjectList{
		Total:  total,
		Offset: req.Offset,
		Limit:  req.Limit,
		Items:  items,
	}, nil
}

func (s *projectService) ListProjectActivities(ctx context.Context, req *contract.ListProjectActivitiesRequest) (*contract.ProjectActivityList, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil {
		req = &contract.ListProjectActivitiesRequest{}
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	var beforeTime *time.Time
	var beforeID uint
	if strings.TrimSpace(req.Cursor) != "" {
		cursor, err := decodeProjectActivityCursor(req.Cursor)
		if err != nil {
			return nil, err
		}
		beforeTime = &cursor.CreatedAt
		beforeID = cursor.ID
	}

	opt := db.ProjectActivityListOptions{
		OperatorIDs: mergeProjectActivityOperatorIDs(req.OperatorID, req.OperatorIDs),
		BeforeTime:  beforeTime,
		BeforeID:    beforeID,
		Limit:       limit,
	}

	if strings.TrimSpace(req.ProjectID) != "" {
		project, err := db.GetProjectByPublicID(ctx, s.db, caller.OrgID, strings.TrimSpace(req.ProjectID))
		if err != nil {
			return nil, err
		}
		if project == nil {
			return nil, errors.New("project not found")
		}
		opt.ProjectID = project.PublicID
	} else {
		projectIDs, err := db.ListProjectIDsByUser(ctx, s.db, caller.OrgID, caller.Uin)
		if err != nil {
			return nil, err
		}
		projects, err := db.GetProjectsByIDs(ctx, s.db, projectIDs)
		if err != nil {
			return nil, err
		}
		opt.ProjectIDs = make([]string, 0, len(projects))
		for _, project := range projects {
			opt.ProjectIDs = append(opt.ProjectIDs, project.PublicID)
		}
	}

	activities, err := db.ListProjectActivities(ctx, s.db, opt)
	if err != nil {
		return nil, err
	}
	items, err := s.buildProjectActivityItems(ctx, caller.OrgID, activities)
	if err != nil {
		return nil, err
	}

	nextCursor := ""
	if len(activities) == limit {
		last := activities[len(activities)-1]
		nextCursor = encodeProjectActivityCursor(projectActivityCursor{CreatedAt: last.CreatedAt, ID: last.ID})
	}
	return &contract.ProjectActivityList{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

func (s *projectService) GetWorkbenchRecentContext(ctx context.Context) (*contract.WorkbenchRecentContext, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}

	recent, err := db.GetWorkbenchRecentContext(ctx, s.db, caller.OrgID, caller.Uin)
	if err != nil {
		return nil, err
	}
	if recent == nil {
		return nil, nil
	}

	project, err := db.GetProjectByID(ctx, s.db, recent.ProjectID)
	if err != nil {
		return nil, err
	}
	if project == nil || project.OrgID != caller.OrgID || s.perm.RequireProject(ctx, FromTypeCaller(caller), project, types.ActionProjectView) != nil {
		return nil, nil
	}

	var task *types.Task
	if recent.TaskID != nil {
		task, err = db.GetTaskByID(ctx, s.db, caller.OrgID, *recent.TaskID)
		if err != nil {
			return nil, err
		}
		if task == nil || task.ProjectID != project.ID || s.perm.RequireTask(ctx, FromTypeCaller(caller), task, types.ActionTaskView) != nil {
			task = nil
		}
	}

	return buildWorkbenchRecentContext(project, task, recent.UsedAt), nil
}

func (s *projectService) SaveWorkbenchRecentContext(ctx context.Context, req *contract.SaveWorkbenchRecentContextRequest) (*contract.WorkbenchRecentContext, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New("project_id is required")
	}

	project, err := db.GetProjectByPublicID(ctx, s.db, caller.OrgID, strings.TrimSpace(req.ProjectID))
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, errors.New("project not found")
	}

	var task *types.Task
	var taskID *uint
	if req.TaskID != nil && strings.TrimSpace(*req.TaskID) != "" {
		task, err = db.GetTaskByPublicID(ctx, s.db, caller.OrgID, strings.TrimSpace(*req.TaskID))
		if err != nil {
			return nil, err
		}
		if task == nil {
			return nil, errors.New("task not found")
		}
		if err := s.perm.RequireTask(ctx, FromTypeCaller(caller), task, types.ActionTaskView); err != nil {
			return nil, err
		}
		if task.ProjectID != project.ID {
			return nil, errors.New("task does not belong to project")
		}
		taskID = &task.ID
	}

	usedAt := time.Now()
	entity := &types.WorkbenchRecentContext{
		OrgID:     caller.OrgID,
		Uin:       caller.Uin,
		ProjectID: project.ID,
		TaskID:    taskID,
		UsedAt:    usedAt,
	}
	if err := db.UpsertWorkbenchRecentContext(ctx, s.db, entity); err != nil {
		return nil, err
	}

	return buildWorkbenchRecentContext(project, task, usedAt), nil
}

// bindProjectMembers 创建项目时绑定默认 AI 队友与用户成员（写 leros_resource_binding）。
// 创建者本人的 owner 绑定由调用方在事务内先行创建，此处跳过。
func (s *projectService) bindProjectMembers(ctx context.Context, tx *gorm.DB, projectID, resourceID uint, caller *types.Caller, inputs []contract.MemberInput) (types.ProjectActivityPayload, bool, error) {
	payload := types.ProjectActivityPayload{}
	defaultAssistantID, err := db.GetDefaultAssistantIDByOrg(ctx, tx, caller.OrgID)
	if err != nil {
		return payload, false, fmt.Errorf("get default assistant: %w", err)
	}
	if defaultAssistantID == 0 {
		return payload, false, ErrNoDefaultAssistantInOrg
	}

	assistantPublicIDs, userMembers, err := parseMemberInputs(inputs)
	if err != nil {
		return payload, false, err
	}

	assistantIDs, err := resolveAssistantIDsByPublicID(ctx, tx, caller.OrgID, assistantPublicIDs)
	if err != nil {
		return payload, false, fmt.Errorf("resolve assistant public ids: %w", err)
	}

	if err := validateAssistantIDs(assistantIDs, defaultAssistantID); err != nil {
		return payload, false, err
	}

	if err := s.bindProjectAssistantMembers(ctx, tx, caller.OrgID, resourceID, projectID, caller, defaultAssistantID, assistantIDs); err != nil {
		return payload, false, err
	}

	if err := s.bindProjectUserMembers(ctx, tx, caller.OrgID, resourceID, projectID, caller, userMembers); err != nil {
		return payload, false, err
	}

	addedAssistantIDs := make([]uint, 0, len(assistantIDs))
	for _, id := range assistantIDs {
		if id == defaultAssistantID {
			continue
		}
		addedAssistantIDs = append(addedAssistantIDs, id)
	}
	payload.AddedAITeammateIDs, err = publicIDsForAssistants(ctx, tx, uniqueUintSlice(addedAssistantIDs))
	if err != nil {
		return payload, false, err
	}

	addedUserIDs := make([]uint, 0, len(userMembers))
	publicIDs := make([]string, 0, len(userMembers))
	for _, m := range userMembers {
		publicIDs = append(publicIDs, m.PublicID)
	}
	uinMap, err := s.userRepo.GetUinMapByPublicIDs(ctx, caller.OrgID, publicIDs)
	if err != nil {
		return payload, false, fmt.Errorf("get user uins: %w", err)
	}
	for _, m := range userMembers {
		uin, ok := uinMap[m.PublicID]
		if !ok || uin == 0 || uin == caller.Uin {
			continue
		}
		addedUserIDs = append(addedUserIDs, uin)
	}
	payload.AddedMemberIDs, err = s.publicIDsForUsers(ctx, uniqueUintSlice(addedUserIDs))
	if err != nil {
		return payload, false, err
	}

	changed := len(payload.AddedMemberIDs) > 0 || len(payload.AddedAITeammateIDs) > 0
	return payload, changed, nil
}

// bindProjectUserMembers 为用户成员在指定资源上创建 leros_resource_binding（带角色），跳过创建者本人。
func (s *projectService) bindProjectUserMembers(ctx context.Context, tx *gorm.DB, orgID, resourceID, projectID uint, caller *types.Caller, userMembers []userMemberInput) error {
	if len(userMembers) == 0 {
		return nil
	}
	publicIDs := make([]string, 0, len(userMembers))
	for _, m := range userMembers {
		publicIDs = append(publicIDs, m.PublicID)
	}
	uinMap, err := s.userRepo.GetUinMapByPublicIDs(ctx, orgID, publicIDs)
	if err != nil {
		return fmt.Errorf("get user uins: %w", err)
	}

	seen := make(map[uint]bool, len(userMembers))
	for _, m := range userMembers {
		uin, ok := uinMap[m.PublicID]
		if !ok || uin == 0 || uin == caller.Uin || seen[uin] {
			continue
		}
		seen[uin] = true
		if err := s.permWithDB(tx).RequireProjectMember(ctx, FromTypeCaller(caller), projectID, ActionProjectMemberCreate, &MemberInput{
			TargetUin:     uin,
			RequestedRole: m.Role,
		}); err != nil {
			return err
		}
		boundUin := uin
		if err := db.CreateResourceBinding(ctx, tx, &types.ResourceBinding{
			OrgID:      orgID,
			Uin:        &boundUin,
			ResourceID: resourceID,
			Role:       m.Role,
		}); err != nil {
			return fmt.Errorf("bind project user member %d: %w", uin, err)
		}
	}
	return nil
}

// bindProjectAssistantMembers 为默认及额外 AI 队友在指定资源上创建 leros_resource_binding（member 角色）。
func (s *projectService) bindProjectAssistantMembers(ctx context.Context, tx *gorm.DB, orgID, resourceID, projectID uint, caller *types.Caller, defaultAssistantID uint, assistantIDs []uint) error {
	allIDs := append([]uint{defaultAssistantID}, assistantIDs...)
	seen := make(map[uint]bool, len(allIDs))
	for _, id := range allIDs {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		if err := s.permWithDB(tx).RequireProjectMember(ctx, FromTypeCaller(caller), projectID, ActionProjectMemberCreate, &MemberInput{
			TargetAssistantID: &id,
			RequestedRole:     types.ResourceRoleMember,
		}); err != nil {
			return err
		}
		boundID := id
		if err := db.CreateResourceBinding(ctx, tx, &types.ResourceBinding{
			OrgID:       orgID,
			AssistantID: &boundID,
			ResourceID:  resourceID,
			Role:        types.ResourceRoleMember,
		}); err != nil {
			return fmt.Errorf("bind project assistant member %d: %w", id, err)
		}
	}
	return nil
}

// userMemberInput 表示带角色的用户成员输入，供绑定到 leros_resource_binding 使用。
type userMemberInput struct {
	PublicID string
	Role     types.ResourceRole
}

// resolveResourceRole 将请求传入的角色字符串解析为 ResourceRole。
// 空值默认为 member；非法值返回错误。
func resolveResourceRole(raw string) (types.ResourceRole, error) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "":
		return types.ResourceRoleMember, nil
	case string(types.ResourceRoleOwner):
		return types.ResourceRoleOwner, nil
	case string(types.ResourceRoleAdmin):
		return types.ResourceRoleAdmin, nil
	case string(types.ResourceRoleMember):
		return types.ResourceRoleMember, nil
	default:
		return "", fmt.Errorf("invalid member role: %s", raw)
	}
}

// parseMemberInputs 将 MemberInput 列表拆分为 assistant public_id 组和带角色的 user 成员组。
func parseMemberInputs(inputs []contract.MemberInput) (assistantPublicIDs []string, userMembers []userMemberInput, err error) {
	for _, m := range inputs {
		switch m.Type {
		case "assistant":
			assistantPublicIDs = append(assistantPublicIDs, m.ID)
		case "user":
			role, rerr := resolveResourceRole(m.Role)
			if rerr != nil {
				return nil, nil, rerr
			}
			userMembers = append(userMembers, userMemberInput{PublicID: m.ID, Role: role})
		default:
			return nil, nil, fmt.Errorf("invalid member type: %s", m.Type)
		}
	}
	return assistantPublicIDs, userMembers, nil
}

// syncProjectMembers 在 UpdateProject 时 diff 当前成员与传入列表：
// 新增的添加，要移除的删除（is_default=true 的不可移除）。
func (s *projectService) syncProjectMembers(ctx context.Context, tx *gorm.DB, projectID, orgID uint, caller *types.Caller, inputs []contract.MemberInput) (types.ProjectActivityPayload, bool, error) {
	payload := types.ProjectActivityPayload{}
	defaultAssistantID, err := db.GetDefaultAssistantIDByOrg(ctx, tx, orgID)
	if err != nil {
		return payload, false, fmt.Errorf("get default assistant: %w", err)
	}
	if defaultAssistantID == 0 {
		return payload, false, ErrNoDefaultAssistantInOrg
	}

	assistantPublicIDs, userMembers, err := parseMemberInputs(inputs)
	if err != nil {
		return payload, false, err
	}

	assistantIDs, err := resolveAssistantIDsByPublicID(ctx, tx, orgID, assistantPublicIDs)
	if err != nil {
		return payload, false, fmt.Errorf("resolve assistant public ids: %w", err)
	}

	if err := validateAssistantIDs(assistantIDs, defaultAssistantID); err != nil {
		return payload, false, err
	}

	addedAssistantIDs, removedAssistantIDs, err := s.syncProjectAssistantMembers(ctx, tx, projectID, orgID, caller, assistantIDs, defaultAssistantID)
	if err != nil {
		return payload, false, err
	}
	payload.AddedAITeammateIDs, err = publicIDsForAssistants(ctx, tx, addedAssistantIDs)
	if err != nil {
		return payload, false, err
	}
	payload.RemovedAITeammateIDs, err = publicIDsForAssistants(ctx, tx, removedAssistantIDs)
	if err != nil {
		return payload, false, err
	}

	// 同步用户成员：权限与成员来源为 leros_resource_binding。
	addedUserIDs, removedUserIDs, err := s.syncProjectUserMembers(ctx, tx, projectID, orgID, caller, userMembers)
	if err != nil {
		return payload, false, err
	}
	payload.AddedMemberIDs, err = s.publicIDsForUsers(ctx, addedUserIDs)
	if err != nil {
		return payload, false, err
	}
	payload.RemovedMemberIDs, err = s.publicIDsForUsers(ctx, removedUserIDs)
	if err != nil {
		return payload, false, err
	}

	changed := len(payload.AddedMemberIDs) > 0 ||
		len(payload.RemovedMemberIDs) > 0 ||
		len(payload.AddedAITeammateIDs) > 0 ||
		len(payload.RemovedAITeammateIDs) > 0
	return payload, changed, nil
}

// syncProjectAssistantMembers 在 UpdateProject 时 diff 项目上的 AI 队友成员与 resource binding：
// 新增创建、缺失删除；写入前逐人校验 project:member.* 权限。默认 AI 队友不参与 diff。
func (s *projectService) syncProjectAssistantMembers(ctx context.Context, tx *gorm.DB, projectID, orgID uint, caller *types.Caller, assistantIDs []uint, defaultAssistantID uint) (addedAssistantIDs, removedAssistantIDs []uint, err error) {
	resource, err := db.GetResourceByBizID(ctx, tx, orgID, types.ResourceTypeProject, projectID)
	if err != nil {
		return nil, nil, fmt.Errorf("get project resource: %w", err)
	}
	if resource == nil {
		return nil, nil, fmt.Errorf("project resource not found for project %d", projectID)
	}

	requestedAssistantSet := make(map[uint]bool, len(assistantIDs))
	for _, id := range assistantIDs {
		requestedAssistantSet[id] = true
	}

	bindings, err := db.ListResourceBindingsByResourceID(ctx, tx, resource.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("list resource bindings: %w", err)
	}
	existingAssistantBindings := make(map[uint]*types.ResourceBinding)
	for _, b := range bindings {
		if b.AssistantID == nil || *b.AssistantID == 0 || *b.AssistantID == defaultAssistantID {
			continue
		}
		existingAssistantBindings[*b.AssistantID] = b
	}

	for id := range existingAssistantBindings {
		if !requestedAssistantSet[id] {
			targetID := id
			if err := s.permWithDB(tx).RequireProjectMember(ctx, FromTypeCaller(caller), projectID, ActionProjectMemberDelete, &MemberInput{
				TargetAssistantID: &targetID,
			}); err != nil {
				return nil, nil, err
			}
		}
	}
	for _, id := range assistantIDs {
		if _, ok := existingAssistantBindings[id]; !ok {
			targetID := id
			if err := s.permWithDB(tx).RequireProjectMember(ctx, FromTypeCaller(caller), projectID, ActionProjectMemberCreate, &MemberInput{
				TargetAssistantID: &targetID,
				RequestedRole:     types.ResourceRoleMember,
			}); err != nil {
				return nil, nil, err
			}
		}
	}

	oldAssistantIDs := make([]uint, 0, len(existingAssistantBindings))
	for id := range existingAssistantBindings {
		oldAssistantIDs = append(oldAssistantIDs, id)
	}
	addedAssistantIDs, removedAssistantIDs = diffUintSlices(oldAssistantIDs, assistantIDs)

	for id, b := range existingAssistantBindings {
		if !requestedAssistantSet[id] {
			if err := db.DeleteResourceBinding(ctx, tx, b.ID); err != nil {
				return nil, nil, fmt.Errorf("delete resource binding for assistant %d: %w", id, err)
			}
		}
	}
	for _, id := range assistantIDs {
		if _, ok := existingAssistantBindings[id]; !ok {
			boundID := id
			if err := db.CreateResourceBinding(ctx, tx, &types.ResourceBinding{
				OrgID:       orgID,
				AssistantID: &boundID,
				ResourceID:  resource.ID,
				Role:        types.ResourceRoleMember,
			}); err != nil {
				return nil, nil, fmt.Errorf("create resource binding for assistant %d: %w", id, err)
			}
		}
	}

	return addedAssistantIDs, removedAssistantIDs, nil
}

// syncProjectUserMembers 在 UpdateProject 时 diff 项目资源上的用户绑定与传入用户成员：
// 新增创建、缺失删除、角色变化更新；写入前逐人校验 project:member.* 权限。
func (s *projectService) syncProjectUserMembers(ctx context.Context, tx *gorm.DB, projectID, orgID uint, caller *types.Caller, userMembers []userMemberInput) (addedUserIDs []uint, removedUserIDs []uint, err error) {
	resource, err := db.GetResourceByBizID(ctx, tx, orgID, types.ResourceTypeProject, projectID)
	if err != nil {
		return nil, nil, fmt.Errorf("get project resource: %w", err)
	}
	if resource == nil {
		return nil, nil, fmt.Errorf("project resource not found for project %d", projectID)
	}

	bindings, err := db.ListResourceBindingsByResourceID(ctx, tx, resource.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("list resource bindings: %w", err)
	}
	existingUserBindings := make(map[uint]*types.ResourceBinding)
	for _, b := range bindings {
		if b.Uin == nil || *b.Uin == 0 {
			continue
		}
		existingUserBindings[*b.Uin] = b
	}

	publicIDs := make([]string, 0, len(userMembers))
	for _, m := range userMembers {
		publicIDs = append(publicIDs, m.PublicID)
	}
	uinMap, err := s.userRepo.GetUinMapByPublicIDs(ctx, orgID, publicIDs)
	if err != nil {
		return nil, nil, fmt.Errorf("get user uins: %w", err)
	}
	requestedRoles := make(map[uint]types.ResourceRole, len(userMembers))
	for _, m := range userMembers {
		uin, ok := uinMap[m.PublicID]
		if !ok || uin == 0 {
			return nil, nil, fmt.Errorf("user not found for member public_id %q", m.PublicID)
		}
		if uin == caller.Uin {
			if b, ok := existingUserBindings[uin]; ok {
				requestedRoles[uin] = b.Role
			}
			continue
		}
		requestedRoles[uin] = m.Role
	}
	for uin, b := range existingUserBindings {
		if b.Role == types.ResourceRoleOwner {
			if _, ok := requestedRoles[uin]; !ok {
				requestedRoles[uin] = types.ResourceRoleOwner
			}
		}
	}

	for uin := range existingUserBindings {
		if _, ok := requestedRoles[uin]; !ok {
			if err := s.permWithDB(tx).RequireProjectMember(ctx, FromTypeCaller(caller), projectID, ActionProjectMemberDelete, &MemberInput{
				TargetUin: uin,
			}); err != nil {
				return nil, nil, err
			}
		}
	}
	for uin, role := range requestedRoles {
		if b, ok := existingUserBindings[uin]; ok {
			if b.Role != role {
				if err := s.permWithDB(tx).RequireProjectMember(ctx, FromTypeCaller(caller), projectID, ActionProjectMemberUpdate, &MemberInput{
					TargetUin:     uin,
					RequestedRole: role,
				}); err != nil {
					return nil, nil, err
				}
			}
			continue
		}
		if err := s.permWithDB(tx).RequireProjectMember(ctx, FromTypeCaller(caller), projectID, ActionProjectMemberCreate, &MemberInput{
			TargetUin:     uin,
			RequestedRole: role,
		}); err != nil {
			return nil, nil, err
		}
	}

	oldUserIDs := make([]uint, 0, len(existingUserBindings))
	for uin := range existingUserBindings {
		oldUserIDs = append(oldUserIDs, uin)
	}
	requestedUserIDs := make([]uint, 0, len(requestedRoles))
	for uin := range requestedRoles {
		requestedUserIDs = append(requestedUserIDs, uin)
	}
	addedUserIDs, removedUserIDs = diffUintSlices(oldUserIDs, requestedUserIDs)

	for uin, b := range existingUserBindings {
		if _, ok := requestedRoles[uin]; !ok {
			if err := db.DeleteResourceBinding(ctx, tx, b.ID); err != nil {
				return nil, nil, fmt.Errorf("delete resource binding for user %d: %w", uin, err)
			}
		}
	}
	for uin, role := range requestedRoles {
		if b, ok := existingUserBindings[uin]; ok {
			if b.Role != role {
				if err := db.UpdateResourceBindingRole(ctx, tx, b.ID, role); err != nil {
					return nil, nil, fmt.Errorf("update resource binding role for user %d: %w", uin, err)
				}
			}
			continue
		}
		boundUin := uin
		if err := db.CreateResourceBinding(ctx, tx, &types.ResourceBinding{
			OrgID:      orgID,
			Uin:        &boundUin,
			ResourceID: resource.ID,
			Role:       role,
		}); err != nil {
			return nil, nil, fmt.Errorf("create resource binding for user %d: %w", uin, err)
		}
	}

	return addedUserIDs, removedUserIDs, nil
}

func (s *projectService) createProjectActivity(
	ctx context.Context,
	tx *gorm.DB,
	projectID string,
	operatorUin uint,
	actionType types.ProjectActivityAction,
	payload types.ProjectActivityPayload,
	requestID *string,
) error {
	return s.createProjectActivityAt(ctx, tx, projectID, operatorUin, actionType, payload, requestID, time.Now())
}

func (s *projectService) createProjectActivityAt(
	ctx context.Context,
	tx *gorm.DB,
	projectID string,
	operatorUin uint,
	actionType types.ProjectActivityAction,
	payload types.ProjectActivityPayload,
	requestID *string,
	createdAt time.Time,
) error {
	operatorID, err := s.publicIDForUser(ctx, operatorUin)
	if err != nil {
		return err
	}
	payload = normalizeProjectActivityPayload(payload)
	return db.CreateProjectActivity(ctx, tx, &types.ProjectActivity{
		ProjectID:  projectID,
		OperatorID: operatorID,
		ActionType: actionType,
		Payload:    payload,
		RequestID:  requestID,
		Version:    1,
		CreatedAt:  createdAt,
	})
}

func normalizeProjectActivityPayload(payload types.ProjectActivityPayload) types.ProjectActivityPayload {
	if payload.AddedSkillIDs == nil {
		payload.AddedSkillIDs = []string{}
	}
	if payload.RemovedSkillIDs == nil {
		payload.RemovedSkillIDs = []string{}
	}
	if payload.AddedMCPIDs == nil {
		payload.AddedMCPIDs = []string{}
	}
	if payload.RemovedMCPIDs == nil {
		payload.RemovedMCPIDs = []string{}
	}
	if payload.AddedMemberIDs == nil {
		payload.AddedMemberIDs = []string{}
	}
	if payload.RemovedMemberIDs == nil {
		payload.RemovedMemberIDs = []string{}
	}
	if payload.AddedAITeammateIDs == nil {
		payload.AddedAITeammateIDs = []string{}
	}
	if payload.RemovedAITeammateIDs == nil {
		payload.RemovedAITeammateIDs = []string{}
	}
	return payload
}

func (s *projectService) buildProjectActivityItems(ctx context.Context, orgID uint, activities []*types.ProjectActivity) ([]contract.ProjectActivityItem, error) {
	if len(activities) == 0 {
		return []contract.ProjectActivityItem{}, nil
	}

	userIDs := make([]string, 0, len(activities))
	assistantIDs := make([]string, 0)
	skillIDs := make([]string, 0)
	mcpIDs := make([]string, 0)
	for _, activity := range activities {
		userIDs = append(userIDs, activity.OperatorID)
		payload := normalizeProjectActivityPayload(activity.Payload)
		userIDs = append(userIDs, payload.AddedMemberIDs...)
		userIDs = append(userIDs, payload.RemovedMemberIDs...)
		assistantIDs = append(assistantIDs, payload.AddedAITeammateIDs...)
		assistantIDs = append(assistantIDs, payload.RemovedAITeammateIDs...)
		skillIDs = append(skillIDs, payload.AddedSkillIDs...)
		skillIDs = append(skillIDs, payload.RemovedSkillIDs...)
		mcpIDs = append(mcpIDs, payload.AddedMCPIDs...)
		mcpIDs = append(mcpIDs, payload.RemovedMCPIDs...)
	}

	users, err := s.userRepo.GetUsersByPublicIDs(ctx, uniqueStringSlice(userIDs))
	if err != nil {
		return nil, err
	}
	userMap := make(map[string]*account.UserInfo, len(users))
	for _, user := range users {
		userMap[user.PublicID] = user
	}

	assistants, err := db.GetAssistantsByPublicIDs(ctx, s.db, uniqueStringSlice(assistantIDs))
	if err != nil {
		return nil, err
	}
	assistantMap := make(map[string]*types.DigitalAssistant, len(assistants))
	for _, assistant := range assistants {
		assistantMap[assistant.PublicID] = assistant
	}

	skillMap, err := s.buildProjectActivitySkillMap(ctx, orgID, skillIDs)
	if err != nil {
		return nil, err
	}
	mcpMap, err := s.buildProjectActivityPluginMap(ctx, orgID, "mcp", mcpIDs)
	if err != nil {
		return nil, err
	}

	items := make([]contract.ProjectActivityItem, 0, len(activities))
	for _, activity := range activities {
		payload := normalizeProjectActivityPayload(activity.Payload)
		item := contract.ProjectActivityItem{
			ID:         activity.ID,
			ProjectID:  activity.ProjectID,
			OperatorID: activity.OperatorID,
			Operator:   userActorFromMap(userMap, activity.OperatorID),
			ActionType: string(activity.ActionType),
			Payload: contract.ProjectActivityPayloadView{
				AddedSkills:        skillRefsFromMap(skillMap, payload.AddedSkillIDs),
				RemovedSkills:      skillRefsFromMap(skillMap, payload.RemovedSkillIDs),
				AddedMCPs:          skillRefsFromMap(mcpMap, payload.AddedMCPIDs),
				RemovedMCPs:        skillRefsFromMap(mcpMap, payload.RemovedMCPIDs),
				AddedMembers:       userRefsFromMap(userMap, payload.AddedMemberIDs),
				RemovedMembers:     userRefsFromMap(userMap, payload.RemovedMemberIDs),
				AddedAITeammates:   s.assistantRefsFromMap(ctx, orgID, assistantMap, payload.AddedAITeammateIDs),
				RemovedAITeammates: s.assistantRefsFromMap(ctx, orgID, assistantMap, payload.RemovedAITeammateIDs),
			},
			CreatedAt: activity.CreatedAt,
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *projectService) buildProjectActivityPluginMap(
	ctx context.Context,
	orgID uint,
	kind string,
	pluginIDs []string,
) (map[string]contract.ProjectActivitySkill, error) {
	ids := uniqueStringSlice(pluginIDs)
	result := make(map[string]contract.ProjectActivitySkill, len(ids))
	for _, id := range ids {
		result[id] = contract.ProjectActivitySkill{ID: id}
	}
	if len(ids) == 0 {
		return result, nil
	}
	plugins, err := db.ListPlugins(ctx, s.db, orgID, db.PluginListFilter{Kind: kind})
	if err != nil {
		return nil, err
	}
	for _, plugin := range plugins {
		if _, requested := result[plugin.PublicID]; requested {
			result[plugin.PublicID] = contract.ProjectActivitySkill{
				ID:   plugin.PublicID,
				Name: plugin.Name,
			}
		}
	}
	return result, nil
}

func (s *projectService) buildProjectActivitySkillMap(ctx context.Context, orgID uint, skillIDs []string) (map[string]contract.ProjectActivitySkill, error) {
	ids := uniqueStringSlice(skillIDs)
	result := make(map[string]contract.ProjectActivitySkill, len(ids))
	for _, id := range ids {
		result[id] = contract.ProjectActivitySkill{ID: id}
	}
	if len(ids) == 0 {
		return result, nil
	}

	plugins, err := db.ListPlugins(ctx, s.db, orgID, db.PluginListFilter{Kind: "skill"})
	if err != nil {
		return nil, err
	}
	pluginByID := make(map[string]types.Plugin, len(plugins))
	pluginByCode := make(map[string]types.Plugin, len(plugins))
	for _, p := range plugins {
		pluginByID[p.PublicID] = p
		pluginByCode[p.Code] = p
	}
	for _, id := range ids {
		p, ok := pluginByID[id]
		if !ok {
			p, ok = pluginByCode[id]
		}
		if ok {
			result[id] = contract.ProjectActivitySkill{ID: p.PublicID, Name: p.Name}
		}
	}
	return result, nil
}

func (s *projectService) publicIDForUser(ctx context.Context, uin uint) (string, error) {
	user, err := s.userRepo.GetUserByUin(ctx, uin)
	if err != nil {
		return "", err
	}
	if user == nil || strings.TrimSpace(user.PublicID) == "" {
		return "", fmt.Errorf("user %d public_id not found", uin)
	}
	return user.PublicID, nil
}

func (s *projectService) publicIDsForUsers(ctx context.Context, ids []uint) ([]string, error) {
	if len(ids) == 0 {
		return []string{}, nil
	}
	userMap, err := s.userRepo.GetUsersByUins(ctx, uniqueUintSlice(ids))
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		user := userMap[id]
		publicID := ""
		if user != nil {
			publicID = strings.TrimSpace(user.PublicID)
		}
		if publicID == "" {
			return nil, fmt.Errorf("user %d public_id not found", id)
		}
		result = append(result, publicID)
	}
	return result, nil
}

func publicIDsForAssistants(ctx context.Context, database *gorm.DB, ids []uint) ([]string, error) {
	if len(ids) == 0 {
		return []string{}, nil
	}
	assistants, err := db.GetAssistantsByIDs(ctx, database, uniqueUintSlice(ids))
	if err != nil {
		return nil, err
	}
	publicIDByID := make(map[uint]string, len(assistants))
	for _, assistant := range assistants {
		publicIDByID[assistant.ID] = assistant.PublicID
	}
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		publicID := strings.TrimSpace(publicIDByID[id])
		if publicID == "" {
			return nil, fmt.Errorf("digital assistant %d public_id not found", id)
		}
		result = append(result, publicID)
	}
	return result, nil
}

func userActorFromMap(users map[string]*account.UserInfo, id string) *contract.ProjectActivityActor {
	user, ok := users[id]
	if !ok {
		return &contract.ProjectActivityActor{ID: id}
	}
	return &contract.ProjectActivityActor{
		ID:        id,
		Name:      user.Name,
		AvatarURL: user.AvatarURL,
	}
}

func userRefsFromMap(users map[string]*account.UserInfo, ids []string) []contract.ProjectActivityActor {
	refs := make([]contract.ProjectActivityActor, 0, len(ids))
	for _, id := range ids {
		actor := userActorFromMap(users, id)
		refs = append(refs, *actor)
	}
	return refs
}

func (s *projectService) assistantRefsFromMap(ctx context.Context, orgID uint, assistants map[string]*types.DigitalAssistant, ids []string) []contract.ProjectActivityActor {
	refs := make([]contract.ProjectActivityActor, 0, len(ids))
	for _, id := range ids {
		ref := contract.ProjectActivityActor{ID: id}
		if assistant, ok := assistants[id]; ok {
			ref.Name = assistant.Name
			ref.AvatarURL = resolveAvatarField(ctx, s.db, orgID, assistant.Avatar)
		}
		refs = append(refs, ref)
	}
	return refs
}

func skillRefsFromMap(skills map[string]contract.ProjectActivitySkill, ids []string) []contract.ProjectActivitySkill {
	refs := make([]contract.ProjectActivitySkill, 0, len(ids))
	for _, id := range ids {
		if skill, ok := skills[id]; ok {
			refs = append(refs, skill)
			continue
		}
		refs = append(refs, contract.ProjectActivitySkill{ID: id})
	}
	return refs
}

func mergeProjectActivityOperatorIDs(operatorID string, operatorIDs []string) []string {
	seen := make(map[string]struct{}, len(operatorIDs)+1)
	result := make([]string, 0, len(operatorIDs)+1)
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	add(operatorID)
	for _, id := range operatorIDs {
		add(id)
	}
	return result
}

func encodeProjectActivityCursor(cursor projectActivityCursor) string {
	data, err := json.Marshal(cursor)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeProjectActivityCursor(value string) (projectActivityCursor, error) {
	var cursor projectActivityCursor
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return cursor, errors.New("invalid cursor")
	}
	if err := json.Unmarshal(data, &cursor); err != nil {
		return cursor, errors.New("invalid cursor")
	}
	if cursor.ID == 0 || cursor.CreatedAt.IsZero() {
		return cursor, errors.New("invalid cursor")
	}
	return cursor, nil
}

func diffUintSlices(oldIDs, newIDs []uint) (added, removed []uint) {
	oldSet := make(map[uint]bool, len(oldIDs))
	newSet := make(map[uint]bool, len(newIDs))
	for _, id := range oldIDs {
		if id > 0 {
			oldSet[id] = true
		}
	}
	for _, id := range newIDs {
		if id == 0 {
			continue
		}
		newSet[id] = true
		if !oldSet[id] {
			added = append(added, id)
		}
	}
	for _, id := range oldIDs {
		if id > 0 && !newSet[id] {
			removed = append(removed, id)
		}
	}
	return added, removed
}

func uniqueUintSlice(values []uint) []uint {
	seen := make(map[uint]bool, len(values))
	result := make([]uint, 0, len(values))
	for _, value := range values {
		if value == 0 || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i] < result[j]
	})
	return result
}

func uniqueStringSlice(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

// validateAssistantIDs 校验 assistant ID 列表：去重、非零、不出现在默认 assistant 中。
func validateAssistantIDs(ids []uint, defaultAssistantID uint) error {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[uint]bool, len(ids))
	for _, id := range ids {
		if id == 0 {
			return ErrInvalidAssistantID
		}
		if id == defaultAssistantID {
			return fmt.Errorf("default assistant %d cannot be specified as extra", id)
		}
		if seen[id] {
			return fmt.Errorf("%w: %d", ErrDuplicateAssistant, id)
		}
		seen[id] = true
	}
	return nil
}

func convertToContractProject(project *types.Project) *contract.Project {
	if project == nil {
		return nil
	}

	var metadata map[string]interface{}
	m := make(map[string]interface{})
	if len(project.Metadata.Tags) > 0 {
		m["tags"] = project.Metadata.Tags
	}
	if project.Metadata.Type != "" {
		m["type"] = project.Metadata.Type
	}
	if project.Metadata.Extra != nil && len(project.Metadata.Extra) > 0 {
		m["extra"] = project.Metadata.Extra
	}
	if len(m) > 0 {
		metadata = m
	}

	return &contract.Project{
		PublicID:     project.PublicID,
		Name:         project.Name,
		Description:  project.Description,
		Objective:    project.Objective,
		OwnerID:      project.OwnerID,
		Status:       project.Status,
		Metadata:     metadata,
		AutomationID: project.AutomationID,
		CreatedAt:    project.CreatedAt,
		UpdatedAt:    project.UpdatedAt,
	}
}

func buildWorkbenchRecentContext(project *types.Project, task *types.Task, usedAt time.Time) *contract.WorkbenchRecentContext {
	if project == nil {
		return nil
	}

	var taskID *string
	var taskTitle *string
	if task != nil {
		// 中文注释：任务为空表示用户最近只选中了项目，首页应回显为“新建任务”入口。
		taskID = &task.PublicID
		taskTitle = &task.Title
	}

	return &contract.WorkbenchRecentContext{
		ProjectID:   project.PublicID,
		ProjectName: project.Name,
		TaskID:      taskID,
		TaskTitle:   taskTitle,
		UsedAt:      usedAt,
	}
}

func (s *projectService) DetailProject(ctx context.Context, publicID string) (*contract.ProjectDetail, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(publicID) == "" {
		return nil, errors.New("public_id is required")
	}

	project, err := db.GetProjectByPublicID(ctx, s.db, caller.OrgID, publicID)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, errors.New("project not found")
	}

	result := &contract.ProjectDetail{
		Project: *convertToContractProject(project),
		Tasks:   make([]contract.ProjectTaskItem, 0),
		Members: make([]contract.ProjectMemberItem, 0),
	}

	// 查询项目会话
	prjSession, _ := db.GetProjectSession(ctx, s.db, project.ID)
	if prjSession != nil {
		result.Session = convertToContractSession(ctx, prjSession, s.db)
	}

	// 查询项目任务
	tasks, err := db.ListTasksByProjectID(ctx, s.db, caller.OrgID, project.ID)
	if err != nil {
		return nil, err
	}

	// 收集任务会话ID，批量查询会话
	taskSessionIDs := make([]uint, 0)
	taskIDs := make([]uint, 0, len(tasks))
	for _, t := range tasks {
		taskIDs = append(taskIDs, t.ID)
		if t.SessionID != nil {
			taskSessionIDs = append(taskSessionIDs, *t.SessionID)
		}
	}

	taskSessions, err := db.GetSessionsByIDs(ctx, s.db, taskSessionIDs)
	if err != nil {
		return nil, err
	}
	sessionMap := make(map[uint]*types.Session, len(taskSessions))
	for _, sess := range taskSessions {
		sessionMap[sess.ID] = sess
	}

	canViewAllTasks, err := s.perm.AllowsTaskViewViaProject(ctx, FromTypeCaller(caller), project)
	if err != nil {
		return nil, err
	}

	for _, t := range tasks {
		if !canViewAllTasks {
			if err := s.perm.RequireTask(ctx, FromTypeCaller(caller), t, types.ActionTaskView); err != nil {
				continue
			}
		}
		item := contract.ProjectTaskItem{
			Task: *convertToContractTask(t, project.PublicID, project.Name),
		}
		if t.SessionID != nil {
			if sess, ok := sessionMap[*t.SessionID]; ok {
				item.Session = convertToContractSession(ctx, sess, s.db)
				item.Session.RuntimeStatus = lookupSessionRuntimeStatus(ctx, s.db, sess.ID)
			}
		}
		result.Tasks = append(result.Tasks, item)
	}

	// 查询项目成员：统一从 leros_resource_binding 读取用户与 AI 队友。
	resource, err := db.GetResourceByBizID(ctx, s.db, caller.OrgID, types.ResourceTypeProject, project.ID)
	if err != nil {
		return nil, err
	}
	if resource == nil {
		return result, nil
	}

	bindings, err := db.ListResourceBindingsByResourceID(ctx, s.db, resource.ID)
	if err != nil {
		return nil, err
	}

	defaultAssistantID, _ := db.GetDefaultAssistantIDByOrg(ctx, s.db, caller.OrgID)

	userIDs := make([]uint, 0)
	assistantIDs := make([]uint, 0)
	for _, b := range bindings {
		if b.Uin != nil && *b.Uin != 0 {
			userIDs = append(userIDs, *b.Uin)
			continue
		}
		if b.AssistantID != nil && *b.AssistantID != 0 {
			assistantIDs = append(assistantIDs, *b.AssistantID)
		}
	}

	// 中文注释：资源绑定保存的是组织成员 UIN，必须按 UIN 查询并建索引；不能把 UIN 当作用户表主键使用。
	userMap, err := s.userRepo.GetUsersByUins(ctx, userIDs)
	if err != nil {
		return nil, fmt.Errorf("get users: %w", err)
	}

	assistants, _ := db.GetAssistantsByIDs(ctx, s.db, assistantIDs)
	assistantMap := make(map[uint]*types.DigitalAssistant, len(assistants))
	for _, a := range assistants {
		assistantMap[a.ID] = a
	}

	for _, b := range bindings {
		if b.AssistantID != nil && *b.AssistantID != 0 {
			assistantID := *b.AssistantID
			item := contract.ProjectMemberItem{
				MemberID:   assistantID,
				MemberType: string(types.MemberTypeAssistant),
				MemberRole: string(b.Role),
				IsDefault:  defaultAssistantID > 0 && assistantID == defaultAssistantID,
				JoinedAt:   b.CreatedAt,
			}
			if a, ok := assistantMap[assistantID]; ok {
				// 中文注释：AI 队友同样返回 public_id，避免前端只能用内部 ID 匹配导致漏过滤。
				item.PublicID = a.PublicID
				item.Name = a.Name
				item.AvatarURL = resolveAvatarField(ctx, s.db, caller.OrgID, a.Avatar)
			}
			result.Members = append(result.Members, item)
			continue
		}
		if b.Uin == nil || *b.Uin == 0 {
			continue
		}
		uin := *b.Uin
		item := contract.ProjectMemberItem{
			MemberID:   uin,
			MemberType: string(types.MemberTypeUser),
			MemberRole: string(b.Role),
			IsDefault:  false,
			JoinedAt:   b.CreatedAt,
		}
		if u, ok := userMap[uin]; ok {
			// 中文注释：项目成员弹窗依赖 public_id 判断候选项是否已加入项目。
			item.PublicID = u.PublicID
			item.Name = u.Name
			item.AvatarURL = resolveAvatarField(ctx, s.db, caller.OrgID, u.AvatarURL)
		}
		result.Members = append(result.Members, item)
	}

	return result, nil
}

func resolveAvatarField(ctx context.Context, gdb *gorm.DB, orgID uint, avatar string) string {
	if !account.IsFilePublicID(avatar) {
		return avatar
	}
	fileUpload, err := db.GetFileUploadByPublicID(ctx, gdb, orgID, avatar)
	if err != nil || fileUpload == nil {
		return avatar
	}
	url, err := filestore.ResolvePublicURL(ctx, fileUpload.StorageURI)
	if err != nil {
		return avatar
	}
	return url
}

func (s *projectService) GetProjectMemory(ctx context.Context, publicID string) (*contract.ProjectMemory, error) {
	// 1. 鉴权
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(publicID) == "" {
		return nil, errors.New("public_id is required")
	}

	// 2. 查项目（org 隔离）
	project, err := db.GetProjectByPublicID(ctx, s.db, caller.OrgID, publicID)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, errors.New("project not found")
	}

	// 3. 拼 repo 路径: {workspaceRoot}/projects/{orgID}/{publicID}/repo/
	workerID, err := resolveProjectWorkerID(ctx, s.db, project.OrgID, project.ID, s.inferrer)
	if err != nil {
		return nil, fmt.Errorf("resolve project worker: %w", err)
	}
	repoDir, err := workspace.ProjectRepoPath(project.OrgID, workerID, publicID)
	if err != nil {
		return nil, err
	}

	// 4. 读取 MEMORY.md
	memoryPath := workspace.ProjectMemoryPath(repoDir)
	entries, err := localmemory.ReadEntries(memoryPath)
	if err != nil {
		// 文件不存在或不可读时返回空列表而非报错
		if os.IsNotExist(err) {
			return &contract.ProjectMemory{
				Entries: []string{},
				Total:   0,
			}, nil
		}
		return nil, fmt.Errorf("read project memory: %w", err)
	}

	if entries == nil {
		entries = []string{}
	}

	return &contract.ProjectMemory{
		Entries: entries,
		Total:   len(entries),
	}, nil
}

func (s *projectService) GetProjectFileTree(ctx context.Context, publicID string, query contract.ProjectFileTreeQuery) ([]*contract.FileTreeNode, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(publicID) == "" {
		return nil, errors.New("public_id is required")
	}

	project, err := db.GetProjectByPublicID(ctx, s.db, caller.OrgID, publicID)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, errors.New("project not found")
	}

	filter := db.ProjectFileListFilter{
		ResourceType: strings.TrimSpace(query.ResourceType),
		FileExt:      strings.TrimSpace(query.FileExt),
	}
	taskPublicID := strings.TrimSpace(query.TaskPublicID)
	if taskPublicID != "" {
		task, err := db.GetTaskByPublicID(ctx, s.db, caller.OrgID, taskPublicID)
		if err != nil {
			return nil, err
		}
		if task == nil {
			return nil, errors.New("task not found")
		}
		if task.ProjectID != project.ID {
			return nil, errors.New("task does not belong to this project")
		}
		filter.TaskID = task.ID
	}

	files, err := db.ListProjectFilesFiltered(ctx, s.db, caller.OrgID, project.ID, filter)
	if err != nil {
		return nil, fmt.Errorf("list project files: %w", err)
	}

	authorized := s.perm.FilterProjectFilesByAction(ctx, FromTypeCaller(caller), files, projectFileViewAction)

	return buildFileTreeFromProjectFiles(ctx, s.db, authorized), nil
}

// DownloadProjectFile 通过 project_file 表和 filestore 下载/预览项目文件。
func (s *projectService) DownloadProjectFile(ctx context.Context, publicID string, filePath string) (io.ReadCloser, string, int64, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, "", 0, err
	}
	if strings.TrimSpace(publicID) == "" {
		return nil, "", 0, errors.New("public_id is required")
	}
	if strings.TrimSpace(filePath) == "" {
		return nil, "", 0, errors.New("file path is required")
	}

	project, err := db.GetProjectByPublicID(ctx, s.db, caller.OrgID, publicID)
	if err != nil {
		return nil, "", 0, err
	}
	if project == nil {
		return nil, "", 0, errors.New("project not found")
	}

	if !isPathAllowed(filePath) {
		return nil, "", 0, errors.New("file access denied")
	}
	normalizedPath, err := workspace.NormalizeRelativePath(filePath)
	if err != nil {
		return nil, "", 0, errors.New("file path is required")
	}
	target, err := db.GetLatestProjectFileByRelativePath(
		ctx,
		s.db,
		caller.OrgID,
		project.ID,
		types.ProjectFileResourceTypeUserUpload,
		normalizedPath,
	)
	if err != nil {
		return nil, "", 0, fmt.Errorf("get latest project file: %w", err)
	}
	if target == nil {
		target, err = db.GetLatestProjectFileByRelativePath(
			ctx,
			s.db,
			caller.OrgID,
			project.ID,
			types.ProjectFileResourceTypeArtifact,
			normalizedPath,
		)
		if err != nil {
			return nil, "", 0, fmt.Errorf("get latest project file: %w", err)
		}
	}
	if target == nil {
		target, err = db.GetLatestProjectFileByRelativePath(
			ctx,
			s.db,
			caller.OrgID,
			project.ID,
			types.ProjectFileResourceTypePlan,
			normalizedPath,
		)
		if err != nil {
			return nil, "", 0, fmt.Errorf("get latest project file: %w", err)
		}
	}
	if err != nil {
		return nil, "", 0, fmt.Errorf("get latest project file: %w", err)
	}
	if target == nil {
		files, err := db.ListProjectFiles(ctx, s.db, caller.OrgID, project.ID, "")
		if err != nil {
			return nil, "", 0, fmt.Errorf("list project files: %w", err)
		}
		fileName := filepath.Base(normalizedPath)
		for i := range files {
			fileUpload, err := db.GetFileUploadByPublicID(ctx, s.db, caller.OrgID, files[i].FilePublicID)
			if err != nil {
				return nil, "", 0, fmt.Errorf("get file upload: %w", err)
			}
			if fileUpload != nil && (fileUpload.OriginalName == fileName || fileUpload.Filename == fileName) {
				target = &files[i]
				break
			}
		}
	}
	if target == nil {
		return nil, "", 0, errors.New("file not found")
	}
	if err := s.perm.RequireProjectFile(ctx, FromTypeCaller(caller), target, projectFileDownloadAction(target)); err != nil {
		return nil, "", 0, err
	}
	return openProjectFileVersion(ctx, s.db, caller.OrgID, target.FilePublicID)
}

// DownloadProjectFileByPublicID downloads one concrete project file version.
func (s *projectService) DownloadProjectFileByPublicID(
	ctx context.Context,
	publicID string,
	filePublicID string,
) (io.ReadCloser, string, int64, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, "", 0, err
	}
	project, err := db.GetProjectByPublicID(ctx, s.db, caller.OrgID, strings.TrimSpace(publicID))
	if err != nil {
		return nil, "", 0, err
	}
	if project == nil {
		return nil, "", 0, errors.New("project not found")
	}
	if err := s.perm.RequireProject(ctx, FromTypeCaller(caller), project, types.ActionProjectView); err != nil {
		return nil, "", 0, err
	}
	file, err := db.GetProjectFileByProjectAndFilePublicID(
		ctx,
		s.db,
		caller.OrgID,
		project.ID,
		strings.TrimSpace(filePublicID),
	)
	if err != nil {
		return nil, "", 0, err
	}
	if file == nil {
		return nil, "", 0, errors.New("file not found")
	}
	return openProjectFileVersion(ctx, s.db, caller.OrgID, file.FilePublicID)
}

// GetProjectFileVersions returns every version in the selected file's initial-file chain.
func (s *projectService) GetProjectFileVersions(
	ctx context.Context,
	publicID string,
	filePublicID string,
) (*contract.ProjectFileVersionList, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	project, err := db.GetProjectByPublicID(ctx, s.db, caller.OrgID, strings.TrimSpace(publicID))
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, errors.New("project not found")
	}
	if err := s.perm.RequireProject(ctx, FromTypeCaller(caller), project, types.ActionProjectView); err != nil {
		return nil, err
	}
	selected, err := db.GetProjectFileByProjectAndFilePublicID(
		ctx,
		s.db,
		caller.OrgID,
		project.ID,
		strings.TrimSpace(filePublicID),
	)
	if err != nil {
		return nil, err
	}
	if selected == nil {
		return nil, errors.New("file not found")
	}
	initialID := selected.InitialFilePublicID
	if initialID == "" {
		initialID = selected.FilePublicID
	}
	versions, err := db.ListProjectFileVersions(ctx, s.db, caller.OrgID, project.ID, initialID)
	if err != nil {
		return nil, fmt.Errorf("list project file versions: %w", err)
	}
	result := &contract.ProjectFileVersionList{
		InitialFilePublicID: initialID,
		Items:               make([]contract.ProjectFileVersion, 0, len(versions)),
	}
	for i := range versions {
		fileUpload, err := db.GetFileUploadByPublicID(ctx, s.db, caller.OrgID, versions[i].FilePublicID)
		if err != nil {
			return nil, fmt.Errorf("get file upload: %w", err)
		}
		if fileUpload == nil {
			continue
		}
		if result.CurrentFilePublicID == "" {
			result.CurrentFilePublicID = versions[i].FilePublicID
		}
		result.Items = append(result.Items, contract.ProjectFileVersion{
			PublicID:            versions[i].FilePublicID,
			InitialFilePublicID: initialID,
			RelativePath:        versions[i].RelativePath,
			Name:                filepath.Base(versions[i].RelativePath),
			VersionNo:           versions[i].VersionNo,
			VersionLabel:        fmt.Sprintf("第 %d 版", versions[i].VersionNo),
			Size:                fileUpload.FileSize,
			MimeType:            fileUpload.MimeType,
			CreatedAt:           versions[i].CreatedAt.Unix(),
			StorageURI:          fileUpload.StorageURI,
			Sha256:              fileUpload.Sha256,
		})
	}
	return result, nil
}

// RestoreProjectFileVersion restores one stored version to the project path and creates a new version.
func (s *projectService) RestoreProjectFileVersion(
	ctx context.Context,
	publicID string,
	filePublicID string,
) (*contract.FileTreeNode, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	project, err := db.GetProjectByPublicID(ctx, s.db, caller.OrgID, strings.TrimSpace(publicID))
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, errors.New("project not found")
	}
	if err := s.perm.RequireProject(ctx, FromTypeCaller(caller), project, types.ActionProjectView); err != nil {
		return nil, err
	}
	target, err := db.GetProjectFileByProjectAndFilePublicID(
		ctx,
		s.db,
		caller.OrgID,
		project.ID,
		strings.TrimSpace(filePublicID),
	)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, errors.New("file not found")
	}
	if target.ResourceType != types.ProjectFileResourceTypeArtifact {
		return nil, errors.New("only artifact versions can be restored")
	}
	relativePath, err := workspace.NormalizeRelativePath(target.RelativePath)
	if err != nil {
		return nil, errors.New("file access denied")
	}
	if err := s.perm.RequireProjectFile(ctx, FromTypeCaller(caller), target, projectFileDownloadAction(target)); err != nil {
		return nil, err
	}

	workerID, err := resolveProjectWorkerID(ctx, s.db, project.OrgID, project.ID, s.inferrer)
	if err != nil {
		return nil, fmt.Errorf("resolve project worker: %w", err)
	}
	downloadURL, targetUpload, err := filestore.PresignDownloadByPublicID(
		ctx,
		s.db,
		caller.OrgID,
		target.FilePublicID,
		10*time.Minute,
	)
	if err != nil {
		return nil, fmt.Errorf("presign project file version download: %w", err)
	}
	if s.publisher == nil {
		return nil, errors.New("worker command publisher is unavailable")
	}
	commandResult, err := s.requestProjectFileRestore(
		ctx,
		project.OrgID,
		workerID,
		project.PublicID,
		relativePath,
		project.GiteaDefaultBranch,
		downloadURL,
		caller,
	)
	if err != nil {
		return nil, err
	}
	if !commandResult.Success {
		return nil, fmt.Errorf("worker restore project file failed: %s", strings.TrimSpace(commandResult.Error))
	}

	reader, _, err := filestore.OpenFileByPublicID(ctx, s.db, caller.OrgID, target.FilePublicID)
	if err != nil {
		return nil, fmt.Errorf("open restored project file version: %w", err)
	}
	data, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read restored project file version: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close restored project file version: %w", closeErr)
	}

	fileName := filepath.Base(relativePath)
	mimeType := strings.TrimSpace(targetUpload.MimeType)
	if mimeType == "" {
		mimeType = mimeTypeByExt(fileName)
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	objectKey := fmt.Sprintf(
		"projects/%d/%s/artifacts/%s%s",
		caller.OrgID,
		project.PublicID,
		snowflake.GenerateIDBase58(),
		filepath.Ext(fileName),
	)

	var restoredFile *types.ProjectFile
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		fileUpload, err := filestore.Upload(ctx, tx, filestore.UploadParams{
			Data:         data,
			Filename:     fileName,
			OriginalName: targetUpload.OriginalName,
			MimeType:     mimeType,
			OrgID:        caller.OrgID,
			OwnerID:      caller.Uin,
			ObjectKey:    objectKey,
			Purpose:      filestore.PurposeArtifact,
		})
		if err != nil {
			return fmt.Errorf("upload restored project file: %w", err)
		}

		restoredFile = &types.ProjectFile{
			FilePublicID: fileUpload.PublicID,
			OrgID:        target.OrgID,
			ProjectID:    target.ProjectID,
			TaskID:       target.TaskID,
			ResourceID:   fileUpload.ID,
			ResourceType: target.ResourceType,
			Uin:          caller.Uin,
			RelativePath: relativePath,
		}
		if err := db.CreateProjectFileVersion(ctx, tx, restoredFile); err != nil {
			return fmt.Errorf("create restored project file version: %w", err)
		}
		projResource, err := db.GetResourceByBizID(ctx, tx, project.OrgID, types.ResourceTypeProject, project.ID)
		if err != nil {
			return fmt.Errorf("get project resource for restored file: %w", err)
		}
		if projResource != nil {
			resourceType := types.ResourceTypeFile
			if restoredFile.ResourceType == types.ProjectFileResourceTypeArtifact {
				resourceType = types.ResourceTypeArtifact
			}
			parentID := projResource.ID
			if err := db.CreateResource(ctx, tx, &types.Resource{
				OrgID:                 project.OrgID,
				Uin:                   caller.Uin,
				Type:                  resourceType,
				BizID:                 restoredFile.ID,
				ParentResourceID:      &parentID,
				ParentResourcePathIDs: types.ResourcePathIDs{projResource.ID},
			}); err != nil {
				return fmt.Errorf("sync restored project file resource: %w", err)
			}
		}
		if err := db.TouchProjectUpdatedAt(ctx, tx, project.ID, time.Now()); err != nil {
			return fmt.Errorf("touch project updated_at: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	nodes := buildFileTreeFromProjectFiles(ctx, s.db, []types.ProjectFile{*restoredFile})
	if len(nodes) == 0 {
		return nil, errors.New("file not found")
	}
	return nodes[0], nil
}

func (s *projectService) requestProjectFileRestore(
	ctx context.Context,
	orgID uint,
	workerID uint,
	projectPublicID string,
	relativePath string,
	branch string,
	downloadURL string,
	caller *types.Caller,
) (messaging.ProjectFileRestoreResult, error) {
	topic, err := messaging.WorkerCommandSubject(orgID, workerID, messaging.LaneFile)
	if err != nil {
		return messaging.ProjectFileRestoreResult{}, fmt.Errorf("build project file command topic: %w", err)
	}
	name := "leros-project-file-restore"
	email := "project-file-restore@leros.local"
	if caller != nil {
		name = fmt.Sprintf("leros-user-%d", caller.Uin)
		email = fmt.Sprintf("user-%d@org-%d.leros.local", caller.Uin, caller.OrgID)
	}
	command := withRequestTrace(ctx, messaging.NewProjectFileRestoreCommand(
		"project-file-restore-"+snowflake.GenerateIDBase58(),
		messaging.RouteContext{OrgID: orgID, WorkerID: workerID},
		messaging.ProjectFileRestoreCommandPayload{
			ProjectPublicID: projectPublicID,
			RelativePath:    relativePath,
			Branch:          branch,
			DownloadURL:     downloadURL,
			AuthorName:      name,
			AuthorEmail:     email,
		},
	))
	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	reply, err := s.publisher.Request(requestCtx, topic, command)
	if err != nil {
		return messaging.ProjectFileRestoreResult{}, fmt.Errorf("request worker project file restore: %w", err)
	}
	var result messaging.ProjectFileRestoreResult
	if err := json.Unmarshal(reply.Data, &result); err != nil {
		return messaging.ProjectFileRestoreResult{}, fmt.Errorf("decode worker project file restore result: %w", err)
	}
	return result, nil
}

func openProjectFileVersion(
	ctx context.Context,
	dbConn *gorm.DB,
	orgID uint,
	filePublicID string,
) (io.ReadCloser, string, int64, error) {
	fileUpload, err := db.GetFileUploadByPublicID(ctx, dbConn, orgID, filePublicID)
	if err != nil {
		return nil, "", 0, fmt.Errorf("get file upload: %w", err)
	}
	if fileUpload == nil {
		return nil, "", 0, errors.New("file not found")
	}

	objectKey, err := storageKeyFromFilestoreURI(fileUpload.StorageURI)
	if err != nil {
		return nil, "", 0, fmt.Errorf("parse storage path: %w", err)
	}

	st := filestore.GetStorage()
	obj, err := st.GetObject(ctx, filestore.DefaultBucket(), objectKey)
	if err != nil {
		return nil, "", 0, fmt.Errorf("read file from storage: %w", err)
	}

	return obj.Body, fileUpload.MimeType, fileUpload.FileSize, nil
}

func generateProjectPublicID() string {
	return fmt.Sprintf("prj_%s", snowflake.GenerateIDBase58())
}

func (s *projectService) buildRepoName(orgID uint, projectPublicID string) string {
	return fmt.Sprintf("%s-%d-%s", s.env, orgID, projectPublicID)
}

var visibleFolders = []string{consts.RepoDirArtifacts + "/", consts.RepoDirUploads + "/"}

var ignoredFiles = map[string]bool{".gitkeep": true}

func isPathAllowed(filePath string) bool {
	name := filepath.Base(filePath)
	if ignoredFiles[name] {
		return false
	}
	for _, prefix := range visibleFolders {
		if strings.HasPrefix(filePath, prefix) {
			return true
		}
	}
	return false
}

// lookupFileCreatedAt 已移除，创建时间现在直接使用 ProjectFile.CreatedAt。
// 此文件中的一切 Gitea API 调用仅用于 Gitea 启用时的仓库初始化和 commit 记录查询。

func mimeTypeByExt(filename string) string {
	ext := filepath.Ext(filename)
	if mimeType := mime.TypeByExtension(ext); mimeType != "" {
		return mimeType
	}
	return ""
}

// buildFileTreeFromProjectFiles 将 ProjectFile 列表转换为平铺的 FileTreeNode 列表。
func buildFileTreeFromProjectFiles(ctx context.Context, dbParam *gorm.DB, files []types.ProjectFile) []*contract.FileTreeNode {
	var nodes []*contract.FileTreeNode
	for _, pf := range files {
		node := projectFileToTreeNode(ctx, dbParam, pf)
		if node != nil {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

func projectFileToTreeNode(
	ctx context.Context,
	dbParam *gorm.DB,
	pf types.ProjectFile,
) *contract.FileTreeNode {
	fullPath := strings.TrimSpace(pf.RelativePath)
	if fullPath == "" {
		return nil
	}

	name := filepath.Base(strings.TrimSuffix(fullPath, "/"))
	if name == "" || name == "." {
		name = strings.TrimSuffix(fullPath, "/")
	}

	initialID := pf.InitialFilePublicID
	if initialID == "" {
		initialID = pf.FilePublicID
	}

	node := &contract.FileTreeNode{
		Name:                name,
		Path:                fullPath,
		Type:                "file",
		ModTime:             pf.UpdatedAt.Unix(),
		CreatedAt:           pf.CreatedAt.Unix(),
		PublicID:            pf.FilePublicID,
		InitialFilePublicID: initialID,
		VersionNo:           pf.VersionNo,
		VersionLabel:        fmt.Sprintf("第 %d 版", pf.VersionNo),
		VersionCount:        pf.VersionNo,
		ResourceType:        string(pf.ResourceType),
	}

	fileUpload, err := db.GetFileUploadByPublicID(ctx, dbParam, pf.OrgID, pf.FilePublicID)
	if err != nil || fileUpload == nil {
		return node
	}
	node.Size = fileUpload.FileSize
	node.MimeType = fileUpload.MimeType
	node.StorageURI = fileUpload.StorageURI
	node.Sha256 = fileUpload.Sha256
	if node.Name == "" || node.Name == "." {
		node.Name = filepath.Base(fileUpload.OriginalName)
	}
	return node
}

func storageKeyFromFilestoreURI(uri string) (string, error) {
	_, _, key, err := storage.ParseURI(uri)
	if err != nil {
		return "", fmt.Errorf("parse storage uri: %w", err)
	}
	return key, nil
}

// ensure project implements contract.ProjectService at compile time
var _ contract.ProjectService = (*projectService)(nil)
