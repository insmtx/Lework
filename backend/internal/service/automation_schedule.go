// 自动化调度纯逻辑组件（不访问数据库）。
//
// 负责将前端表单配置（AutomationScheduleFormConfig）编译为规范化调度规则
// （AutomationScheduleSpec），并计算下一次执行时间。Phase 2 将在此基础上
// 扩展为完整的 ScheduleEngine，支持 Planner 的遗漏折叠与 DST 边界计算。
package service

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "time/tzdata"

	"github.com/insmtx/Leros/backend/types"
)

const (
	// AutomationScheduleVersion 当前规范化调度规则版本
	AutomationScheduleVersion = types.AutomationScheduleVersion
	// minIntervalSeconds 固定间隔最小秒数（5 分钟）
	minIntervalSeconds = 300
)

// 错误码常量（稳定，供前端映射文案）
const (
	errCodeInvalidSchedule = "invalid_automation_schedule"
	errCodeInvalidTimezone = "invalid_automation_timezone"
)

// errInvalidAutomationSchedule 表示调度配置非法
var errInvalidAutomationSchedule = errors.New(string(errCodeInvalidSchedule))

// validateTimezone 校验 IANA 时区是否存在，返回 *time.Location。
func validateTimezone(timezone string) (*time.Location, error) {
	if strings.TrimSpace(timezone) == "" {
		return nil, errors.New(errCodeInvalidTimezone)
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, errors.New(errCodeInvalidTimezone)
	}
	return loc, nil
}

// normalizeTimezone 归一化时区字符串：去掉首尾空格。
func normalizeTimezone(timezone string) string {
	return strings.TrimSpace(timezone)
}

// compileCalendarExpression 将日历类预设编译为 cron 表达式（5 段标准格式）。
//
// 预设只负责生成表达式，不参与后端执行算法选择：
//   - daily:   m h * * *
//   - weekly:  m h * * dow...
//   - monthly: m h dom * *
//   - hourly:  m * * * *
//
// 日历表达式格式为「分 时 日 月 周」（月字段固定为 *，仅用日或周之一）。
func compileCalendarExpression(preset string, cfg *types.AutomationCalendarConfig) (string, error) {
	h, err := validateHour(cfg.Hour)
	if err != nil {
		return "", err
	}
	m, err := validateMinute(cfg.Minute)
	if err != nil {
		return "", err
	}

	switch preset {
	case string(types.AutomationPresetDaily):
		return fmt.Sprintf("%d %d * * *", m, h), nil
	case string(types.AutomationPresetWeekly):
		days, err := validateDaysOfWeek(cfg.DaysOfWeek)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%d %d * * %s", m, h, days), nil
	case string(types.AutomationPresetMonthly):
		dom, err := validateDaysOfMonth(cfg.DaysOfMonth)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%d %d %s * *", m, h, dom), nil
	case string(types.AutomationPresetHourly):
		return fmt.Sprintf("%d * * * *", m), nil
	default:
		return "", errors.New(errCodeInvalidSchedule)
	}
}

func validateHour(hour int) (int, error) {
	if hour < 0 || hour > 23 {
		return 0, errors.New(errCodeInvalidSchedule)
	}
	return hour, nil
}

func validateMinute(minute int) (int, error) {
	if minute < 0 || minute > 59 {
		return 0, errors.New(errCodeInvalidSchedule)
	}
	return minute, nil
}

func validateDaysOfWeek(days []int) (string, error) {
	if len(days) == 0 {
		return "", errors.New(errCodeInvalidSchedule)
	}
	seen := map[int]bool{}
	vals := []int{}
	for _, d := range days {
		if d < 0 || d > 6 || seen[d] {
			return "", errors.New(errCodeInvalidSchedule)
		}
		seen[d] = true
		vals = append(vals, d)
	}
	sort.Ints(vals)
	parts := make([]string, 0, len(vals))
	for _, v := range vals {
		parts = append(parts, strconv.Itoa(v))
	}
	return strings.Join(parts, ","), nil
}

func validateDaysOfMonth(days []int) (string, error) {
	if len(days) == 0 {
		return "", errors.New(errCodeInvalidSchedule)
	}
	seen := map[int]bool{}
	vals := []int{}
	for _, d := range days {
		if d < 1 || d > 31 || seen[d] {
			return "", errors.New(errCodeInvalidSchedule)
		}
		seen[d] = true
		vals = append(vals, d)
	}
	sort.Ints(vals)
	parts := make([]string, 0, len(vals))
	for _, v := range vals {
		parts = append(parts, strconv.Itoa(v))
	}
	return strings.Join(parts, ","), nil
}

