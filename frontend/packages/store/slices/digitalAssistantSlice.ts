import { digitalAssistantApi } from "../api/digitalAssistantApi";
import type { BackendDigitalAssistant } from "../api/types";
import type { SliceCreator } from "../types";
import { flattenActions } from "../utils";
import { readStoredAuthUser } from "../utils/authStorage";

export type DigitalAssistantItem = {
	id: number;
	publicId: string;
	code: string;
	name: string;
	roleName: string;
	description: string;
	avatar: string;
	status: string;
	systemPrompt: string;
	expertise: string[];
	templateId?: number;
	source: string;
	deploymentPublicId: string;
	deploymentStatus: string;
	deploymentError: string;
	version: number;
	createdAt: number;
	updatedAt: number;
};

/** 默认兜底 AI 仅供系统调度，不应出现在可选或展示的 AI 队友列表中。 */
export const DEFAULT_SYSTEM_ASSISTANT_PUBLIC_ID_PREFIX = "assistant_default_";

export function isSystemDefaultAssistant(publicId: string | undefined): boolean {
	return publicId?.trim().startsWith(DEFAULT_SYSTEM_ASSISTANT_PUBLIC_ID_PREFIX) ?? false;
}

export type DigitalAssistantState = {
	assistants: DigitalAssistantItem[];
	assistantsLoaded: boolean;
	activeAssistantId: number | null;
	assistantSearchQuery: string;
};

export type DigitalAssistantAction = Pick<DASliceImpl, keyof DASliceImpl>;
export type DAStore = DigitalAssistantState & DigitalAssistantAction;

function mapBackendDA(da: BackendDigitalAssistant): DigitalAssistantItem {
	const publicId = da.public_id || da.code || String(da.id);
	const roleName = da.role_name?.trim() ?? "";
	return {
		id: da.id,
		publicId,
		// 中文注释：输入框选择器仍使用 code 作为本地选项值，后端交互统一取 publicId。
		code: publicId,
		name: da.name,
		// 中文注释：历史数据可能把角色名称回填为队友名称，前端隐藏重复副标题。
		roleName: roleName === da.name.trim() ? "" : roleName,
		description: da.description ?? "",
		avatar: da.avatar ?? "",
		status: da.status,
		systemPrompt: da.system_prompt ?? "",
		expertise: da.expertise ?? [],
		templateId: da.template_id,
		source: da.source ?? "",
		deploymentPublicId: da.deployment?.public_id ?? "",
		deploymentStatus: da.deployment?.status ?? "",
		deploymentError: da.deployment?.last_error ?? "",
		version: da.version,
		createdAt: new Date(da.created_at).getTime(),
		updatedAt: new Date(da.updated_at).getTime(),
	};
}

const _initialState: DigitalAssistantState = {
	assistants: [],
	assistantsLoaded: false,
	activeAssistantId: null,
	assistantSearchQuery: "",
};

type SetState = (
	partial: DAStore | Partial<DAStore> | ((state: DAStore) => DAStore | Partial<DAStore>),
	replace?: boolean,
) => void;

export const createDASlice = (set: SetState) => new DASliceImpl(set);

export class DASliceImpl {
	readonly #set: SetState;
	#fetchAssistantsPromise: Promise<void> | null = null;
	#assistantsFetchEpoch = 0;

	constructor(set: SetState) {
		this.#set = set;
	}

