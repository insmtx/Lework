package service

import (
	"context"
	"fmt"
	"time"

	"code.gitea.io/sdk/gitea"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/infra/git"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/logs"
)

// AutomationProjectProvisioner 负责自动化的专属项目懒创建与换代。
//
// 首次执行时创建新一代项目；项目被软删除后，下一次执行创建下一代项目。
// 以 (automation_id, generation) 幂等：重试先恢复已有项目，不重复创建。
type AutomationProjectProvisioner struct {
	db          *gorm.DB
	giteaClient *gitea.Client
	giteaCfg    *config.GiteaConfig
	env         string
}

// NewAutomationProjectProvisioner 构造 provisioner
func NewAutomationProjectProvisioner(db *gorm.DB, giteaClient *gitea.Client, giteaCfg *config.GiteaConfig, env string) *AutomationProjectProvisioner {
	return &AutomationProjectProvisioner{db: db, giteaClient: giteaClient, giteaCfg: giteaCfg, env: env}
}

// EnsureProject 返回自动化当前的可用项目；不存在或已删时创建新一代。
func (p *AutomationProjectProvisioner) EnsureProject(ctx context.Context, a *types.Automation) (*types.Project, error) {
	// 1. 复用当前项目（若仍存在且未删除）
	if a.ProjectID != nil {
		proj, err := db.GetProjectByID(ctx, p.db, *a.ProjectID)
		if err != nil {
			return nil, err
		}
		if proj != nil && !proj.DeletedAt.Valid {
			return proj, nil
		}
	}

	// 2. 创建新一代项目（generation+1）
	generation := a.ProjectGeneration + 1
	proj := &types.Project{}
	err := p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 幂等恢复：并发下另一实例可能已创建
		existing, err := db.GetAutomationProjectByGeneration(ctx, tx, a.OrgID, a.ID, generation)
		if err != nil {
			return err
		}
		if existing != nil {
			*proj = *existing
			return nil
		}

		created, err := p.createProject(ctx, tx, a, generation)
		if err != nil {
			return err
		}
		*proj = *created
		return nil
	})
	if err != nil {
		return nil, err
	}

	// 3. 回填 Automation.ProjectID / ProjectGeneration
	if err := p.db.WithContext(ctx).Model(&types.Automation{}).
		Where("id = ?", a.ID).
		Updates(map[string]interface{}{
			"project_id":         proj.ID,
			"project_generation": generation,
			"updated_at":         time.Now(),
		}).Error; err != nil {
		logs.WarnContextf(ctx, "provisioner backfill automation project failed: %v", err)
	}
	return proj, nil
}

func (p *AutomationProjectProvisioner) createProject(ctx context.Context, tx *gorm.DB, a *types.Automation, generation int) (*types.Project, error) {
	// 稳定派生项目 public_id：{automation_public_id}_{generation}。
	// 重试先按 (automation_id, generation) 恢复已有项目，不重复创建，也不依赖随机键，
	// 降低 DB/Gitea 部分成功造成的孤儿资源风险。
	id := fmt.Sprintf("prj_%s_%d", a.PublicID, generation)
	project := &types.Project{
		PublicID:             id,
		OrgID:                a.OrgID,
		OwnerID:              a.OwnerID,
		Name:                 a.Name,
		Status:               string(types.ProjectStatusActive),
		GiteaDefaultBranch:   "main",
		AutomationID:         &a.ID,
		AutomationGeneration: generation,
	}

	// 创建 Gitea 仓库（仓库名遵循 worker clone 规则：{env}-{orgID}-{projectPublicID}）。
	// Gitea 未启用时跳过，不阻塞项目创建。
	repoName := fmt.Sprintf("%s-%d-%s", p.env, a.OrgID, id)
	if p.giteaClient != nil && p.giteaCfg != nil && p.giteaCfg.Enabled {
		repoInfo, gErr := git.CreateRepoWithRetry(ctx, p.giteaClient, gitea.CreateRepoOption{
			Name:        repoName,
			Description: "",
			Private:     true,
			AutoInit:    false,
		})
		if gErr != nil {
			return nil, fmt.Errorf("create gitea repo: %w", gErr)
		}
		if repoInfo == nil || repoInfo.FullName == "" {
			return nil, fmt.Errorf("create gitea repo: incomplete response (project=%s repo=%s)", id, repoName)
		}
		project.GiteaRepoFullName = repoInfo.FullName
		project.GiteaRepoID = repoInfo.ID
	}

	if err := db.CreateProject(ctx, tx, project); err != nil {
		return nil, err
	}

	// Gitea 仓库初始结构（幂等；失败仅告警不阻塞）
	if project.GiteaRepoFullName != "" {
		if iErr := git.InitRepoStructure(ctx, p.giteaClient, project.GiteaRepoFullName); iErr != nil {
			logs.WarnContextf(ctx, "provisioner init gitea repo structure: %v", iErr)
		}
	}

	// 项目级 Session（项目协作会话）
	if err := ensureAutomationProjectSession(ctx, tx, project, a.OwnerID); err != nil {
		return nil, err
	}

	// 项目资源 + 创建者 owner 绑定（可见性来源）
	resource := &types.Resource{
		OrgID: a.OrgID,
		Uin:   a.OwnerID,
		Type:  types.ResourceTypeProject,
		BizID: project.ID,
	}
	if err := db.CreateResource(ctx, tx, resource); err != nil {
		return nil, err
	}
	ownerUin := a.OwnerID
	if err := db.CreateResourceBinding(ctx, tx, &types.ResourceBinding{
		OrgID:      a.OrgID,
		Uin:        &ownerUin,
		ResourceID: resource.ID,
		Role:       types.ResourceRoleOwner,
	}); err != nil {
		return nil, err
	}

	// 绑定固定 AI 队友
	assistantUin := a.AssistantID
	existing, err := db.GetResourceBindingByAssistantID(ctx, tx, resource.ID, a.AssistantID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		if err := db.CreateResourceBinding(ctx, tx, &types.ResourceBinding{
			OrgID:       a.OrgID,
			AssistantID: &assistantUin,
			ResourceID:  resource.ID,
			Role:        types.ResourceRoleMember,
		}); err != nil {
			return nil, err
		}
	}

	// 首次绑定组织有效 Skill 与创建者本人可见的 MCP 连接器（best effort，失败不阻塞项目创建）
	bindAutomationProjectPluginsBestEffort(ctx, tx, a, project.ID)

	return project, nil
}

