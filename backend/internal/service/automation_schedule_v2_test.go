package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/types"
)

func TestCompileScheduleSpecIntervalUsesCreationOrigin(t *testing.T) {
	base := time.Date(2026, 8, 19, 2, 40, 0, 0, time.UTC)
	form := &types.AutomationScheduleFormConfig{
		Mode:     "interval",
		Interval: &types.AutomationIntervalConfig{IntervalMinutes: 30},
		Timezone: "Asia/Shanghai",
	}
	spec, err := compileScheduleSpecAt(form, "", "", base)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if spec.Spec.OriginAt != base.Format(time.RFC3339Nano) {
		t.Fatalf("origin_at=%q, want %q", spec.Spec.OriginAt, base.Format(time.RFC3339Nano))
	}
	next := computeNextRunAt(spec, true, base)
	if next == nil {
		t.Fatal("next_run_at is nil")
	}
	want := base.Add(30 * time.Minute)
	if !next.Equal(want) || next.Location() != time.UTC {
		t.Fatalf("next_run_at=%s (%s), want %s UTC", next, next.Location(), want)
	}
}

func TestComputeOccurrenceWindowV2UsesStoredAbsoluteOrigin(t *testing.T) {
	spec := &types.AutomationScheduleSpec{Spec: types.AutomationScheduleSpecItem{
		Version:         AutomationScheduleVersion,
		Mode:            "interval",
		OriginAt:        "2026-08-19T02:40:00Z",
		IntervalSeconds: 1800,
		Timezone:        "Asia/Shanghai",
	}}
	oldNext := time.Date(2026, 8, 19, 2, 40, 0, 0, time.UTC)
	now := time.Date(2026, 8, 19, 3, 11, 0, 0, time.UTC)
	w, err := ComputeOccurrenceWindow(spec, oldNext, now)
	if err != nil {
		t.Fatalf("compute window: %v", err)
	}
	if want := time.Date(2026, 8, 19, 3, 10, 0, 0, time.UTC); !w.LatestDue.Equal(want) {
		t.Fatalf("latest_due=%s, want %s", w.LatestDue, want)
	}
	if want := time.Date(2026, 8, 19, 3, 40, 0, 0, time.UTC); !w.Next.Equal(want) {
		t.Fatalf("next=%s, want %s", w.Next, want)
	}
	if w.MissedCount != 0 {
		t.Fatalf("missed_count=%d, want 0", w.MissedCount)
	}
}

func TestApplyAutomationUpdatePreservesAndResetsIntervalOriginAtBoundaries(t *testing.T) {
	origin := "2026-08-19T00:00:00Z"
	next := time.Date(2026, 8, 19, 0, 5, 0, 0, time.UTC)
	automation := &types.Automation{
		Enabled:      automationBoolPtr(true),
		ScheduleMode: "interval",
		Timezone:     "Asia/Shanghai",
		NextRunAt:    &next,
		ScheduleSpec: types.AutomationScheduleSpec{Spec: types.AutomationScheduleSpecItem{
			Version: 2, Mode: "interval", OriginAt: origin, IntervalSeconds: 300, Timezone: "Asia/Shanghai",
		}},
	}
	if err := applyAutomationUpdate(automation, &contract.UpdateAutomationRequest{Name: "仅改名称"}); err != nil {
		t.Fatalf("name update: %v", err)
	}
	if automation.ScheduleSpec.Spec.OriginAt != origin || automation.NextRunAt == nil || !automation.NextRunAt.Equal(next) {
		t.Fatalf("name update changed interval timing: origin=%q next=%v", automation.ScheduleSpec.Spec.OriginAt, automation.NextRunAt)
	}
	if err := applyAutomationUpdate(automation, &contract.UpdateAutomationRequest{
		ScheduleMode: automationStringPtr("interval"),
		Schedule: &contract.AutomationScheduleInput{
			Mode:     "interval",
			Timezone: "Asia/Shanghai",
			Interval: &contract.AutomationIntervalInput{IntervalMinutes: 10},
		},
	}); err != nil {
		t.Fatalf("interval update: %v", err)
	}
	if automation.ScheduleSpec.Spec.IntervalSeconds != 600 || automation.ScheduleSpec.Spec.OriginAt == origin {
		t.Fatalf("interval update did not reset origin: %+v", automation.ScheduleSpec.Spec)
	}
	originAfterInterval := automation.ScheduleSpec.Spec.OriginAt
	if err := applyAutomationUpdate(automation, &contract.UpdateAutomationRequest{Timezone: automationStringPtr("America/New_York")}); err != nil {
		t.Fatalf("timezone update: %v", err)
	}
	if automation.ScheduleSpec.Spec.Timezone != "America/New_York" || automation.ScheduleSpec.Spec.OriginAt == originAfterInterval {
		t.Fatalf("timezone update did not reset origin: %+v", automation.ScheduleSpec.Spec)
	}
}

