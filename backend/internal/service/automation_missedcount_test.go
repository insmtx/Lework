package service

import (
	"github.com/insmtx/Leros/backend/types"
	"testing"
	"time"
)

// TestMissedCount_OnTimeIsZero 正常准点执行时 missed_count 应为 0，
// 而不是把"距锚点累计周期"当遗漏（回归：曾每 5 分钟准点却 missed 累加到 100+）。
// 场景：interval 5min，anchor 00:00，Asia/Shanghai；oldNextRunAt=16:55，now=17:00（恰好一个周期后）。
func TestMissedCount_OnTimeIsZero(t *testing.T) {
	loc := mustLoc(t)
	spec := &types.AutomationScheduleSpec{Spec: types.AutomationScheduleSpecItem{
		Mode: "interval", IntervalSeconds: 300, AnchorAt: "00:00", Timezone: "Asia/Shanghai",
	}}
	oldNext := time.Date(2026, 8, 6, 16, 55, 0, 0, loc).UTC() // 16:55 CST
	now := time.Date(2026, 8, 6, 17, 0, 0, 0, loc).UTC()      // 17:00 CST
	w := computeIntervalWindow(spec, oldNext, now, loc)
	if w.MissedCount != 0 {
		t.Fatalf("准点执行 missed=%d, want 0", w.MissedCount)
	}
	// Next 应为 17:00 CST（下一个整 5 分钟刻度）
	wantNext := time.Date(2026, 8, 6, 17, 0, 0, 0, loc).UTC()
	if !w.Next.Equal(wantNext) {
		t.Fatalf("Next=%s, want 17:00 CST", w.Next.In(loc).Format("15:04:05"))
	}
}

// TestMissedCount_DowntimeFolds 停机错过多个周期时，missed = 被折叠的更早周期数 = N-1。
// oldNextRunAt=16:40，now=17:00（跨 4 个 5 分钟周期）→ 折叠 16:45/16:50/16:55 = 3。
func TestMissedCount_DowntimeFolds(t *testing.T) {
	loc := mustLoc(t)
	spec := &types.AutomationScheduleSpec{Spec: types.AutomationScheduleSpecItem{
		Mode: "interval", IntervalSeconds: 300, AnchorAt: "00:00", Timezone: "Asia/Shanghai",
	}}
	oldNext := time.Date(2026, 8, 6, 16, 40, 0, 0, loc).UTC() // 16:40 CST
	now := time.Date(2026, 8, 6, 17, 0, 0, 0, loc).UTC()      // 17:00 CST
	w := computeIntervalWindow(spec, oldNext, now, loc)
	if w.MissedCount != 3 {
		t.Fatalf("停机 4 周期 missed=%d, want 3", w.MissedCount)
	}
}
