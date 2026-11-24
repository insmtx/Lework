import type { SessionState } from "@lework/api";

import type { Theme } from "../theme/theme";

/** SessionStatus 与总览的执行状态同义，不引入第二套状态名。 */
export type SessionStatus = SessionState;

/** StatusTone 决定状态标记的颜色，不绑定具体主题色值。 */
export type StatusTone = "info" | "warn" | "error" | "success" | "muted";

/**
 * statusLabel 把执行状态翻译成一行文案。
 *
 * 文案只描述“服务端知道的事实”：客户端不推断进度，也不假装知道历史。
 */
export function statusLabel(state: SessionStatus | null): string {
	switch (state) {
		case "running":
			return "正在执行…";
		case "waiting_permission":
			return "等待你的处理";
		case "idle":
			return "执行完成";
		case "error":
			return "执行出错";
		case "offline":
			return "节点离线";
		default:
			return "";
	}
}

/**
 * statusGlyph 给静态状态挑一个图形符号；running/waiting 改用色块动画。
 *
 * 只有单个符号，避免状态文字挤占标题；完整含义放在无障碍标签里。
 */
export function statusGlyph(state: SessionStatus | null): string {
	switch (state) {
		case "running":
			return "";
		case "waiting_permission":
			return "!";
		case "idle":
			return "✓";
		case "error":
			return "✕";
		case "offline":
			return "⊘";
		default:
			return "";
	}
}

/**
 * rowSweepKind 决定会话行铺什么背景。
 *
 * 运行与等待铺扫光，出错铺静态淡红：会动的底色会被读成“还在跑”。其余状态
 * 不铺，避免整个列表都在发光。
 */
export function rowSweepKind(state: SessionStatus | null): "sweep" | "static" | null {
	switch (state) {
		case "running":
		case "waiting_permission":
			return "sweep";
		case "error":
			return "static";
		default:
			return null;
	}
}

/** statusTone 给状态配色：完成用成功色，只有需要人动手或出错的才用醒目色。 */
export function statusTone(state: SessionStatus | null): StatusTone {
	switch (state) {
		case "running":
			return "info";
		case "waiting_permission":
			return "warn";
		case "idle":
			return "success";
		case "error":
			return "error";
		default:
			return "muted";
	}
}

/**
 * statusColor 把状态语气映射到主题色，会话页与子代理页共用同一套语义。
 *
 * 颜色只由语气决定，页面不再各自写一份 switch；以后加状态时只需要改这里。
 */
export function statusColor(theme: Theme, tone: StatusTone): string {
	switch (tone) {
		case "info":
			return theme.colors.accent;
		case "warn":
			return theme.colors.warning;
		case "error":
			return theme.colors.danger;
		case "success":
			return theme.colors.success;
		default:
			return theme.colors.offline;
	}
}
