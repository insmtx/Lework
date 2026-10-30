package service

import (
	"github.com/insmtx/Leros/backend/types"
	"testing"
	"time"
)

// TestComputeOccurrenceWindow_IntervalUTCNoDrift 回归：固定间隔遗漏计算不得因 CST/UTC 混用产生 ±8h 偏移。
// 场景：每5分钟、anchor 00:00、Asia/Shanghai；oldNext 以 UTC 绝对瞬间存储，Planner 在 15:34 CST 扫描。
// 期望 Next = 15:35 CST（下一个整 5 分钟刻度），而不是跳到 8 小时后。
func TestComputeOccurrenceWindow_IntervalUTCNoDrift(t *testing.T) {
	loc := mustLoc(t)
	oldNextUTC := time.Date(2026, 8, 6, 15, 20, 0, 0, loc).UTC() // 15:20 CST
	now := time.Date(2026, 8, 6, 15, 34, 0, 0, loc).UTC()

	spec := &types.AutomationScheduleSpec{Spec: types.AutomationScheduleSpecItem{
		Version: 2, Mode: "interval", IntervalSeconds: 300, OriginAt: "2026-08-06T07:15:00Z", Timezone: "Asia/Shanghai",
	}}
	w, err := ComputeOccurrenceWindow(spec, oldNextUTC, now)
	if err != nil {
		t.Fatalf("err=%v", err)
	}

	want := time.Date(2026, 8, 6, 15, 35, 0, 0, loc).UTC()
	if !w.Next.Equal(want) {
		t.Fatalf("Next(CST)=%s, want 15:35 CST", w.Next.In(loc).Format("15:04:05"))
	}
	wantDue := time.Date(2026, 8, 6, 15, 30, 0, 0, loc).UTC()
	if !w.LatestDue.Equal(wantDue) {
		t.Fatalf("LatestDue(CST)=%s, want 15:30 CST", w.LatestDue.In(loc).Format("15:04:05"))
	}
	// oldNext=15:20，now=15:34：15:20、15:25、15:30 三个周期到期，
	// 折叠较早的两个，只保留 LatestDue=15:30，missed=1。
	if w.MissedCount != 1 {
		t.Fatalf("MissedCount=%d, want 1（被折叠的更早周期）", w.MissedCount)
	}
}

// TestComputeOccurrenceWindow_IntervalFromOriginOneDueNoMissed 验证 v2 interval
// 以 oldNextRunAt 为游标：15:25 之后只有 15:30 一个到期点，missed 必须为 0。
func TestComputeOccurrenceWindow_IntervalFromOriginOneDueNoMissed(t *testing.T) {
	loc := mustLoc(t)
	oldNextUTC := time.Date(2026, 8, 6, 15, 25, 0, 0, loc).UTC() // 15:25
	now := time.Date(2026, 8, 6, 15, 34, 0, 0, loc).UTC()        // 15:34

	spec := &types.AutomationScheduleSpec{Spec: types.AutomationScheduleSpecItem{
		Version: 2, Mode: "interval", IntervalSeconds: 300, OriginAt: "2026-08-06T07:20:00Z", Timezone: "Asia/Shanghai",
	}}
	w, err := ComputeOccurrenceWindow(spec, oldNextUTC, now)
	if err != nil {
		t.Fatalf("err=%v", err)
	}

	want := time.Date(2026, 8, 6, 15, 35, 0, 0, loc).UTC()
	if !w.Next.Equal(want) {
		t.Fatalf("Next(CST)=%s, want 15:35 CST", w.Next.In(loc).Format("15:04:05"))
	}
	wantDue := time.Date(2026, 8, 6, 15, 30, 0, 0, loc).UTC()
	if !w.LatestDue.Equal(wantDue) {
		t.Fatalf("LatestDue(CST)=%s, want 15:30 CST", w.LatestDue.In(loc).Format("15:04:05"))
	}
	if w.MissedCount != 0 {
		t.Fatalf("MissedCount=%d, want 0（只有一个到期点）", w.MissedCount)
	}
}
