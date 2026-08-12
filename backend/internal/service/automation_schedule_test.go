package service

import (
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/types"
)

func mustLoc(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	return loc
}

func TestCompileScheduleSpec_CalendarDaily(t *testing.T) {
	form := &types.AutomationScheduleFormConfig{
		Mode: "calendar",
		Calendar: &types.AutomationCalendarConfig{
			Preset: "daily",
			Hour:   9,
			Minute: 30,
		},
		Timezone: "Asia/Shanghai",
	}
	spec, err := compileScheduleSpec(form, "", "")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if spec.Spec.Expression != "30 9 * * *" {
		t.Fatalf("unexpected expression: %s", spec.Spec.Expression)
	}
	if spec.Spec.Timezone != "Asia/Shanghai" {
		t.Fatalf("unexpected timezone: %s", spec.Spec.Timezone)
	}
}

func TestCompileScheduleSpec_CalendarWeekly(t *testing.T) {
	form := &types.AutomationScheduleFormConfig{
		Mode: "calendar",
		Calendar: &types.AutomationCalendarConfig{
			Preset:     "weekly",
			Hour:       9,
			Minute:     0,
			DaysOfWeek: []int{1, 3},
		},
		Timezone: "Asia/Shanghai",
	}
	spec, err := compileScheduleSpec(form, "", "")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if spec.Spec.Expression != "0 9 * * 1,3" {
		t.Fatalf("unexpected expression: %s", spec.Spec.Expression)
	}
}

func TestCompileScheduleSpec_CalendarMonthly(t *testing.T) {
	form := &types.AutomationScheduleFormConfig{
		Mode: "calendar",
		Calendar: &types.AutomationCalendarConfig{
			Preset:      "monthly",
			Hour:        9,
			Minute:      0,
			DaysOfMonth: []int{31},
		},
		Timezone: "Asia/Shanghai",
	}
	spec, err := compileScheduleSpec(form, "", "")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if spec.Spec.Expression != "0 9 31 * *" {
		t.Fatalf("unexpected expression: %s", spec.Spec.Expression)
	}
	if spec.Spec.Policy == nil || spec.Spec.Policy.MonthDayOverflow != "last_day" {
		t.Fatalf("expected last_day policy, got %+v", spec.Spec.Policy)
	}
}

func TestCompileScheduleSpec_Interval(t *testing.T) {
	form := &types.AutomationScheduleFormConfig{
		Mode: "interval",
		Interval: &types.AutomationIntervalConfig{
			IntervalMinutes: 30,
		},
		Timezone: "Asia/Shanghai",
	}
	spec, err := compileScheduleSpec(form, "", "")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if spec.Spec.IntervalSeconds != 1800 {
		t.Fatalf("unexpected interval seconds: %d", spec.Spec.IntervalSeconds)
	}
	if spec.Spec.AnchorAt == "" {
		t.Fatalf("anchor_at should not be empty")
	}
}

func TestCompileScheduleSpec_InvalidMode(t *testing.T) {
	form := &types.AutomationScheduleFormConfig{
		Mode:     "unknown",
		Timezone: "Asia/Shanghai",
	}
	if _, err := compileScheduleSpec(form, "", ""); err == nil {
		t.Fatalf("expected error for unknown mode")
	}
}

func TestCompileScheduleSpec_InvalidTimezone(t *testing.T) {
	form := &types.AutomationScheduleFormConfig{
		Mode:     "calendar",
		Calendar: &types.AutomationCalendarConfig{Preset: "daily", Hour: 9, Minute: 0},
		Timezone: "Not/AZone",
	}
	if _, err := compileScheduleSpec(form, "", ""); err == nil {
		t.Fatalf("expected error for invalid timezone")
	}
}

func TestCompileScheduleSpec_IntervalTooShort(t *testing.T) {
	form := &types.AutomationScheduleFormConfig{
		Mode: "interval",
		Interval: &types.AutomationIntervalConfig{
			IntervalMinutes: 1,
		},
		Timezone: "Asia/Shanghai",
	}
	if _, err := compileScheduleSpec(form, "", ""); err == nil {
		t.Fatalf("expected error for interval below minimum")
	}
}

func TestNextDaily(t *testing.T) {
	loc := mustLoc(t)
	now := time.Date(2026, 8, 5, 8, 0, 0, 0, loc) // 2026-08-05 08:00 +08
	next, err := nextCalendar(now, "30 9 * * *", nil, loc)
	if err != nil {
		t.Fatalf("nextCalendar: %v", err)
	}
	expected := time.Date(2026, 8, 5, 9, 30, 0, 0, loc)
	if !next.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, next)
	}
}

