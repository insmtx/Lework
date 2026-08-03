/**
 * 对话相关的 layout / project 副作用出口。
 *
 * 可以做：写入 conversations 列表、跳转 taskDetail、触发 fetchProjectDetail、
 * 清空工作台选中态等跨 slice 写入。
 * 不可以做：发 HTTP 消息、开 SSE、改 Message 气泡内容。
 */

/**
 * effects 写入联合 store 所需的最小能力。
 */
export type ChatEffectsDeps = {
	/** 写入任意 slice 字段（Zustand 联合 store） */
	setStore: (partial: Record<string, unknown>) => void;
	/** 读取完整 app store（取 conversations / fetchProjectDetail） */
	fullGet: () => Record<string, unknown>;
};

/** 纯 chat 自动建会话后写入侧栏列表的会话摘要。 */
export type ChatConversationSummary = {
	id: string;
	title: string;
	sessionId: string;
	type?: string;
	status?: string;
	createdAt: number;
	updatedAt: number;
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
	 * 纯 chat 无 activeSession 时：把新建会话插到侧栏列表头部并激活。
	 */
	prependChatConversation = (conv: ChatConversationSummary) => {
		const prev = this.#deps.fullGet() as {
			conversations?: ChatConversationSummary[];
		};
		this.#deps.setStore({
			activeSessionId: conv.sessionId,
			conversations: [conv, ...(prev.conversations ?? [])],
			activeConversationId: conv.id,
			conversationsLoaded: true,
		});
	};

	/**
	 * 新建/续聊任务后写入 taskDetail 路由状态，并按需刷新项目详情。
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
			conversationListOpen: false,
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

	/** 清空 composer 草稿（ChatState 字段）。 */
	clearComposer = () => {
		this.#deps.setStore({
			inputText: "",
			inputAttachments: [],
		});
	};
}
