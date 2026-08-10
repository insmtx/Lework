/**
 * 对话相关的 layout / project 副作用出口。
 *
 * 可以做：跳转 taskDetail、触发 fetchProjectDetail、清空工作台选中态等跨 slice 写入。
 * 不可以做：发 HTTP 消息、开 SSE、改 Message 气泡内容。
 */

import type { Attachment } from "../types/chat";
import { revokeAttachmentObjectUrls } from "../utils/messageAttachments";

/**
 * effects 写入联合 store 所需的最小能力。
 */
export type ChatEffectsDeps = {
	/** 写入任意 slice 字段（Zustand 联合 store） */
	setStore: (partial: Record<string, unknown>) => void;
	/** 读取完整 app store（取 fetchProjectDetail） */
	fullGet: () => Record<string, unknown>;
};

/** 跳转到任务详情时的路由与附属选项。 */
export type NavigateToTaskDetailOptions = {
	projectId: string;
	taskId: string;
	sessionId: string;
	/** 工作台发送：清掉 workbench 选中并写入 executionMode */
	fromWorkbench?: boolean;
	executionMode?: "default" | "plan";
	/** true=await 详情；false/缺省=后台 fire-and-forget（项目首页） */
	awaitProjectDetail?: boolean;
};

/**
 * 集中处理 send 管道对 layout / project 的写入，避免散落在各 send 文件里。
 */
export class ChatEffects {
	readonly #deps: ChatEffectsDeps;

	constructor(deps: ChatEffectsDeps) {
		this.#deps = deps;
	}

	/**
	 * 新建/续聊任务后写入 taskDetail 路由状态，并按需刷新项目详情。
	 * 调用方须先 bootstrap（若有），再调用本方法，避免详情页在 waiting 写好前误 load。
	 * 项目首页不 await 详情；工作台新建 await，保证跳转后 store 有任务列表。
	 */
	navigateToTaskDetail = async (options: NavigateToTaskDetailOptions) => {
		const { projectId, taskId, sessionId, fromWorkbench, executionMode, awaitProjectDetail } =
			options;

		this.#deps.setStore({
			activeProjectId: projectId,
			activeTaskDetailProjectId: projectId,
			activeTaskDetailTaskId: taskId,
			activeTaskDetailSessionId: sessionId,
			currentView: "taskDetail",
			activeProjectTab: "chat",
			...(fromWorkbench
				? {
						activeWorkbenchProjectId: null,
						activeWorkbenchTaskId: null,
						...(executionMode ? { executionMode } : {}),
					}
				: {}),
		});

		const fullState = this.#deps.fullGet() as {
			fetchProjectDetail?: (projectId: string) => Promise<void>;
		};
		if (!fullState.fetchProjectDetail) return;
		if (awaitProjectDetail) {
			await fullState.fetchProjectDetail(projectId);
			return;
		}
		// 中文注释：项目详情刷新放到后台，避免阻塞路由跳转到任务对话页。
		void fullState.fetchProjectDetail(projectId);
	};

	/** 清空 composer 草稿，并按统一策略释放本地 blob 预览。 */
	clearComposer = () => {
		const state = this.#deps.fullGet() as { inputAttachments?: Attachment[] };
		revokeAttachmentObjectUrls(state.inputAttachments);
		this.#deps.setStore({
			inputText: "",
			inputAttachments: [],
		});
	};
}