// resolveScheduleMode 解析调度模式。
//
// schedule.mode 是唯一权威来源；若顶层 schedule_mode 也被提交，则二者必须一致，
// 避免保存出错误回显。
func resolveScheduleMode(topMode string, formCfg *types.AutomationScheduleFormConfig) (string, error) {
	if formCfg == nil {
		return "", errInvalidAutomationSchedule
	}
	// 约定：表单配置中的 mode 为准
	mode := strings.TrimSpace(formCfg.Mode)
	if mode == "" {
		mode = strings.TrimSpace(topMode)
	}
	if mode != string(types.AutomationScheduleModeCalendar) && mode != string(types.AutomationScheduleModeInterval) {
		return "", errInvalidAutomationSchedule
	}
	// 双字段都存在时必须一致，避免外部队列调用保存出错误回显
	if top := strings.TrimSpace(topMode); top != "" && top != mode {
		return "", errInvalidAutomationSchedule
	}
	return mode, nil
}

// compileScheduleSpec 将表单配置编译为规范化调度规则。
//
// cfg.Timezone 优先；若为空回退到 fallbackTimezone（来自请求顶层 timezone 字段，
// 或浏览器 IANA 时区）。mode 来源统一为 formCfg.Mode（与顶层 schedule_mode 对齐）。
func compileScheduleSpec(formCfg *types.AutomationScheduleFormConfig, fallbackTimezone, topMode string) (*types.AutomationScheduleSpec, error) {
	return compileScheduleSpecAt(formCfg, fallbackTimezone, topMode, time.Now().UTC())
}

// compileScheduleSpecAt 编译调度规则，并用同一个时间基准生成 interval 起算点。
func compileScheduleSpecAt(
	formCfg *types.AutomationScheduleFormConfig,
	fallbackTimezone, topMode string,
	now time.Time,
) (*types.AutomationScheduleSpec, error) {
	if formCfg == nil {
		return nil, errInvalidAutomationSchedule
	}
	mode, err := resolveScheduleMode(topMode, formCfg)
	if err != nil {
		return nil, err
	}

	timezone := normalizeTimezone(formCfg.Timezone)
	if timezone == "" {
		timezone = normalizeTimezone(fallbackTimezone)
	}
	if _, err := validateTimezone(timezone); err != nil {
		return nil, err
	}

	spec := &types.AutomationScheduleSpec{
		FormConfig: formCfg,
		Spec: types.AutomationScheduleSpecItem{
			Version:  AutomationScheduleVersion,
			Mode:     mode,
			Timezone: timezone,
		},
	}

	switch mode {
	case string(types.AutomationScheduleModeCalendar):
		if formCfg.Calendar == nil {
			return nil, errInvalidAutomationSchedule
		}
		expression, err := compileCalendarExpression(formCfg.Calendar.Preset, formCfg.Calendar)
		if err != nil {
			return nil, err
		}
		spec.Spec.Expression = expression
		spec.Spec.Policy = &types.AutomationSchedulePolicy{
			MonthDayOverflow: "last_day",
		}
	case string(types.AutomationScheduleModeInterval):
		if formCfg.Interval == nil {
			return nil, errInvalidAutomationSchedule
		}
		intervalSeconds := formCfg.Interval.IntervalSeconds
		if intervalSeconds == 0 {
			intervalSeconds = int64(formCfg.Interval.IntervalMinutes) * 60
		}
		if intervalSeconds < minIntervalSeconds {
			return nil, errInvalidAutomationSchedule
		}
		spec.Spec.IntervalSeconds = intervalSeconds

		spec.Spec.OriginAt = now.UTC().Format(time.RFC3339Nano)
	default:
		return nil, errInvalidAutomationSchedule
	}

	return spec, nil
}

