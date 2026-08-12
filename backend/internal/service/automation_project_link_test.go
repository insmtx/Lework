package service

import (
	"context"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

func seedAutomationLinkDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&types.Automation{},
		&types.AutomationExecution{},
		&types.Project{},
		&types.Resource{},
		&types.ResourceBinding{},
		&types.WorkerDeployment{},
		&types.Session{},
		&types.Plugin{},
		&types.PluginRevision{},
		&types.ProjectPluginBinding{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func seedDefaultAssistant(t *testing.T, db *gorm.DB, orgID, assistantID uint) {
	t.Helper()
	deploy := &types.WorkerDeployment{
		PublicID:           "wd_test",
		OrgID:              orgID,
		DigitalAssistantID: assistantID,
		WorkerID:           1,
		DeploymentName:     "default",
		Status:             "ready",
	}
	if err := db.Create(deploy).Error; err != nil {
		t.Fatalf("seed worker deployment: %v", err)
	}
}

// seedLinkedProject 创建项目及其资源与成员绑定（owner=ownerUin、助手=assistantID）。
// assistantID==0 时不创建助手绑定（用于"队友未绑定"用例）。
func seedLinkedProject(t *testing.T, db *gorm.DB, publicID string, orgID, ownerUin, assistantID uint) *types.Project {
	t.Helper()
	project := &types.Project{
		PublicID: publicID,
		Name:     "Link Project",
		OrgID:    orgID,
		OwnerID:  ownerUin,
	}
	if err := db.Create(project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	ctx := context.Background()
	resource := &types.Resource{
		OrgID: orgID,
		Uin:   ownerUin,
		Type:  types.ResourceTypeProject,
		BizID: project.ID,
	}
	if err := infradb.CreateResource(ctx, db, resource); err != nil {
		t.Fatalf("create resource: %v", err)
	}
	ownerUinPtr := ownerUin
	if err := infradb.CreateResourceBinding(ctx, db, &types.ResourceBinding{
		OrgID:      orgID,
		Uin:        &ownerUinPtr,
		ResourceID: resource.ID,
		Role:       types.ResourceRoleOwner,
	}); err != nil {
		t.Fatalf("create owner binding: %v", err)
	}
	if assistantID != 0 {
		asstPtr := assistantID
		if err := infradb.CreateResourceBinding(ctx, db, &types.ResourceBinding{
			OrgID:       orgID,
			AssistantID: &asstPtr,
			ResourceID:  resource.ID,
			Role:        types.ResourceRoleMember,
		}); err != nil {
			t.Fatalf("create assistant binding: %v", err)
		}
	}
	return project
}

func linkTestCaller(orgID, uin uint) context.Context {
	return auth.WithContext(context.Background(), &types.Caller{Uin: uin, OrgID: orgID, State: types.AuthStateSucc}, nil)
}

// linkTargetProject 返回 project_public_id 取值：可选已有项目或切回默认（""）。
func linkReqProject(link *string) *string {
	if link == nil {
		return nil
	}
	v := *link
	return &v
}

func defaultIntervalSchedule() *types.AutomationScheduleFormConfig {
	return &types.AutomationScheduleFormConfig{
		Mode: "interval",
		Interval: &types.AutomationIntervalConfig{
			IntervalSeconds: 300,
			IntervalMinutes: 5,
			IntervalUnit:    "minute",
			AnchorAt:        "00:00",
		},
		Timezone: "Asia/Shanghai",
	}
}

// TestCreateAutomation_DefaultNewProject 创建时省略 project_public_id => 默认新项目（ProjectID nil）。
func TestCreateAutomation_DefaultNewProject(t *testing.T) {
	db := seedAutomationLinkDB(t)
	seedDefaultAssistant(t, db, 1, 99)
	svc := NewAutomationService(db, NewPermissionService(db, NewPermissionCore(db)))

	created, err := svc.CreateAutomation(linkTestCaller(1, 7), &contract.CreateAutomationRequest{
		Name:         "默认项目",
		Instruction:  "指令",
		Enabled:      ptr(true),
		ScheduleMode: "interval",
		Schedule:     defaultIntervalSchedule(),
		Timezone:     "Asia/Shanghai",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ProjectID != nil {
		t.Fatalf("expected nil ProjectID, got %d", *created.ProjectID)
	}
}

// TestCreateAutomation_LinkExistingProject 创建时关联已有项目 => ProjectID 指向该项目、Generation=0。
func TestCreateAutomation_LinkExistingProject(t *testing.T) {
	db := seedAutomationLinkDB(t)
	const asst = 99
	seedDefaultAssistant(t, db, 1, asst)
	proj := seedLinkedProject(t, db, "prj_link", 1, 7, asst)
	svc := NewAutomationService(db, NewPermissionService(db, NewPermissionCore(db)))

	created, err := svc.CreateAutomation(linkTestCaller(1, 7), &contract.CreateAutomationRequest{
		Name:            "关联项目",
		Instruction:     "指令",
		Enabled:         ptr(true),
		ScheduleMode:    "interval",
		Schedule:        defaultIntervalSchedule(),
		Timezone:        "Asia/Shanghai",
		ProjectPublicID: "prj_link",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ProjectID == nil || *created.ProjectID != proj.ID {
		t.Fatalf("expected ProjectID=%d, got %v", proj.ID, created.ProjectID)
	}
	if created.ProjectPublicID != "prj_link" {
		t.Fatalf("expected ProjectPublicID=prj_link, got %q", created.ProjectPublicID)
	}
	// Generation 回显：验证落库为 0
	var stored types.Automation
	if err := db.First(&stored, "public_id = ?", created.PublicID).Error; err != nil {
		t.Fatalf("load automation: %v", err)
	}
	if stored.ProjectGeneration != 0 {
		t.Fatalf("expected ProjectGeneration=0, got %d", stored.ProjectGeneration)
	}
}

// TestCreateAutomation_LinkNotFound 关联不存在的项目 => 404 且不落库。
func TestCreateAutomation_LinkNotFound(t *testing.T) {
	db := seedAutomationLinkDB(t)
	seedDefaultAssistant(t, db, 1, 99)
	svc := NewAutomationService(db, NewPermissionService(db, NewPermissionCore(db)))

	_, err := svc.CreateAutomation(linkTestCaller(1, 7), &contract.CreateAutomationRequest{
		Name:            "不存在",
		Instruction:     "指令",
		Enabled:         ptr(true),
		ScheduleMode:    "interval",
		Schedule:        defaultIntervalSchedule(),
		Timezone:        "Asia/Shanghai",
		ProjectPublicID: "prj_missing",
	})
	if err != ErrAutomationLinkNotFound {
		t.Fatalf("expected ErrAutomationLinkNotFound, got %v", err)
	}
	var count int64
	if err := db.Model(&types.Automation{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("expected 0 automations, got %d (err=%v)", count, err)
	}
}

// TestCreateAutomation_LinkForbidden 关联项目存在但调用者无权限 => 403。
func TestCreateAutomation_LinkForbidden(t *testing.T) {
	db := seedAutomationLinkDB(t)
	const asst = 99
	seedDefaultAssistant(t, db, 1, asst)
	// 项目创建者 owner=7，但调用者 uin=8 非成员 => task:create 被拒
	seedLinkedProject(t, db, "prj_owned", 1, 7, asst)
	svc := NewAutomationService(db, NewPermissionService(db, NewPermissionCore(db)))

	_, err := svc.CreateAutomation(linkTestCaller(1, 8), &contract.CreateAutomationRequest{
		Name:            "无权限",
		Instruction:     "指令",
		Enabled:         ptr(true),
		ScheduleMode:    "interval",
		Schedule:        defaultIntervalSchedule(),
		Timezone:        "Asia/Shanghai",
		ProjectPublicID: "prj_owned",
	})
	if err != ErrAutomationLinkForbidden {
		t.Fatalf("expected ErrAutomationLinkForbidden, got %v", err)
	}
}

// TestCreateAutomation_LinkAssistantUnbound 调用者有权限但固定 AI 队友未绑定 => 400 不可用。
func TestCreateAutomation_LinkAssistantUnbound(t *testing.T) {
	db := seedAutomationLinkDB(t)
	const asst = 99
	seedDefaultAssistant(t, db, 1, asst)
	// 不创建助手绑定（assistantID=0）=> IsProjectAssistantBound 为 false
	seedLinkedProject(t, db, "prj_nobind", 1, 7, 0)
	svc := NewAutomationService(db, NewPermissionService(db, NewPermissionCore(db)))

	_, err := svc.CreateAutomation(linkTestCaller(1, 7), &contract.CreateAutomationRequest{
		Name:            "队友未绑定",
		Instruction:     "指令",
		Enabled:         ptr(true),
		ScheduleMode:    "interval",
		Schedule:        defaultIntervalSchedule(),
		Timezone:        "Asia/Shanghai",
		ProjectPublicID: "prj_nobind",
	})
	if err != ErrAutomationLinkUnavailable {
		t.Fatalf("expected ErrAutomationLinkUnavailable, got %v", err)
	}
}

// seedBaseAutomation 直接落库一条自动化（owner=ownerUin），返回 public_id。
func seedBaseAutomation(t *testing.T, db *gorm.DB, orgID, ownerUin uint, publicID string) string {
	t.Helper()
	automation := &types.Automation{
		PublicID:          publicID,
		OrgID:             orgID,
		OwnerID:           ownerUin,
		Name:              "基础自动化",
		Instruction:       "指令",
		Enabled:           ptr(true),
		ScheduleMode:      "interval",
		ScheduleSpec:      types.AutomationScheduleSpec{Spec: types.AutomationScheduleSpecItem{Mode: "interval", IntervalSeconds: 300, AnchorAt: "00:00", Timezone: "Asia/Shanghai"}},
		Timezone:          "Asia/Shanghai",
		AssistantID:       99,
		NextRunAt:         ptr(time.Now().UTC().Add(time.Hour)),
		ProjectID:         nil,
		ProjectGeneration: 0,
	}
	if err := db.Create(automation).Error; err != nil {
		t.Fatalf("create automation: %v", err)
	}
	return automation.PublicID
}

// seedActiveExecution 写入一条 queued 执行（活动执行）。automationID 为 db 主键。
func seedActiveExecution(t *testing.T, db *gorm.DB, automationID uint) {
	t.Helper()
	if err := db.Create(&types.AutomationExecution{
		OrgID:         1,
		AutomationID:  automationID,
		OwnerID:       7,
		PublicID:      "autoexec_" + publicIDNum(),
		TriggerType:   types.AutomationTriggerManual,
		OccurrenceKey: "manual_" + publicIDNum(),
		Status:        types.AutomationExecutionQueued,
		ScheduledAt:   time.Now().UTC(),
	}).Error; err != nil {
		t.Fatalf("create active execution: %v", err)
	}
}

func publicIDNum() string {
	return time.Now().Format("150405.000000000")
}

func autosByPublicID(db *gorm.DB, publicID string) (*types.Automation, error) {
	var a types.Automation
	err := db.Where("public_id = ?", publicID).First(&a).Error
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// TestUpdateAutomation_KeepLink 更新省略 project_public_id => 保持原有关联。
func TestUpdateAutomation_KeepLink(t *testing.T) {
	db := seedAutomationLinkDB(t)
	const asst = 99
	proj := seedLinkedProject(t, db, "prj_link", 1, 7, asst)
	id := seedBaseAutomation(t, db, 1, 7, "auto_keep")
	// 预置已关联
	if err := infradb.UpdateAutomationProjectLink(context.Background(), db, automationIDOf(t, db, id), &proj.ID, 0); err != nil {
		t.Fatalf("prelink: %v", err)
	}
	svc := NewAutomationService(db, NewPermissionService(db, NewPermissionCore(db)))

	upd, err := svc.UpdateAutomation(linkTestCaller(1, 7), id, &contract.UpdateAutomationRequest{Name: "改名"})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	stored, _ := autosByPublicID(db, id)
	if stored.ProjectID == nil || *stored.ProjectID != proj.ID {
		t.Fatalf("expected project kept=%d, got %v", proj.ID, stored.ProjectID)
	}
	_ = upd
}

// TestUpdateAutomation_UnlinkToDefault 更新 project_public_id="" => 切回默认新项目。
func TestUpdateAutomation_UnlinkToDefault(t *testing.T) {
	db := seedAutomationLinkDB(t)
	const asst = 99
	proj := seedLinkedProject(t, db, "prj_link", 1, 7, asst)
	id := seedBaseAutomation(t, db, 1, 7, "auto_unlink")
	if err := infradb.UpdateAutomationProjectLink(context.Background(), db, automationIDOf(t, db, id), &proj.ID, 0); err != nil {
		t.Fatalf("prelink: %v", err)
	}
	svc := NewAutomationService(db, NewPermissionService(db, NewPermissionCore(db)))

	clear := ""
	if _, err := svc.UpdateAutomation(linkTestCaller(1, 7), id, &contract.UpdateAutomationRequest{ProjectPublicID: &clear}); err != nil {
		t.Fatalf("update unlink: %v", err)
	}
	stored, _ := autosByPublicID(db, id)
	if stored.ProjectID != nil {
		t.Fatalf("expected ProjectID=nil, got %d", *stored.ProjectID)
	}
	if stored.ProjectGeneration != 0 {
		t.Fatalf("expected ProjectGeneration=0, got %d", stored.ProjectGeneration)
	}
}

// TestUpdateAutomation_SameLinkIgnoresActive 关联相同项目即使存在活动执行也不报 409。
func TestUpdateAutomation_SameLinkIgnoresActive(t *testing.T) {
	db := seedAutomationLinkDB(t)
	const asst = 99
	proj := seedLinkedProject(t, db, "prj_link", 1, 7, asst)
	id := seedBaseAutomation(t, db, 1, 7, "auto_same")
	if err := infradb.UpdateAutomationProjectLink(context.Background(), db, automationIDOf(t, db, id), &proj.ID, 0); err != nil {
		t.Fatalf("prelink: %v", err)
	}
	seedActiveExecution(t, db, automationIDOf(t, db, id))
	svc := NewAutomationService(db, NewPermissionService(db, NewPermissionCore(db)))

	same := "prj_link"
	if _, err := svc.UpdateAutomation(linkTestCaller(1, 7), id, &contract.UpdateAutomationRequest{ProjectPublicID: &same}); err != nil {
		t.Fatalf("same-link update should succeed despite active execution, got %v", err)
	}
}

// TestUpdateAutomation_ChangeLinkConflict 更换到不同项目且存在活动执行 => 409。
func TestUpdateAutomation_ChangeLinkConflict(t *testing.T) {
	db := seedAutomationLinkDB(t)
	const asst = 99
	_ = seedLinkedProject(t, db, "prj_a", 1, 7, asst)
	other := seedLinkedProject(t, db, "prj_b", 1, 7, asst)
	id := seedBaseAutomation(t, db, 1, 7, "auto_conflict")
	if err := infradb.UpdateAutomationProjectLink(context.Background(), db, automationIDOf(t, db, id), &other.ID, 0); err != nil {
		t.Fatalf("prelink: %v", err)
	}
	seedActiveExecution(t, db, automationIDOf(t, db, id))
	svc := NewAutomationService(db, NewPermissionService(db, NewPermissionCore(db)))

	// 从 prj_b 换到 prj_a；名称也不应被半次更新写入。
	target := "prj_a"
	name := "不应保存的名称"
	if _, err := svc.UpdateAutomation(linkTestCaller(1, 7), id, &contract.UpdateAutomationRequest{Name: name, ProjectPublicID: &target}); err != ErrAutomationProjectChangeConflict {
		t.Fatalf("expected conflict, got %v", err)
	}
	stored, err := autosByPublicID(db, id)
	if err != nil {
		t.Fatalf("load automation: %v", err)
	}
	if stored.Name != "基础自动化" {
		t.Fatalf("conflicted update must roll back name, got %q", stored.Name)
	}
}

func TestUpdateAutomation_InvalidLinkRollsBackOtherFields(t *testing.T) {
	db := seedAutomationLinkDB(t)
	id := seedBaseAutomation(t, db, 1, 7, "auto_rollback")
	svc := NewAutomationService(db, NewPermissionService(db, NewPermissionCore(db)))

	name := "不应保存的名称"
	missing := "prj_missing"
	_, err := svc.UpdateAutomation(linkTestCaller(1, 7), id, &contract.UpdateAutomationRequest{
		Name:            name,
		ProjectPublicID: &missing,
	})
	if err != ErrAutomationLinkNotFound {
		t.Fatalf("expected ErrAutomationLinkNotFound, got %v", err)
	}
	stored, err := autosByPublicID(db, id)
	if err != nil {
		t.Fatalf("load automation: %v", err)
	}
	if stored.Name != "基础自动化" {
		t.Fatalf("invalid link must roll back name, got %q", stored.Name)
	}
}

// TestUpdateAutomation_ChangeLinkNoConflict 更换到不同项目且无活动执行 => 成功。
func TestUpdateAutomation_ChangeLinkNoConflict(t *testing.T) {
	db := seedAutomationLinkDB(t)
	const asst = 99
	_ = seedLinkedProject(t, db, "prj_a", 1, 7, asst)
	other := seedLinkedProject(t, db, "prj_b", 1, 7, asst)
	id := seedBaseAutomation(t, db, 1, 7, "auto_change")
	if err := infradb.UpdateAutomationProjectLink(context.Background(), db, automationIDOf(t, db, id), &other.ID, 0); err != nil {
		t.Fatalf("prelink: %v", err)
	}
	svc := NewAutomationService(db, NewPermissionService(db, NewPermissionCore(db)))

	target := "prj_a"
	if _, err := svc.UpdateAutomation(linkTestCaller(1, 7), id, &contract.UpdateAutomationRequest{ProjectPublicID: &target}); err != nil {
		t.Fatalf("change link should succeed, got %v", err)
	}
	stored, _ := autosByPublicID(db, id)
	if stored.ProjectID == nil || *stored.ProjectID != dbProjectByName(t, db, "prj_a") {
		t.Fatalf("expected ProjectID -> prj_a, got %v", stored.ProjectID)
	}
}

// TestProvisioner_RecreatesOnUnusable 关联项目失去队友绑定后 EnsureProject 落 CreateProject 自动换代。
func TestProvisioner_RecreatesOnUnusable(t *testing.T) {
	db := seedAutomationLinkDB(t)
	const asst = 99
	proj := seedLinkedProject(t, db, "prj_link", 1, 7, asst) // 有助手绑定
	automation := &types.Automation{
		PublicID:          "auto_gen",
		OrgID:             1,
		OwnerID:           7,
		Name:              "换代",
		Instruction:       "指令",
		ScheduleMode:      "interval",
		ScheduleSpec:      types.AutomationScheduleSpec{Spec: types.AutomationScheduleSpecItem{Mode: "interval", IntervalSeconds: 300, AnchorAt: "00:00", Timezone: "Asia/Shanghai"}},
		Timezone:          "Asia/Shanghai",
		AssistantID:       asst,
		ProjectID:         &proj.ID,
		ProjectGeneration: 0,
	}
	if err := db.Create(automation).Error; err != nil {
		t.Fatalf("create automation: %v", err)
	}
	svc := NewAutomationService(db, NewPermissionService(db, NewPermissionCore(db)))
	_ = svc // 关联在 Create/Update 已校验

	provisioner := NewAutomationProjectProvisioner(db, nil, nil, "test")

	// 1) 项目仍可用 => 复用
	got1, err := provisioner.EnsureProject(context.Background(), automation)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if got1.ID != proj.ID {
		t.Fatalf("expected reuse project %d, got %d", proj.ID, got1.ID)
	}

	// 2) 删除关联项目的助手绑定 => projectUsable 失败 => 创建新一代
	if err := db.Where("assistant_id = ? AND resource_id IN (SELECT id FROM leros_resource WHERE biz_id = ?)", asst, proj.ID).
		Delete(&types.ResourceBinding{}).Error; err != nil {
		t.Fatalf("unbind assistant: %v", err)
	}
	got2, err := provisioner.EnsureProject(context.Background(), automation)
	if err != nil {
		t.Fatalf("ensure after unbind: %v", err)
	}
	if got2.ID == proj.ID {
		t.Fatalf("expected a new generation project, got same project %d", got2.ID)
	}
	if got2.AutomationGeneration != 1 {
		t.Fatalf("expected generation 1, got %d", got2.AutomationGeneration)
	}
}

func TestUpdateAutomation_UnlinkAfterHistoricalDedicatedProjectCreatesNextGeneration(t *testing.T) {
	db := seedAutomationLinkDB(t)
	const asst = 99
	autoID := seedBaseAutomation(t, db, 1, 7, "auto_generation")
	automation, err := autosByPublicID(db, autoID)
	if err != nil {
		t.Fatalf("load automation: %v", err)
	}
	oldDedicated := seedLinkedProject(t, db, "prj_old_dedicated", 1, 7, asst)
	if err := db.Model(oldDedicated).Updates(map[string]interface{}{
		"automation_id":         automation.ID,
		"automation_generation": 1,
	}).Error; err != nil {
		t.Fatalf("mark historical dedicated project: %v", err)
	}
	linked := seedLinkedProject(t, db, "prj_shared", 1, 7, asst)
	if err := infradb.UpdateAutomationProjectLink(context.Background(), db, automation.ID, &oldDedicated.ID, 1); err != nil {
		t.Fatalf("link historical dedicated project: %v", err)
	}

	svc := NewAutomationService(db, NewPermissionService(db, NewPermissionCore(db)))
	link := linked.PublicID
	if _, err := svc.UpdateAutomation(linkTestCaller(1, 7), autoID, &contract.UpdateAutomationRequest{ProjectPublicID: &link}); err != nil {
		t.Fatalf("link shared project: %v", err)
	}
	clear := ""
	if _, err := svc.UpdateAutomation(linkTestCaller(1, 7), autoID, &contract.UpdateAutomationRequest{ProjectPublicID: &clear}); err != nil {
		t.Fatalf("unlink to default project: %v", err)
	}

	automation, err = autosByPublicID(db, autoID)
	if err != nil {
		t.Fatalf("reload automation: %v", err)
	}
	if automation.ProjectID != nil || automation.ProjectGeneration != 1 {
		t.Fatalf("unlink should retain historical generation cursor=1, got project=%v generation=%d", automation.ProjectID, automation.ProjectGeneration)
	}
	provisioner := NewAutomationProjectProvisioner(db, nil, nil, "test")
	created, err := provisioner.EnsureProject(context.Background(), automation)
	if err != nil {
		t.Fatalf("ensure next generation: %v", err)
	}
	if created.ID == oldDedicated.ID || created.AutomationGeneration != 2 {
		t.Fatalf("expected fresh generation 2, got id=%d generation=%d", created.ID, created.AutomationGeneration)
	}
}

func automationIDOf(t *testing.T, db *gorm.DB, publicID string) uint {
	t.Helper()
	a, err := autosByPublicID(db, publicID)
	if err != nil {
		t.Fatalf("load automation id: %v", err)
	}
	return a.ID
}

func dbProjectByName(t *testing.T, db *gorm.DB, publicID string) uint {
	t.Helper()
	var p types.Project
	if err := db.Where("public_id = ?", publicID).First(&p).Error; err != nil {
		t.Fatalf("load project: %v", err)
	}
	return p.ID
}

func ptr[T any](v T) *T { return &v }