func TestNextDailyAlreadyPassed(t *testing.T) {
	loc := mustLoc(t)
	now := time.Date(2026, 8, 5, 10, 0, 0, 0, loc) // 已过今天的 09:30
	next, err := nextCalendar(now, "30 9 * * *", nil, loc)
	if err != nil {
		t.Fatalf("nextCalendar: %v", err)
	}
	expected := time.Date(2026, 8, 6, 9, 30, 0, 0, loc)
	if !next.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, next)
	}
}

func TestNextWeekly(t *testing.T) {
	loc := mustLoc(t)
	// 2026-08-05 是周三(3)，配置周一(1)09:00
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, loc)
	next, err := nextCalendar(now, "0 9 * * 1", nil, loc)
	if err != nil {
		t.Fatalf("nextCalendar: %v", err)
	}
	// 下一个周一 = 2026-08-10
	expected := time.Date(2026, 8, 10, 9, 0, 0, 0, loc)
	if !next.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, next)
	}
}

func TestNextMonthlyLastDay(t *testing.T) {
	loc := mustLoc(t)
	// 配置每月 31 日 09:00，目标月份 8 月有 31 天 → 直接 8-31
	now := time.Date(2026, 8, 1, 0, 0, 0, 0, loc)
	policy := &types.AutomationSchedulePolicy{MonthDayOverflow: "last_day"}
	next, err := nextCalendar(now, "0 9 31 * *", policy, loc)
	if err != nil {
		t.Fatalf("nextCalendar: %v", err)
	}
	expected := time.Date(2026, 8, 31, 9, 0, 0, 0, loc)
	if !next.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, next)
	}
}

func TestNextMonthlyOverflow(t *testing.T) {
	loc := mustLoc(t)
	// 9 月只有 30 天，配置 31 日 → 月末回退到 9-30
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, loc)
	policy := &types.AutomationSchedulePolicy{MonthDayOverflow: "last_day"}
	next, err := nextCalendar(now, "0 9 31 * *", policy, loc)
	if err != nil {
		t.Fatalf("nextCalendar: %v", err)
	}
	expected := time.Date(2026, 9, 30, 9, 0, 0, 0, loc)
	if !next.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, next)
	}
}

func TestNextHourly(t *testing.T) {
	loc := mustLoc(t)
	now := time.Date(2026, 8, 5, 10, 20, 0, 0, loc) // 10:20
	next, err := nextCalendar(now, "0 * * * *", nil, loc)
	if err != nil {
		t.Fatalf("nextCalendar: %v", err)
	}
	expected := time.Date(2026, 8, 5, 11, 0, 0, 0, loc)
	if !next.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, next)
	}
}

func TestNextInterval(t *testing.T) {
	loc := mustLoc(t)
	anchor := "2026-08-05T00:00:00+08:00"
	// 锚点网格语义：绝对锚点 = now 当天 00:00，Next = 锚点 + ceil(elapsed/interval)*interval
	// now=00:25，interval=30min → Next=00:30 CST
	now := time.Date(2026, 8, 5, 0, 25, 0, 0, loc)
	next, err := nextInterval(now, anchor, 1800, loc)
	if err != nil {
		t.Fatalf("nextInterval: %v", err)
	}
	expected := time.Date(2026, 8, 5, 0, 30, 0, 0, loc)
	if !next.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, next)
	}
}

func TestNextInterval_HHMMAnchorFrom00(t *testing.T) {
	loc := mustLoc(t)
	// 锚点网格：anchor=00:00 是真起点，now=00:25+30min → Next=00:30 CST（不是保存时刻+30min）
	now := time.Date(2026, 8, 5, 0, 25, 0, 0, loc)
	next, err := nextInterval(now, "00:00", 1800, loc)
	if err != nil {
		t.Fatalf("nextInterval: %v", err)
	}
	expected := time.Date(2026, 8, 5, 0, 30, 0, 0, loc)
	if !next.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, next)
	}
}

func TestNextInterval_HHMMAnchorWrapsToNextDay(t *testing.T) {
	loc := mustLoc(t)
	// 锚点网格：anchor=23:00，now=23:30，interval=60min → Next=次日 00:00 CST（23:00+60min 跨天）
	anchor := "23:00"
	interval := int64(60 * 60)
	now := time.Date(2026, 8, 5, 23, 30, 0, 0, loc)
	next, err := nextInterval(now, anchor, interval, loc)
	if err != nil {
		t.Fatalf("nextInterval: %v", err)
	}
	expected := time.Date(2026, 8, 6, 0, 0, 0, 0, loc)
	if !next.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, next)
	}
}