// computeNextRunAt 计算下一次执行时间（UTC），停用或计算失败返回 nil。
func computeNextRunAt(spec *types.AutomationScheduleSpec, enabled bool, now time.Time) *time.Time {
	if spec == nil || !enabled {
		return nil
	}
	loc, err := validateTimezone(spec.Spec.Timezone)
	if err != nil {
		return nil
	}
	var next time.Time
	switch spec.Spec.Mode {
	case string(types.AutomationScheduleModeCalendar):
		next, err = nextCalendar(now, spec.Spec.Expression, spec.Spec.Policy, loc)
	case string(types.AutomationScheduleModeInterval):
		if spec.Spec.OriginAt == "" {
			return nil
		}
		next, err = nextIntervalFromOrigin(now, spec.Spec.OriginAt, spec.Spec.IntervalSeconds)
	default:
		return nil
	}
	if err != nil {
		return nil
	}
	utc := next.UTC()
	return &utc
}

// OccurrenceWindow 表示一次到期的调度窗口。
type OccurrenceWindow struct {
	// LatestDue 最近一次遗漏的 occurrence（Planner 需为它创建 execution）
	LatestDue time.Time
	// MissedCount 被折叠的更早遗漏数量
	MissedCount int
	// Next 推送到未来的下一次 occurrence
	Next time.Time
}

// 用于 Planner 的遗漏折叠：服务停机或错过多个周期时，只执行最近一次遗漏，
// 更早遗漏写入 missed_count，并把 next_run_at 推进到未来第一次 occurrence。
func ComputeOccurrenceWindow(spec *types.AutomationScheduleSpec, oldNextRunAt, now time.Time) (*OccurrenceWindow, error) {
	if spec == nil {
		return nil, errInvalidAutomationSchedule
	}
	loc, err := validateTimezone(spec.Spec.Timezone)
	if err != nil {
		return nil, err
	}
	// 统一为 UTC 绝对瞬间比较，避免 CST/UTC 混用导致 ±8h 偏移
	oldUTC := oldNextRunAt.UTC()
	nowUTC := now.UTC()

	switch spec.Spec.Mode {
	case string(types.AutomationScheduleModeCalendar):
		// 从 oldNextRunAt 起逐个向后枚举。
		// LatestDue 初始化为 oldNextRunAt（真正的计划时间）；只有发现更早遗漏的已到期 occurrence
		// 时才递增 missed_count 并更新 LatestDue。避免无遗漏时 LatestDue 为零值、
		// 被上层 fallback 成扫描时刻导致"提前执行"。
		cur := oldUTC
		latest := oldUTC
		missed := 0
		for i := 0; i < 100000; i++ {
			next, nextErr := nextCalendar(cur.Add(time.Nanosecond), spec.Spec.Expression, spec.Spec.Policy, loc)
			if nextErr != nil {
				return nil, nextErr
			}
			nextUTC := next.UTC()
			if nextUTC.After(nowUTC) {
				return &OccurrenceWindow{LatestDue: latest, MissedCount: missed, Next: nextUTC}, nil
			}
			latest = nextUTC
			missed++
			cur = nextUTC
		}
		return nil, errors.New(errCodeInvalidSchedule)
	case string(types.AutomationScheduleModeInterval):
		if spec.Spec.OriginAt == "" {
			return nil, errInvalidAutomationSchedule
		}
		return computeIntervalWindowFromOrigin(spec, oldNextRunAt, now), nil
	default:
		return nil, errInvalidAutomationSchedule
	}
}

// 解析 5 段 cron 表达式（分 时 日 月 周），只处理本项目编译产生的形态。
func nextCalendar(now time.Time, expression string, policy *types.AutomationSchedulePolicy, loc *time.Location) (time.Time, error) {
	fields := strings.Fields(expression)
	if len(fields) != 5 {
		return time.Time{}, errors.New(errCodeInvalidSchedule)
	}
	minute, err := strconv.Atoi(fields[0])
	if err != nil {
		return time.Time{}, errInvalidAutomationSchedule
	}
	dows := parseCommaInts(fields[4])
	doms := parseCommaInts(fields[2])
	hourly := fields[1] == "*" // hourly 预设：时字段为 *

	if hourly {
		return nextHourly(now, minute, loc)
	}

	hour, err := strconv.Atoi(fields[1])
	if err != nil {
		return time.Time{}, errInvalidAutomationSchedule
	}

	// 从候选日的本地时间向后探测
	t := now.In(loc)
	cur := time.Date(t.Year(), t.Month(), t.Day(), hour, minute, 0, 0, loc)

	// 最多探测 400 天，覆盖月任务与边界
	for i := 0; i < 4000; i++ {
		cand, ok := constructLocalTime(cur.Year(), cur.Month(), cur.Day(), hour, minute, loc)
		if ok && cand.After(now) && matchesCalendar(cand, dows, doms, policy) {
			return cand, nil
		}
		cur = cur.AddDate(0, 0, 1)
	}
	return time.Time{}, errors.New(errCodeInvalidSchedule)
}

