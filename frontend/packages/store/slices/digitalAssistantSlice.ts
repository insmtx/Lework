import { digitalAssistantApi } from "../api/digitalAssistantApi";
import type { BackendDigitalAssistant } from "../api/types";
import type { SliceCreator } from "../types";
import { flattenActions } from "../utils";

export type DigitalAssistantItem = {
	id: number;
	code: string;
	name: string;
	description: string;
	avatar: string;
	status: string;
	systemPrompt: string;
	version: number;
	createdAt: number;
	updatedAt: number;
};

export type DigitalAssistantState = {
	assistants: DigitalAssistantItem[];
	assistantsLoaded: boolean;
	activeAssistantId: number | null;
	assistantSearchQuery: string;
	assistantStatusFilter: string;
};

export type DigitalAssistantAction = Pick<DASliceImpl, keyof DASliceImpl>;
export type DAStore = DigitalAssistantState & DigitalAssistantAction;

function mapBackendDA(da: BackendDigitalAssistant): DigitalAssistantItem {
	return {
		id: da.id,
		code: da.code,
		name: da.name,
		description: da.description ?? "",
		avatar: da.avatar ?? "",
		status: da.status,
		systemPrompt: da.system_prompt ?? "",
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
	assistantStatusFilter: "",
};

type SetState = (
	partial: DAStore | Partial<DAStore> | ((state: DAStore) => DAStore | Partial<DAStore>),
	replace?: boolean,
) => void;

export const createDASlice = (set: SetState) => new DASliceImpl(set);

export class DASliceImpl {
	readonly #set: SetState;

	constructor(set: SetState) {
		this.#set = set;
	}

	fetchAssistants = async () => {
		try {
			const res = await digitalAssistantApi.list({ list_all: true, limit: 100 });
			const items = res.data.data?.items ?? [];
			this.#set({
				assistants: items.map(mapBackendDA),
				assistantsLoaded: true,
			});
		} catch (err) {
			console.error("fetchAssistants error:", err);
		}
	};

	createAssistant = async (params: {
		code: string;
		name: string;
		description?: string;
		avatar?: string;
		system_prompt?: string;
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

	updateAssistant = async (params: {
		id: number;
		name?: string;
		description?: string;
		avatar?: string;
		system_prompt?: string;
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
				assistants: state.assistants.map((a) => (a.id === id ? { ...a, status } : a)),
			}));
		} catch (err) {
			console.error("updateAssistantStatus error:", err);
		}
	};

	deleteAssistant = async (id: number) => {
		try {
			await digitalAssistantApi.delete(id);
			this.#set((state) => ({
				assistants: state.assistants.filter((a) => a.id !== id),
				activeAssistantId: state.activeAssistantId === id ? null : state.activeAssistantId,
			}));
		} catch (err) {
			console.error("deleteAssistant error:", err);
		}
	};

	switchAssistant = (id: number) => {
		this.#set({ activeAssistantId: id });
	};

	setAssistantSearchQuery = (query: string) => {
		this.#set({ assistantSearchQuery: query });
	};

	setAssistantStatusFilter = (filter: string) => {
		this.#set({ assistantStatusFilter: filter });
	};
}

export const daSlice: SliceCreator<DAStore> = (...params) => ({
	..._initialState,
	...flattenActions<DigitalAssistantAction>([createDASlice(params[0] as SetState)]),
});