	fetchAssistants = async () => {
		if (!readStoredAuthUser()?.jwtToken) return;
		if (this.#fetchAssistantsPromise) return this.#fetchAssistantsPromise;

		const fetchEpoch = this.#assistantsFetchEpoch;
		this.#fetchAssistantsPromise = (async () => {
			try {
				const res = await digitalAssistantApi.list({ list_all: true, limit: 100 });
				if (fetchEpoch !== this.#assistantsFetchEpoch) return;
				const items = res.data.data?.items ?? [];
				this.#set({
					assistants: items
						.map(mapBackendDA)
						.filter((assistant) => !isSystemDefaultAssistant(assistant.publicId)),
					assistantsLoaded: true,
				});
			} catch (err) {
				console.error("fetchAssistants error:", err);
			} finally {
				if (fetchEpoch === this.#assistantsFetchEpoch) {
					this.#fetchAssistantsPromise = null;
				}
			}
		})();

		return this.#fetchAssistantsPromise;
	};

	resetAuthScopedData = () => {
		this.#assistantsFetchEpoch += 1;
		this.#fetchAssistantsPromise = null;
		this.#set({
			assistants: [],
			assistantsLoaded: false,
			activeAssistantId: null,
			assistantSearchQuery: "",
		});
	};

	createAssistant = async (params: {
		public_id?: string;
		name: string;
		role_name?: string;
		description?: string;
		avatar?: string;
		system_prompt?: string;
		expertise?: string[];
		template_id?: number;
		source?: string;
	}) => {
		try {
			const res = await digitalAssistantApi.create(params);
			const da = res.data.data;
			if (!da) throw new Error("No data returned");
			const item = mapBackendDA(da);
			this.#set((state) => ({
				assistants: [item, ...state.assistants],
				activeAssistantId: item.id,
				assistantsLoaded: true,
			}));
			return item;
		} catch (err) {
			console.error("createAssistant error:", err);
			return null;
		}
	};

	createAssistantFromTemplate = async (params: {
		template_id: number;
		public_id?: string;
		name?: string;
		role_name?: string;
		description?: string;
		avatar?: string;
		system_prompt?: string;
		expertise?: string[];
	}) => {
		try {
			const res = await digitalAssistantApi.createFromTemplate(params);
			const da = res.data.data;
			if (!da) throw new Error("No data returned");
			const item = mapBackendDA(da);
			const visibleItem =
				item.deploymentStatus === "ready"
					? {
							...item,
							deploymentStatus: "pending",
						}
					: item;
			this.#set((state) => ({
				assistants: [visibleItem, ...state.assistants.filter((a) => a.id !== visibleItem.id)],
				activeAssistantId: visibleItem.id,
				assistantsLoaded: true,
			}));
			return visibleItem;
		} catch (err) {
			console.error("createAssistantFromTemplate error:", err);
			return null;
		}
	};

	updateAssistant = async (params: {
		id: number;
		name?: string;
		role_name?: string;
		description?: string;
		avatar?: string;
		system_prompt?: string;
		expertise?: string[];
		template_id?: number;
		source?: string;
	}) => {
		try {
			const res = await digitalAssistantApi.update(params);
			const da = res.data.data;
			if (!da) throw new Error("No data returned");
			const item = mapBackendDA(da);
			this.#set((state) => ({
				assistants: state.assistants.map((a) => (a.id === item.id ? item : a)),
			}));
			return item;
		} catch (err) {
			console.error("updateAssistant error:", err);
			return null;
		}
	};

	updateAssistantStatus = async (id: number, status: string) => {
		try {
			await digitalAssistantApi.updateStatus({ id, status });
			this.#set((state) => ({
				assistants: state.assistants.map((a) =>
					a.id === id
						? {
								...a,
								status,
								deploymentStatus: status === "active" ? "pending" : "stopped",
								deploymentError: "",
							}
						: a,
				),
			}));
			return true;
		} catch (err) {
			console.error("updateAssistantStatus error:", err);
			return false;
		}
	};

	deleteAssistant = async (id: number) => {
		try {
			await digitalAssistantApi.delete(id);
			this.#set((state) => ({
				assistants: state.assistants.filter((a) => a.id !== id),
				activeAssistantId: state.activeAssistantId === id ? null : state.activeAssistantId,
			}));
			return true;
		} catch (err) {
			console.error("deleteAssistant error:", err);
			return false;
		}
	};

	switchAssistant = (id: number) => {
		this.#set({ activeAssistantId: id });
	};

	setAssistantSearchQuery = (query: string) => {
		this.#set({ assistantSearchQuery: query });
	};
}

export const daSlice: SliceCreator<DAStore> = (...params) => ({
	..._initialState,
	...flattenActions<DigitalAssistantAction>([createDASlice(params[0] as SetState)]),
});