// constructLocalTime 在指定时区构造墙钟时间，并检测 DST 缺失/重复。
//
// DST gap（目标当地时间不存在）：返回 (zero,false)，由调用方顺延。
// DST repeat（目标当地时间出现两次）：构造返回第一次映射（time.Date 的默认行为），
// 保证同一当地时间只产生一个 occurrence。
func constructLocalTime(year int, month time.Month, day, hour, minute int, loc *time.Location) (time.Time, bool) {
	cand := time.Date(year, month, day, hour, minute, 0, 0, loc)
	// 校验墙钟是否与目标一致：不一致说明落在 DST gap（被隐式归一化），标记为无效。
	if cand.Hour() != hour || cand.Minute() != minute {
		return time.Time{}, false
	}
	return cand, true
}

// nextHourly 计算严格晚于 now 的下一次整点执行（分钟固定）。
func nextHourly(now time.Time, minute int, loc *time.Location) (time.Time, error) {
	t := now.In(loc)
	base := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), minute, 0, 0, loc)
	if !base.After(now) {
		base = base.Add(time.Hour)
	}
	// 处理跨天时的时间 Date 归一化
	if minute < 0 || minute > 59 {
		return time.Time{}, errInvalidAutomationSchedule
	}
	return base, nil
}

func matchesCalendar(cand time.Time, dows, doms []int, policy *types.AutomationSchedulePolicy) bool {
	// dom 由 cron 的 dom 字段（* 或具体日期）决定
	domMatch := true
	if len(doms) > 0 {
		domMatch = false
		for _, d := range doms {
			domMatch = domMatch || cand.Day() == d
		}
		// 月末回退：目标月份没有该日期时，命中该月最后一天
		if !domMatch && policy != nil && policy.MonthDayOverflow == "last_day" {
			lastDay := lastDayOfMonth(cand)
			domMatch = cand.Day() == lastDay && monthHasLessThanMax(cand, doms)
		}
	}
	// dow 由 cron 的 dow 字段（* 或具体星期）决定
	dowMatch := true
	if len(dows) > 0 {
		dowMatch = false
		for _, d := range dows {
			dowMatch = dowMatch || int(cand.Weekday()) == d
		}
	}
	return domMatch && dowMatch
}

// monthHasLessThanMax 判断目标月份实际天数是否小于配置中的最大日期（即存在溢出）。
func monthHasLessThanMax(cand time.Time, doms []int) bool {
	daysInMonth := time.Date(cand.Year(), cand.Month()+1, 0, 0, 0, 0, 0, cand.Location()).Day()
	maxConfig := 0
	for _, d := range doms {
		if d > maxConfig {
			maxConfig = d
		}
	}
	return maxConfig > daysInMonth
}

func lastDayOfMonth(t time.Time) int {
	return time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, t.Location()).Day()
}

func parseCommaInts(s string) []int {
	if s == "" || s == "*" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil {
			continue
		}
		out = append(out, v)
	}
	return out
}

// nextInterval 计算严格晚于 now 的下一次固定间隔执行时间（锚点网格语义）。
//
// 锚点（anchor_at，HH:MM 相位或绝对时间）是网格真起点；绝对锚点 = now 所在日期的锚点相位。
// 后续执行沿同一时间轴以固定秒数推进：Next = 绝对锚点 + k*interval，k 用算术计算。
// 返回 UTC。
func nextInterval(now time.Time, anchorAt string, intervalSeconds int64, loc *time.Location) (time.Time, error) {
	interval := time.Duration(intervalSeconds) * time.Second
	if interval <= 0 {
		return time.Time{}, errInvalidAutomationSchedule
	}
	anchor, err := parseAnchor(anchorAt, loc)
	if err != nil {
		return time.Time{}, err
	}
	base := anchorGridPoint(now, anchor.In(loc))
	next, err := nextIntervalCounted(now, base, interval)
	if err != nil {
		return time.Time{}, err
	}
	return next, nil
}