// ensureAutomationProjectSession 创建项目的项目级 Session（幂等：已存在则跳过）。
func ensureAutomationProjectSession(ctx context.Context, tx *gorm.DB, project *types.Project, ownerID uint) error {
	existing, err := db.GetProjectSession(ctx, tx, project.ID)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}
	sessID := fmt.Sprintf("sess_%s", newAutoExecPublicID())
	session := &types.Session{
		PublicID:  sessID,
		Type:      types.SessionTypeProject,
		Uin:       ownerID,
		OrgID:     project.OrgID,
		ProjectID: &project.ID,
		Status:    string(types.SessionStatusActive),
		Title:     "项目协作",
	}
	return db.CreateSession(ctx, tx, session)
}

// bindAutomationProjectPluginsBestEffort 首次创建项目时，将组织有效的 Skill 与创建者
// 本人可见的 MCP 连接器绑定到项目（ProjectPluginBinding）。失败仅记录告警，不阻塞项目创建。
func bindAutomationProjectPluginsBestEffort(ctx context.Context, tx *gorm.DB, a *types.Automation, projectID uint) {
	plugins, err := ListOrgVisiblePluginsForAutomation(ctx, tx, a)
	if err != nil {
		logs.WarnContextf(ctx, "provisioner list org plugins failed: %v", err)
		return
	}
	for _, pl := range plugins {
		binding := &types.ProjectPluginBinding{
			ProjectID: projectID,
			PluginID:  pl.ID,
			Enabled:   true,
			Config:    []byte("{}"),
			CreatedBy: a.OwnerID,
			UpdatedBy: a.OwnerID,
		}
		if err := db.CreateProjectPluginBinding(ctx, tx, binding); err != nil {
			logs.WarnContextf(ctx, "provisioner bind plugin %d to project %d failed: %v", pl.ID, projectID, err)
		}
	}
}

// ListOrgVisiblePluginsForAutomation 返回自动化项目首次创建时应绑定的插件集合。
//
// 规则：
//   - 仅限 owner_scope = organization、org 内、active、存在有效当前修订的插件；
//   - Skill：组织全部可见；
//   - MCP 连接器：仅创建者（automation owner）本人可见（kind='mcp' 且 created_by = owner）。
//
// 通过 JOIN PluginRevision（revision = current_revision）校验当前修订真实存在且有效。
// best effort：若插件模型仍在演进导致查询失败，调用方记录告警而不阻断。
func ListOrgVisiblePluginsForAutomation(ctx context.Context, tx *gorm.DB, a *types.Automation) ([]types.Plugin, error) {
	var plugins []types.Plugin
	err := tx.WithContext(ctx).
		Distinct("p.*").
		Table(types.TableNamePlugin+" AS p").
		Joins("JOIN "+types.TableNamePluginRevision+" AS r ON r.plugin_id = p.id AND r.revision = p.current_revision AND r.deleted_at IS NULL").
		Where("p.owner_scope = ? AND p.org_id = ? AND p.status = ? AND p.deleted_at IS NULL",
			string(types.OwnerScopeOrganization), a.OrgID, types.PluginStatusActive).
		Where("(p.kind <> ? OR p.created_by = ?)", "mcp", a.OwnerID).
		Order("p.id ASC").
		Find(&plugins).Error
	if err != nil {
		return nil, err
	}
	return plugins, nil
}
