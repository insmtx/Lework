import { type BackendModel, modelApi } from "../api/modelApi";
import type { SliceCreator } from "../types";
import { flattenActions } from "../utils";

export type ModelItem = {
	id: number;
	orgId: number;
	code: string;
	name: string;
	description: string;
	provider: string;
	model: string;
	baseUrl: string;
	baseUrlHasV1: boolean;
	apiKeyMasked: string;
	maxTokens: number;
	temperature: number;
	status: string;
	purpose?: string;
	isDefault: boolean;
	isSystem: boolean;
	config?: Record<string, unknown>;
	createdAt: number;
	updatedAt: number;
};

export type ModelState = {
	models: ModelItem[];
	total: number;
	loading: boolean;
	loaded: boolean;
};

export type ModelAction = Pick<ModelSliceImpl, keyof ModelSliceImpl>;
export type ModelStore = ModelState & ModelAction;

function mapBackendModel(m: BackendModel): ModelItem {
	return {
		id: m.id,
		orgId: m.org_id,
		code: m.code,
		name: m.name,
		description: m.description ?? "",
		provider: m.provider,
		model: m.model,
		baseUrl: m.base_url,
		baseUrlHasV1: m.base_url_has_v1,
		apiKeyMasked: m.api_key,
		maxTokens: m.max_tokens,
		temperature: m.temperature,
		status: m.status,
		purpose: m.purpose,
		isDefault: m.is_default,
		isSystem: m.is_system,
		config: m.config,
		createdAt: new Date(m.created_at).getTime(),
		updatedAt: new Date(m.updated_at).getTime(),
	};
}

const _initialState: ModelState = { models: [], total: 0, loading: false, loaded: false };

type SetState = (
	partial:
		| ModelStore
		| Partial<ModelStore>
		| ((state: ModelStore) => ModelStore | Partial<ModelStore>),
	replace?: boolean,
) => void;

export const createModelSlice = (set: SetState) => new ModelSliceImpl(set);

export class ModelSliceImpl {
	readonly #set: SetState;

	constructor(set: SetState) {
		this.#set = set;
	}

	fetchModels = async () => {
		this.#set({ loading: true });
		try {
			const res = await modelApi.list({ offset: 0, limit: 200 });
			const items = res.data.data?.items ?? [];
			this.#set({
				models: items.map(mapBackendModel),
				total: res.data.data?.total ?? 0,
				loaded: true,
			});
		} catch (err) {
			console.error("fetchModels error:", err);
		} finally {
			this.#set({ loading: false });
		}
	};

	createModel = async (params: Parameters<typeof modelApi.create>[0]) => {
		const res = await modelApi.create(params);
		if (!res.data.data) throw new Error("create model failed");
		await this.fetchModels();
		return mapBackendModel(res.data.data);
	};

	updateModel = async (params: Parameters<typeof modelApi.update>[0]) => {
		const res = await modelApi.update(params);
		if (!res.data.data) throw new Error("update model failed");
		await this.fetchModels();
		return mapBackendModel(res.data.data);
	};

	deleteModel = async (id: number) => {
		await modelApi.delete(id);
		await this.fetchModels();
	};

	setDefault = async (id: number) => {
		await modelApi.update({ id, is_default: true });
		await this.fetchModels();
	};

	setStatus = async (id: number, status: string) => {
		await modelApi.setStatus(id, status);
		await this.fetchModels();
	};

	testModel = (params: Parameters<typeof modelApi.test>[0]) => modelApi.test(params);

	resetAuthScopedData = () => {
		this.#set({ models: [], total: 0, loading: false, loaded: false });
	};
}

export const modelSlice: SliceCreator<ModelStore> = (...params) => ({
	..._initialState,
	...flattenActions<ModelAction>([createModelSlice(params[0] as SetState)]),
});