// nextIntervalFromOrigin 计算 v2 固定间隔规则的下一次绝对 occurrence。
func nextIntervalFromOrigin(now time.Time, originAt string, intervalSeconds int64) (time.Time, error) {
	interval := time.Duration(intervalSeconds) * time.Second
	if interval <= 0 {
		return time.Time{}, errInvalidAutomationSchedule
	}
	origin, err := time.Parse(time.RFC3339Nano, originAt)
	if err != nil {
		return time.Time{}, errInvalidAutomationSchedule
	}
	origin = origin.UTC()
	nowUTC := now.UTC()
	if nowUTC.Before(origin) {
		return origin, nil
	}
	elapsed := nowUTC.Sub(origin)
	steps := elapsed/interval + 1
	return origin.Add(steps * interval), nil
}

// computeIntervalWindowFromOrigin 计算 v2 固定间隔规则的遗漏窗口。
func computeIntervalWindowFromOrigin(
	spec *types.AutomationScheduleSpec,
	oldNextRunAt, now time.Time,
) *OccurrenceWindow {
	interval := time.Duration(spec.Spec.IntervalSeconds) * time.Second
	if interval <= 0 {
		return &OccurrenceWindow{Next: now.UTC()}
	}
	oldUTC := oldNextRunAt.UTC()
	nowUTC := now.UTC()
	if nowUTC.Before(oldUTC) {
		return &OccurrenceWindow{LatestDue: oldUTC, Next: oldUTC}
	}
	elapsed := nowUTC.Sub(oldUTC)
	dueSteps := elapsed / interval
	latest := oldUTC.Add(dueSteps * interval)
	next := latest.Add(interval)
	missed := int(dueSteps) - 1
	if missed < 0 {
		missed = 0
	}
	return &OccurrenceWindow{LatestDue: latest, MissedCount: missed, Next: next}
}

// computeIntervalWindow 用算术（非循环）计算 interval 的遗漏窗口。
//
// 绝对锚点 = oldNextRunAt 所在日期的锚点相位；网格沿同一时间轴按 interval 推进。
// k = ceil((now - 绝对锚点)/interval)，Next = 绝对锚点 + k*interval（第一个未来点），
// LatestDue = 绝对锚点 + (k-1)*interval（最近一次已到期点），missed = k-1。
func computeIntervalWindow(spec *types.AutomationScheduleSpec, oldNextRunAt, now time.Time, loc *time.Location) *OccurrenceWindow {
	interval := time.Duration(spec.Spec.IntervalSeconds) * time.Second
	if interval <= 0 || spec == nil {
		return &OccurrenceWindow{Next: now.UTC(), MissedCount: 0}
	}
	anchor, err := parseAnchor(spec.Spec.AnchorAt, loc)
	if err != nil {
		return &OccurrenceWindow{Next: now.UTC(), MissedCount: 0}
	}
	nowUTC := now.UTC()
	base := anchorGridPoint(oldNextRunAt, anchor.In(loc)).UTC()

	elapsed := nowUTC.Sub(base)
	if elapsed < 0 {
		// now 早于锚点：下一次就是锚点
		return &OccurrenceWindow{LatestDue: base, MissedCount: 0, Next: base}
	}
	k := elapsed / interval
	if elapsed%interval != 0 {
		k++
	}
	next := base.Add(interval * k)
	latest := base.Add(interval * (k - 1))

	// missed_count = 距上次计划触发点（oldNextRunAt）之间被折叠的更早周期数。
	// 以 oldNextRunAt 为基准（而非锚点），正常准点执行时为 0，停机错过 N 个周期时为 N-1。
	fromAlreadyScheduled := nowUTC.Sub(oldNextRunAt.UTC())
	if fromAlreadyScheduled < 0 {
		fromAlreadyScheduled = 0
	}
	missed := int(fromAlreadyScheduled / interval)
	if fromAlreadyScheduled%interval != 0 {
		missed++
	}
	if missed > 0 {
		missed--
	}
	return &OccurrenceWindow{LatestDue: latest, MissedCount: missed, Next: next}
}

// nextIntervalCounted 返回绝对锚点 base 之后、严格晚于 now 的第一个网格点（算术计算）。
func nextIntervalCounted(now, base time.Time, interval time.Duration) (time.Time, error) {
	if interval <= 0 {
		return time.Time{}, errInvalidAutomationSchedule
	}
	nowUTC := now.UTC()
	baseUTC := base.UTC()
	elapsed := nowUTC.Sub(baseUTC)
	if elapsed < 0 {
		return baseUTC, nil
	}
	k := elapsed / interval
	if elapsed%interval != 0 {
		k++
	}
	return baseUTC.Add(interval * k), nil
}

