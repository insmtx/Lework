import type { SessionState } from "@lework/api";

/** SessionStatus 与总览的执行状态同义，不引入第二套状态名。 */
export type SessionStatus = SessionState;

/** StatusTone 决定状态条的颜色，不绑定具体主题色值。 */
export type StatusTone = "info" | "warn" | "error" | "muted";

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
			return "等待你的批准";
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

/** statusTone 给状态条配色：只有需要人动手或出错的才用醒目色。 */
export function statusTone(state: SessionStatus | null): StatusTone {
	switch (state) {
		case "running":
			return "info";
		case "waiting_permission":
			return "warn";
		case "error":
			return "error";
		default:
			return "muted";
	}
}