func TestParseAnchor_HHMMAndTimestamp(t *testing.T) {
	loc := mustLoc(t)
	if _, err := parseAnchor("00:00", loc); err != nil {
		t.Fatalf("parse HH:MM: %v", err)
	}
	if _, err := parseAnchor("09:30", loc); err != nil {
		t.Fatalf("parse HH:MM: %v", err)
	}
	if _, err := parseAnchor("2026-08-05T09:00:00+08:00", loc); err != nil {
		t.Fatalf("parse RFC3339: %v", err)
	}
	if _, err := parseAnchor("bad", loc); err == nil {
		t.Fatalf("expected error for invalid anchor")
	}
}

func TestBuildAutomationSummary(t *testing.T) {
	spec := &types.AutomationScheduleSpec{
		Spec: types.AutomationScheduleSpecItem{
			Mode:       "calendar",
			Expression: "30 9 * * 1,3",
			Timezone:   "Asia/Shanghai",
		},
	}
	summary := buildAutomationSummary(spec)
	if summary != "每周周一、周三 09:30" {
		t.Fatalf("unexpected summary: %s", summary)
	}
}

func TestBuildAutomationSummary_WeekdaySundayLast(t *testing.T) {
	// 周日(0)与周一(1)、周三(3)同时选中时，摘要按界面固定顺序“周一、周三、周日”展示
	spec := &types.AutomationScheduleSpec{
		Spec: types.AutomationScheduleSpecItem{
			Mode:       "calendar",
			Expression: "30 9 * * 1,3,0",
			Timezone:   "Asia/Shanghai",
		},
	}
	summary := buildAutomationSummary(spec)
	if summary != "每周周一、周三、周日 09:30" {
		t.Fatalf("unexpected summary: %s", summary)
	}
}

func TestBuildAutomationSummary_Hourly(t *testing.T) {
	spec := &types.AutomationScheduleSpec{
		Spec: types.AutomationScheduleSpecItem{
			Mode:       "calendar",
			Expression: "0 * * * *",
			Timezone:   "Asia/Shanghai",
		},
	}
	summary := buildAutomationSummary(spec)
	if summary != "每小时的 00 分执行" {
		t.Fatalf("unexpected hourly summary: %s", summary)
	}
}

func TestCompileScheduleSpec_InvalidAnchor(t *testing.T) {
	form := &types.AutomationScheduleFormConfig{
		Mode: "interval",
		Interval: &types.AutomationIntervalConfig{
			IntervalMinutes: 30,
			AnchorAt:        "not-a-time",
		},
		Timezone: "Asia/Shanghai",
	}
	if _, err := compileScheduleSpec(form, "", ""); err == nil {
		t.Fatalf("expected error for invalid anchor")
	}
}

func TestCompileScheduleSpec_IntervalBackfillsAnchorIntoFormConfig(t *testing.T) {
	form := &types.AutomationScheduleFormConfig{
		Mode: "interval",
		Interval: &types.AutomationIntervalConfig{
			IntervalMinutes: 30,
		},
		Timezone: "Asia/Shanghai",
	}
	spec, err := compileScheduleSpec(form, "", "")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// 缺省锚点应回填到 form_config，保证编辑回显
	if spec.FormConfig == nil || spec.FormConfig.Interval == nil || spec.FormConfig.Interval.AnchorAt == "" {
		t.Fatalf("expected anchor backfilled into form_config, got %+v", spec.FormConfig)
	}
	if spec.Spec.AnchorAt == "" {
		t.Fatalf("expected anchor in spec")
	}
}

func TestResolveScheduleMode_Mismatch(t *testing.T) {
	form := &types.AutomationScheduleFormConfig{Mode: "interval"}
	if _, err := resolveScheduleMode("calendar", form); err == nil {
		t.Fatalf("expected error when top-level mode mismatches schedule.mode")
	}
}

func TestResolveScheduleMode_Consistent(t *testing.T) {
	form := &types.AutomationScheduleFormConfig{Mode: "interval"}
	mode, err := resolveScheduleMode("interval", form)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if mode != "interval" {
		t.Fatalf("unexpected mode: %s", mode)
	}
}
