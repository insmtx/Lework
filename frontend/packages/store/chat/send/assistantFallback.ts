/**
 * 问答路径：等待 GlobalEvents assistant 的超时兜底。
 *
 * 可以做：在完整等待窗口内观察本地态；run 尚未 responding 时少量轮询 GetSession；
 * GE 已接管或 run 已结束时提前退出；超时后把占位标失败并写入正文报错。
 * 不可以做：因 runtime_status=responding 提前失败（responding ≠ GE assistant 已到）；
 * 不可以在 responding 后继续空转打 GetSession；不可以在此处打开 SessionEvents。
 */
import { sessionApi } from "../../api/sessionApi";
import {
	ASSISTANT_GLOBAL_EVENTS_TIMEOUT_TEXT,
	TASK_ROOM_ASSISTANT_START_FALLBACK_MS,
} from "../messageMerge";
import type { SendPipelineDeps } from "./deps";

const POLL_INTERVAL_MS = 2_000;

type SessionProbeResult = "idle_finished" | "responding" | "pending";

/** 超时收尾：写入报错、抑制本轮后续 GE/resume，并结束 generating。 */
function failWaitingAssistant(
	deps: SendPipelineDeps,
	sessionId: string,
	assistantMsgId: string,
): void {
	const current = deps.get().messagesMap[assistantMsgId];
	if (current) {
		deps.updateMessage(assistantMsgId, {
			...current,
			status: "failed",
			statusText: undefined,
			// 中文注释：与 run.failed 一致写入正文，确保回复气泡直接展示报错。
			content: ASSISTANT_GLOBAL_EVENTS_TIMEOUT_TEXT,
		});
	}
	deps.set({
		pendingBootstrapSessionId: null,
		// 中文注释：抑制迟到 GE assistant，以及超时后 isGenerating→false 触发的 resume 开流。
		suppressedReplySessionId: sessionId,
	});
	deps.finishStream();
}

/** 本地 waiting 是否已被 GE assistant 接管（或用户已离开/取消）。 */
function isWaitingAssistantReleased(
	deps: SendPipelineDeps,
	sessionId: string,
	assistantMsgId: string,
): boolean {
	const state = deps.get();
	return (
		state.activeSessionId !== sessionId ||
		state.streamingMessageId !== assistantMsgId ||
		!state.isGenerating
	);
}

/**
 * 探一次 GetSession：idle 且消息增加则回拉历史；responding 告知调用方停止轮询。
 */
async function probeSessionWhileWaiting(
	deps: SendPipelineDeps,
	sessionId: string,
	baselineMessageCount: number,
): Promise<SessionProbeResult> {
	try {
		const res = await sessionApi.get({ session_id: sessionId });
		const status = res.data.data?.runtime_status;
		const messageCount = res.data.data?.message_count;
		// 中文注释：run 已结束且消息数增加，直接回拉历史；仍不开放 SessionEvents。
		if (messageCount !== undefined && messageCount > baselineMessageCount && status === "idle") {
			deps.set({ pendingBootstrapSessionId: null });
			await deps.loadConversationMessages(sessionId, { resumeStream: false });
			deps.finishStream();
			return "idle_finished";
		}
		if (status === "responding") return "responding";
		return "pending";
	} catch {
		return "pending";
	}
}

/**
 * 等不到 GlobalEvents assistant 时的终态收尾。
 * 任务群聊续聊与新建任务 bootstrap 共用，避免两套超时语义分叉。
 *
 * GetSession 只用在「尚未 responding」时探活，以及超时前最后一次探 idle；
 * 一旦已是 responding，只靠本地态等 GE，禁止继续轮询。
 */
export async function waitForGlobalAssistantOrFail(
	deps: SendPipelineDeps,
	sessionId: string,
	assistantMsgId: string,
): Promise<void> {
	const deadline = Date.now() + TASK_ROOM_ASSISTANT_START_FALLBACK_MS;
	let baselineMessageCount = 0;
	/** 已确认后端在跑后不再打 GetSession，避免 responding 下空转轮询。 */
	let runtimeKnownResponding = false;
	try {
		const sessionRes = await sessionApi.get({ session_id: sessionId });
		baselineMessageCount = sessionRes.data.data?.message_count ?? 0;
		runtimeKnownResponding = sessionRes.data.data?.runtime_status === "responding";
	} catch (err) {
		console.error("waitForGlobalAssistantOrFail baseline error:", err);
	}

	try {
		while (Date.now() < deadline) {
			await new Promise((resolve) => setTimeout(resolve, POLL_INTERVAL_MS));
			// 中文注释：GlobalEvents assistant 到达后会替换 waiting 占位并改 streamingMessageId，兜底立刻退出。
			if (isWaitingAssistantReleased(deps, sessionId, assistantMsgId)) {
				return;
			}

			// 中文注释：已 responding 只等 GE / 总超时，不再每 2s GetSession。
			if (runtimeKnownResponding) {
				continue;
			}

			const probe = await probeSessionWhileWaiting(deps, sessionId, baselineMessageCount);
			if (
				probe === "idle_finished" ||
				isWaitingAssistantReleased(deps, sessionId, assistantMsgId)
			) {
				return;
			}
			if (probe === "responding") {
				runtimeKnownResponding = true;
			}
		}

		if (isWaitingAssistantReleased(deps, sessionId, assistantMsgId)) {
			return;
		}
		// 中文注释：超时前最后探一次：GE 丢了但 run 已落库时回拉历史，避免误报超时。
		const finalProbe = await probeSessionWhileWaiting(deps, sessionId, baselineMessageCount);
		if (
			finalProbe === "idle_finished" ||
			isWaitingAssistantReleased(deps, sessionId, assistantMsgId)
		) {
			return;
		}
		failWaitingAssistant(deps, sessionId, assistantMsgId);
	} catch (err) {
		console.error("waitForGlobalAssistantOrFail error:", err);
		if (isWaitingAssistantReleased(deps, sessionId, assistantMsgId)) {
			return;
		}
		failWaitingAssistant(deps, sessionId, assistantMsgId);
	}
}
