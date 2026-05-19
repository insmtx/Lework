import { sessionApi } from "../api/sessionApi";
import type { BackendSession } from "../api/types";
import type { SliceCreator } from "../types";
import { flattenActions } from "../utils";

export type WorkspaceMode = "remote" | "local";

export type Conversation = {
	id: string;
	title: string;
	type: string;
	status: string;
	createdAt: number;
	updatedAt: number;
};

export type Workspace = {
	id: string;
	name: string;
	mode: WorkspaceMode;
	collapsed: boolean;
};

export type NavGroup = {
	id: string;
	label: string;
	items: NavItem[];
};

export type NavItem = {
	id: string;
	label: string;
	icon: string;
	badge?: number;
};

export type ViewMode = "chat" | "workbench" | "digitalAssistant" | "knowledge" | "skills" | "settings";

export type LayoutState = {
	leftRailCollapsed: boolean;
	rightRailCollapsed: boolean;
	conversationListOpen: boolean;
	currentView: ViewMode;
	activeConversationId: string | null;
	activeWorkspaceId: string | null;
	workspaces: Workspace[];
	conversations: Conversation[];
	conversationsLoaded: boolean;
	inputFocused: boolean;
	activeRightTab: "shortcuts" | "inbox" | "artifacts";
	navGroups: NavGroup[];
	collapsedNavGroups: Set<string>;
	conversationSearchQuery: string;
};

export type LayoutAction = Pick<LayoutActionImpl, keyof LayoutActionImpl>;
export type LayoutStore = LayoutState & LayoutAction;

function mapSessionToConversation(s: BackendSession): Conversation {
	return {
		id: s.session_id,
		title: s.title || "未命名会话",
		type: s.type,
		status: s.status,
		createdAt: new Date(s.created_at).getTime(),
		updatedAt: new Date(s.updated_at).getTime(),
	};
}

const _initialState: LayoutState = {
	leftRailCollapsed: false,
	rightRailCollapsed: false,
	conversationListOpen: true,
	currentView: "chat",
	activeConversationId: null,
	activeWorkspaceId: null,
	workspaces: [
		{ id: "remote-1", name: "远程工作区", mode: "remote", collapsed: false },
		{ id: "local-1", name: "本地工作区", mode: "local", collapsed: false },
	],
	conversations: [],
	conversationsLoaded: false,
	inputFocused: false,
	activeRightTab: "shortcuts",
	navGroups: [
		{
			id: "core",
			label: "",
			items: [
				{ id: "ai-assistant", label: "AI 助手", icon: "IconRobot" },
				{ id: "workbench", label: "工作台", icon: "IconCommand" },
			],
		},
		{
			id: "ai-capability",
			label: "AI 能力",
			items: [
				{ id: "ai-employee", label: "AI 员工", icon: "IconUsers" },
				{ id: "knowledge", label: "知识库", icon: "IconBook" },
				{ id: "skills", label: "技能管理", icon: "IconStar" },
			],
		},
		{
			id: "system",
			label: "系统",
			items: [{ id: "settings", label: "个人设置", icon: "IconSettings" }],
		},
	],
	collapsedNavGroups: new Set(),
	conversationSearchQuery: "",
};

type SetState = (
	partial:
		| LayoutStore
		| Partial<LayoutStore>
		| ((state: LayoutStore) => LayoutStore | Partial<LayoutStore>),
	replace?: boolean,
) => void;

export const createLayoutSlice = (set: SetState, get: () => LayoutStore) =>
	new LayoutActionImpl(set, get);

export class LayoutActionImpl {
	readonly #set: SetState;
	readonly #get: () => LayoutStore;

	constructor(set: SetState, get: () => LayoutStore) {
		this.#set = set;
		this.#get = get;
	}

	toggleLeftRail = () => {
		this.#set((state) => ({ leftRailCollapsed: !state.leftRailCollapsed }));
	};

	toggleConversationList = () => {
		this.#set((state) => ({
			conversationListOpen: !state.conversationListOpen,
		}));
	};

	switchView = (view: ViewMode) => {
		this.#set({
			currentView: view,
			conversationListOpen: view === "chat",
		});
	};

	toggleRightRail = () => {
		this.#set((state) => ({ rightRailCollapsed: !state.rightRailCollapsed }));
	};

	toggleWorkspaceCollapse = (workspaceId: string) => {
		this.#set((state) => ({
			workspaces: state.workspaces.map((w) =>
				w.id === workspaceId ? { ...w, collapsed: !w.collapsed } : w,
			),
		}));
	};

	switchConversation = (conversationId: string) => {
		this.#set({ activeConversationId: conversationId });
	};

	fetchConversations = async () => {
		if (this.#get().conversationsLoaded) return;
		try {
			const res = await sessionApi.list({ page: 1, per_page: 50 });
			const items = res.data.data?.items ?? [];
			this.#set({
				conversations: items.map(mapSessionToConversation),
				conversationsLoaded: true,
			});
		} catch (err) {
			console.error("fetchConversations error:", err);
		}
	};

	createConversation = async (title: string) => {
		try {
			const res = await sessionApi.create({
				type: "chat",
				title: title || "新会话",
			});
			const session = res.data.data;
			if (!session) throw new Error("No session data returned");
			const conv = mapSessionToConversation(session);
			this.#set((state) => ({
				conversations: [conv, ...state.conversations],
				activeConversationId: conv.id,
				conversationsLoaded: true,
			}));
			return conv;
		} catch (err) {
			console.error("createConversation error:", err);
			return null;
		}
	};

	deleteConversation = async (conversationId: string) => {
		const state = this.#get();
		const conv = state.conversations.find((c) => c.id === conversationId);
		if (!conv) return;

		try {
			await sessionApi.delete(conv.id);
			this.#set((state) => ({
				conversations: state.conversations.filter((c) => c.id !== conversationId),
				activeConversationId:
					state.activeConversationId === conversationId ? null : state.activeConversationId,
			}));
		} catch (err) {
			console.error("deleteConversation error:", err);
		}
	};

	updateConversationTitle = async (conversationId: string, title: string) => {
		const state = this.#get();
		const conv = state.conversations.find((c) => c.id === conversationId);
		if (!conv) return;

		try {
			await sessionApi.update({ session_id: conv.id, title });
			this.#set((state) => ({
				conversations: state.conversations.map((c) =>
					c.id === conversationId ? { ...c, title, updatedAt: Date.now() } : c,
				),
			}));
		} catch (err) {
			console.error("updateConversationTitle error:", err);
		}
	};

	setInputFocused = (focused: boolean) => {
		this.#set({ inputFocused: focused });
	};

	setActiveRightTab = (tab: "shortcuts" | "inbox" | "artifacts") => {
		this.#set({ activeRightTab: tab });
	};

	toggleNavGroup = (groupId: string) => {
		this.#set((state) => {
			const collapsed = new Set(state.collapsedNavGroups);
			if (collapsed.has(groupId)) {
				collapsed.delete(groupId);
			} else {
				collapsed.add(groupId);
			}
			return { collapsedNavGroups: collapsed };
		});
	};

	setConversationSearchQuery = (query: string) => {
		this.#set({ conversationSearchQuery: query });
	};
}

export const layoutSlice: SliceCreator<LayoutStore> = (...params) => ({
	..._initialState,
	...flattenActions<LayoutAction>([createLayoutSlice(params[0] as SetState, params[1])]),
});