// anchorGridPoint 返回 t 所在日期、以 anchor 相位为起点的网格点。
func anchorGridPoint(t time.Time, anchor time.Time) time.Time {
	loc := anchor.Location()
	local := t.In(loc)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	return time.Date(day.Year(), day.Month(), day.Day(),
		anchor.Hour(), anchor.Minute(), anchor.Second(), anchor.Nanosecond(), loc)
}

// parseAnchor 解析本地时区锚点。
func parseAnchor(anchorAt string, loc *time.Location) (time.Time, error) {
	trimmed := strings.TrimSpace(anchorAt)
	if trimmed == "" {
		return time.Time{}, errInvalidAutomationSchedule
	}
	// 纯 HH:MM / HH:MM:SS 相位
	if t, err := time.ParseInLocation("15:04:05", trimmed, loc); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("15:04", trimmed, loc); err == nil {
		return t, nil
	}
	// 优先按 RFC3339 解析（带偏移）
	if t, err := time.Parse(time.RFC3339, trimmed); err == nil {
		return t, nil
	}
	// 无偏移的本地时间，按 loc 解释
	if t, err := time.ParseInLocation("2006-01-02T15:04:05", trimmed, loc); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", trimmed, loc); err == nil {
		return t, nil
	}
	return time.Time{}, errInvalidAutomationSchedule
}

// buildAutomationSummary 根据规范化的调度规则生成前端展示摘要文案。
func buildAutomationSummary(spec *types.AutomationScheduleSpec) string {
	if spec == nil {
		return ""
	}
	switch spec.Spec.Mode {
	case string(types.AutomationScheduleModeCalendar):
		fields := strings.Fields(spec.Spec.Expression)
		if len(fields) != 5 {
			return ""
		}
		minute, _ := strconv.Atoi(fields[0])
		dowStr, domStr := fields[4], fields[2]
		dows := parseCommaInts(dowStr)
		doms := parseCommaInts(domStr)
		hourly := fields[1] == "*"

		switch {
		case hourly:
			// 每小时在指定的分钟执行：时字段为 *
			return fmt.Sprintf("每小时的 %02d 分执行", minute)
		case len(doms) > 0:
			return fmt.Sprintf("每月%s %s", monthDayDesc(doms), formatSummaryTime(fields[1], minute))
		case len(dows) > 0:
			return fmt.Sprintf("每周%s %s", weekDayDesc(dows), formatSummaryTime(fields[1], minute))
		default:
			return fmt.Sprintf("每天 %s", formatSummaryTime(fields[1], minute))
		}
	case string(types.AutomationScheduleModeInterval):
		secs := spec.Spec.IntervalSeconds
		return fmt.Sprintf("每 %s 执行一次", formatInterval(secs))
	default:
		return ""
	}
}

// formatSummaryTime 格式化小时:分钟；hour 字段为数字（daily/weekly/monthly）。
func formatSummaryTime(hourField string, minute int) string {
	h, _ := strconv.Atoi(hourField)
	return fmt.Sprintf("%02d:%02d", h, minute)
}

func formatInterval(seconds int64) string {
	if seconds >= 86400 && seconds%86400 == 0 {
		return fmt.Sprintf("%d 天", seconds/86400)
	}
	if seconds >= 3600 && seconds%3600 == 0 {
		return fmt.Sprintf("%d 小时", seconds/3600)
	}
	if seconds >= 60 && seconds%60 == 0 {
		return fmt.Sprintf("%d 分钟", seconds/60)
	}
	return fmt.Sprintf("%d 秒", seconds)
}

func weekDayDesc(dows []int) string {
	names := []string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}
	// 与前端表单展示一致：周一至周六按 1-6 升序、周日(0)排在末尾
	sorted := make([]int, 0, len(dows))
	for _, d := range dows {
		if d >= 0 && d <= 6 {
			sorted = append(sorted, d)
		}
	}
	sort.Slice(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a == 0 {
			return false
		}
		if b == 0 {
			return true
		}
		return a < b
	})
	parts := make([]string, 0, len(sorted))
	for _, d := range sorted {
		parts = append(parts, names[d])
	}
	return strings.Join(parts, "、")
}

func monthDayDesc(doms []int) string {
	parts := make([]string, 0, len(doms))
	for _, d := range doms {
		parts = append(parts, fmt.Sprintf("%d 日", d))
	}
	return strings.Join(parts, "、")
}
