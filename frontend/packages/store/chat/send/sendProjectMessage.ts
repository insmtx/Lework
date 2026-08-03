/**
 * 路径 B：项目首页 / 工作台「新建任务」发送。
 *
 * 可以做：CreateInitialMessage、bootstrap 乐观 waiting、经 effects 跳转 taskDetail。
 * 不可以做：立刻开 SessionEvents（开流策略=afterGlobalAssistant，由 GlobalEvents 触发）。
 */

import { sessionApi } from "../../api/sessionApi";
import type { BackendNewMessageData } from "../../api/types";
import type { Attachment, MessageMetadata } from "../../types/chat";
import { mapOutgoingAttachments } from "../../utils/messageAttachments";
import { bootstrapNewTaskSession } from "./bootstrap";
import type { SendPipelineDeps } from "./deps";
import { buildBackendMessageMetadata, extractAssistantIdsFromMetadata } from "./metadata";

/**
 * 新建任务发送的扩展选项。
 * 工作台与项目首页共用本函数；差异用选项表达，避免 layout 再抄一套 CreateInitialMessage。
 */
export type SendProjectMessageOptions = {
	attachments?: Attachment[];
	metadata?: MessageMetadata;
	/** 显式 assistant_ids（工作台 @召唤）；缺省则从 metadata tokens 提取 */
	assistantIds?: string[];
	/** 工作台选中已有任务但无 session 时带上 task_id */
	taskId?: string | null;
	/** 覆盖 ChatState.executionMode（工作台发送时写入） */
	executionMode?: "default" | "plan";
	/** 允许空文案 + assistantIds / 仅附件（工作台） */
	allowEmptyContent?: boolean;
	/** 工作台导航：清 workbench 选中；并 await 项目详情 */
	fromWorkbench?: boolean;
};

/**
 * 项目首页或工作台新建任务：CreateInitialMessage → bootstrap → 跳转详情。
 * 不在此处开 SessionEvents；等待 GlobalEvents assistant。
 */
export async function sendProjectMessage(
	deps: SendPipelineDeps,
	content: string,
	projectId?: string | null,
	attachments?: Attachment[],
	metadata?: MessageMetadata,
	options?: SendProjectMessageOptions,
): Promise<BackendNewMessageData | null> {
	const trimmed = content.trim();
	const resolvedAttachments = options?.attachments ?? attachments;
	const resolvedMetadata = options?.metadata ?? metadata;
	const allowEmpty = Boolean(options?.allowEmptyContent);
	const assistantIds = options?.assistantIds ?? extractAssistantIdsFromMetadata(resolvedMetadata);

	if (!allowEmpty && !trimmed) return null;
	if (allowEmpty && !trimmed && !assistantIds?.length && !resolvedAttachments?.length) {
		return null;
	}
	// 中文注释：工作台允许无 projectId（后端自建项目）；项目首页必须带 projectId。
	if (!options?.fromWorkbench && !projectId) return null;
	if (!options?.fromWorkbench && deps.get().isGenerating) return null;

	const executionMode = options?.executionMode ?? deps.get().executionMode;

	try {
		void deps.startGlobalEvents();
		const res = await sessionApi.createInitialMessage({
			content: trimmed,
			execution_mode: executionMode,
			...(projectId ? { project_id: projectId } : {}),
			...(options?.taskId ? { task_id: options.taskId } : {}),
			...(assistantIds?.length ? { assistant_ids: assistantIds } : {}),
			metadata: buildBackendMessageMetadata(resolvedMetadata),
			attachments: mapOutgoingAttachments(resolvedAttachments),
		});
		const data = res.data.data;
		if (!data?.project_id || !data?.task_id || !data?.session_id) return null;

		const navigateOpts = {
			projectId: data.project_id,
			taskId: data.task_id,
			sessionId: data.session_id,
			fromWorkbench: options?.fromWorkbench,
			executionMode: options?.fromWorkbench ? executionMode : undefined,
			awaitProjectDetail: Boolean(options?.fromWorkbench),
		} as const;

		if (options?.fromWorkbench) {
			// 中文注释：工作台原先 await 详情后再 bootstrap，保证跳转后 store 有任务列表可供标题 patch。
			await deps.effects.navigateToTaskDetail(navigateOpts);
			bootstrapNewTaskSession(deps, data.session_id, trimmed, {
				attachments: resolvedAttachments,
				metadata: resolvedMetadata,
			});
		} else {
			// 中文注释：项目首页必须先打 pendingBootstrap，再切视图，避免详情页 remount 清掉乐观等待态。
			bootstrapNewTaskSession(deps, data.session_id, trimmed, {
				attachments: resolvedAttachments,
				metadata: resolvedMetadata,
			});
			await deps.effects.navigateToTaskDetail(navigateOpts);
		}

		deps.effects.clearComposer();
		return data;
	} catch (err) {
		console.error("sendProjectMessage error:", err);
		return null;
	}
}
