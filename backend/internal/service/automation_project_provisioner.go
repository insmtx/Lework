package service

import (
	"context"
	"fmt"

	"code.gitea.io/sdk/gitea"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

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
	var project *types.Project
	err := p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 与关联项目更新共享同一行锁，避免执行器使用旧关联覆盖用户刚保存的选择。
		var automation types.Automation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND org_id = ?", a.ID, a.OrgID).First(&automation).Error; err != nil {
			return err
		}

		// 1. 复用当前项目（若仍存在、未删除且固定 AI 队友仍可执行）。
		if automation.ProjectID != nil {
			current, err := db.GetProjectByID(ctx, tx, *automation.ProjectID)
			if err != nil {
				return err
			}
			if current != nil && !current.DeletedAt.Valid {
				usable, usableErr := p.projectUsable(ctx, tx, &automation, current.ID)
				if usableErr != nil {
					return usableErr
				}
				if usable {
					project = current
					return nil
				}
			}
		}

		// 2. 创建新的、单调递增的专属项目代数；不能复用历史项目，也避免软删除项目的 public_id 冲突。
		latestGeneration, err := db.MaxAutomationProjectGeneration(ctx, tx, automation.OrgID, automation.ID)
		if err != nil {
			return err
		}
		generation := latestGeneration + 1
		created, err := p.createProject(ctx, tx, &automation, generation)
		if err != nil {
			return err
		}
		if err := db.UpdateAutomationProjectLink(ctx, tx, automation.ID, &created.ID, generation); err != nil {
			return err
		}
		project = created
		return nil
	})
	if err != nil {
		return nil, err
	}
	return project, nil
}

// projectUsable 判断自动化当前项目是否仍可复用。
//
// 由于 EnsureProject 由 Dispatcher 调用、无 caller 上下文，这里做**保守**检查：
// ① 固定 AI 队友（automation.AssistantID）仍绑定为该项目成员（否则 execution 无法路由）；
// ② 自动化 owner 仍是项目成员（权限/Create-Update 已强校验，此处兜底成员身份）。
// 任一明确不满足时由上层创建下一代；查询错误必须返回，让 Dispatcher 重试而非永久改写关联。
func (p *AutomationProjectProvisioner) projectUsable(ctx context.Context, database *gorm.DB, a *types.Automation, projectID uint) (bool, error) {
	bound, err := db.IsProjectAssistantBound(ctx, database, a.OrgID, projectID, a.AssistantID)
	if err != nil {
		return false, err
	}
	if !bound {
		return false, nil
	}
	member, err := db.IsProjectUserMember(ctx, database, a.OrgID, a.OwnerID, projectID)
	if err != nil {
		return false, err
	}
	return member, nil
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
