package service

import (
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/types"
)

// TestCalendarWindow_NoMissedKeepsPlannedTime 验证日历任务无更早遗漏时，
// LatestDue = oldNextRunAt（计划时间），而非回退成扫描时刻（提前执行根因）。
// 场景：每天 17:12 Asia/Shanghai（cron `12 17 * * *`），oldNextRunAt 被错误推进到
// 17:10 CST（09:10 UTC，过去），now=17:11 CST（09:11 UTC）触发扫描。
// 期望 LatestDue=17:10（oldNextRunAt），Next=17:12 UTC+8（09:12 UTC），missed=0。
func TestCalendarWindow_NoMissedKeepsPlannedTime(t *testing.T) {
	loc := mustLoc(t)
	spec := &types.AutomationScheduleSpec{
		Spec: types.AutomationScheduleSpecItem{
			Mode:       "calendar",
			Expression: "12 17 * * *", // 每天 17:12（分 时 日 月 周 = 12 17 * * *）
			Timezone:   "Asia/Shanghai",
		},
	}
	// oldNextRunAt = 17:10 CST 的 UTC 绝对瞬间（历史被推进到过去）
	oldNext := time.Date(2026, 8, 6, 17, 10, 0, 0, loc).UTC()
	now := time.Date(2026, 8, 6, 17, 11, 0, 0, loc).UTC() // 17:11 CST

	w, err := ComputeOccurrenceWindow(spec, oldNext, now)
	if err != nil {
		t.Fatalf("err=%v", err)
	}

	// LatestDue 应为 oldNextRunAt（17:10 CST），不能是零值/扫描时刻
	wantLatest := time.Date(2026, 8, 6, 17, 10, 0, 0, loc).UTC()
	if !w.LatestDue.Equal(wantLatest) {
		t.Fatalf("LatestDue=%s CST, want 17:10 CST（应为 oldNextRunAt，而非扫描时刻）", w.LatestDue.In(loc).Format("15:04:05"))
	}
	// Next 应为下一个计划点 17:12 CST = 09:12 UTC
	wantNext := time.Date(2026, 8, 6, 17, 12, 0, 0, loc).UTC()
	if !w.Next.Equal(wantNext) {
		t.Fatalf("Next=%s CST, want 17:12 CST", w.Next.In(loc).Format("2006-01-02 15:04:05"))
	}
	if w.MissedCount != 0 {
		t.Fatalf("MissedCount=%d, want 0", w.MissedCount)
	}
}