func TestApplyAutomationUpdateKeepsDueIntervalCursorForUnchangedSchedule(t *testing.T) {
	origin := "2026-08-19T00:00:00Z"
	// 模拟 Planner 尚未领取的到期游标。编辑弹窗会完整回传相同 schedule，
	// 但这不能让保存操作跳过本次 occurrence。
	dueNext := time.Date(2026, 8, 19, 0, 5, 0, 0, time.UTC)
	automation := &types.Automation{
		Enabled:      automationBoolPtr(true),
		ScheduleMode: "interval",
		Timezone:     "Asia/Shanghai",
		NextRunAt:    &dueNext,
		ScheduleSpec: types.AutomationScheduleSpec{Spec: types.AutomationScheduleSpecItem{
			Version: 2, Mode: "interval", OriginAt: origin, IntervalSeconds: 300, Timezone: "Asia/Shanghai",
		}},
	}
	enabled := true
	if err := applyAutomationUpdate(automation, &contract.UpdateAutomationRequest{
		Name:         "仅改名称",
		Enabled:      &enabled,
		ScheduleMode: automationStringPtr("interval"),
		Timezone:     automationStringPtr("Asia/Shanghai"),
		Schedule: &contract.AutomationScheduleInput{
			Mode:     "interval",
			Timezone: "Asia/Shanghai",
			Interval: &contract.AutomationIntervalInput{IntervalMinutes: 5},
		},
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if automation.ScheduleSpec.Spec.OriginAt != origin {
		t.Fatalf("origin_at=%q, want %q", automation.ScheduleSpec.Spec.OriginAt, origin)
	}
	if automation.NextRunAt == nil || !automation.NextRunAt.Equal(dueNext) {
		t.Fatalf("next_run_at=%v, want unchanged due cursor %s", automation.NextRunAt, dueNext)
	}
}

func automationBoolPtr(value bool) *bool { return &value }

func automationStringPtr(value string) *string { return &value }

func TestAnchorGridPointUsesAnchorLocationForLegacyRules(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	tUTC := time.Date(2026, 8, 19, 2, 40, 0, 0, time.UTC)
	anchor := time.Date(0, 1, 1, 10, 50, 0, 0, loc)
	got := anchorGridPoint(tUTC, anchor)
	want := time.Date(2026, 8, 19, 10, 50, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("grid point=%s, want %s", got, want)
	}
}

func TestAutomationResponseNormalizesNextRunAtToUTC(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	next := time.Date(2026, 8, 19, 10, 50, 0, 0, loc)
	created := time.Date(2026, 8, 19, 10, 0, 0, 0, loc)
	updated := time.Date(2026, 8, 19, 10, 20, 0, 0, loc)
	out := toContractAutomation(&types.Automation{
		Model:     gorm.Model{CreatedAt: created, UpdatedAt: updated},
		NextRunAt: &next,
	})
	payload, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(payload); !strings.Contains(got, "2026-08-19T02:50:00Z") {
		t.Fatalf("response=%s, missing UTC next_run_at", got)
	}
	if got := string(payload); !strings.Contains(got, "2026-08-19T02:00:00Z") ||
		!strings.Contains(got, "2026-08-19T02:20:00Z") {
		t.Fatalf("response=%s, missing UTC lifecycle timestamps", got)
	}
}
