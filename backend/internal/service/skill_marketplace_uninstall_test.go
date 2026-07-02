package service

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/pkg/messaging"
)

func expectProjectsReferencingSkill(mock sqlmock.Sqlmock, skillName string) {
	columns := []string{
		"id", "created_at", "updated_at", "deleted_at", "public_id",
		"org_id", "owner_id", "name", "description", "objective", "status",
		"gitea_repo_full_name", "gitea_repo_id", "gitea_default_branch", "metadata",
	}
	now := time.Now()
	metadata := []byte(`{"extra":{"skills":[{"code":"demo-skill","name":"Demo Skill"}]}}`)
	mock.ExpectQuery(`SELECT .* FROM "leros_project" WHERE \(org_id = \$1 AND deleted_at IS NULL\) AND \(EXISTS`).
		WithArgs(uint(100), skillName, skillName).
		WillReturnRows(sqlmock.NewRows(columns).AddRow(
			1, now, now, nil, "prj_demo",
			100, 1, "Demo Project", "", "", "active",
			"", 0, "main", metadata,
		))
}

func TestUninstallSkillCleansProjectReferencesAfterWorkerSuccess(t *testing.T) {
	database, mock, ctx, cleanup := setupSkillMarketplaceInstallServiceDB(t)
	defer cleanup()
	expectDefaultWorkerDeployment(mock)
	expectProjectsReferencingSkill(mock, "demo-skill")
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "leros_project" SET`)).
		WillReturnResult(sqlmock.NewResult(1, 1))

	publisher := &skillInstallPublisher{
		response: messaging.WorkerCommandResult{
			Success: true,
			Action:  "uninstall",
			Message: "skill \"demo-skill\" uninstalled",
		},
	}
	service := NewSkillMarketplaceService(database, publisher, nil, "")

	resp, err := service.UninstallSkill(ctx, &contract.UninstallSkillRequest{Name: "demo-skill"})
	if err != nil {
		t.Fatalf("uninstall skill: %v", err)
	}
	if resp.Status != "accepted" {
		t.Fatalf("status = %q, want accepted", resp.Status)
	}
	if !strings.Contains(resp.Message, "removed from 1 project(s)") {
		t.Fatalf("message = %q, want project cleanup note", resp.Message)
	}
	if len(publisher.requests) != 1 {
		t.Fatalf("request count = %d, want 1", len(publisher.requests))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestUninstallSkillWorkerFailureDoesNotCleanProjectReferences(t *testing.T) {
	database, mock, ctx, cleanup := setupSkillMarketplaceInstallServiceDB(t)
	defer cleanup()
	expectDefaultWorkerDeployment(mock)
	publisher := &skillInstallPublisher{
		response: messaging.WorkerCommandResult{
			Success: false,
			Action:  "uninstall",
			Error:   "cannot uninstall built-in skill",
		},
	}
	service := NewSkillMarketplaceService(database, publisher, nil, "")

	_, err := service.UninstallSkill(ctx, &contract.UninstallSkillRequest{Name: "demo-skill"})
	if err == nil {
		t.Fatal("expected uninstall error")
	}
	if !strings.Contains(err.Error(), "cannot uninstall built-in skill") {
		t.Fatalf("error = %q, want built-in uninstall failure", err.Error())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestUninstallSkillUsesRequestReply(t *testing.T) {
	database, mock, ctx, cleanup := setupSkillMarketplaceInstallServiceDB(t)
	defer cleanup()
	expectDefaultWorkerDeployment(mock)
	mock.ExpectQuery(`SELECT .* FROM "leros_project" WHERE \(org_id = \$1 AND deleted_at IS NULL\) AND \(EXISTS`).
		WithArgs(uint(100), "demo-skill", "demo-skill").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	publisher := &skillInstallPublisher{
		response: messaging.WorkerCommandResult{
			Success: true,
			Action:  "uninstall",
			Message: "skill \"demo-skill\" uninstalled",
		},
	}
	service := NewSkillMarketplaceService(database, publisher, nil, "")

	_, err := service.UninstallSkill(ctx, &contract.UninstallSkillRequest{Name: "demo-skill"})
	if err != nil {
		t.Fatalf("uninstall skill: %v", err)
	}
	if len(publisher.requests) != 1 {
		t.Fatalf("request count = %d, want request/reply", len(publisher.requests))
	}
	wcmd, ok := publisher.requests[0].(messaging.WorkerCommand)
	if !ok {
		raw, _ := json.Marshal(publisher.requests[0])
		t.Fatalf("request type = %T, payload = %s", publisher.requests[0], string(raw))
	}
	payload, err := messaging.DecodeCommandPayload[messaging.SkillCommandPayload](&wcmd.Body)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.Action != "uninstall" || payload.Name != "demo-skill" {
		t.Fatalf("payload = %#v, want uninstall demo-skill", payload)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
