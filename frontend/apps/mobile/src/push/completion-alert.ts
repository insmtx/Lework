import type { ControlEvent } from "@lework/api";

/**
 * CompletionAlert 是 App 在前台收到「跑完了」时本地弹出的提醒。
 *
 * 它只依赖控制面事件，不经过 Expo 推送：推送需要 EAS 项目与 FCM 凭据，而
 * 缺少凭据时 App 连设备都登记不上，等推送只会永远等不到。
 */
export interface CompletionAlert {
	nodeID: string;
	sessionID: string;
	kind: "done" | "failed";
	title: string;
	body: string;
}

/** completionAlertFrom 把执行结束事件翻译成本地提醒；其它事件返回 null。 */
export function completionAlertFrom(event: ControlEvent): CompletionAlert | null {
	const nodeID = event.node_id;
	const sessionID = event.session_id;
	if (!nodeID || !sessionID) return null;

	switch (event.type) {
		case "session.execution.succeeded":
			return { nodeID, sessionID, kind: "done", title: "执行完成", body: "点开查看结果" };
		case "session.execution.failed":
			return { nodeID, sessionID, kind: "failed", title: "执行失败", body: "点开查看错误" };
		default:
			return null;
	}
}
