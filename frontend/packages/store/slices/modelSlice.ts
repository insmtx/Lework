import { type BackendModel, type ModelRuntime, modelApi } from "../api/modelApi";
import type { SliceCreator } from "../types";
import { flattenActions } from "../utils";

export type ModelItem = {
	runtime?: ModelRuntime;
	connectionTest?: {
		state: "available" | "unavailable" | "unknown";
		observedAt: number;
		validUntil: number;
	};
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
	/** 列表加载失败时的错误信息，null 表示无错误。 */
	error: string | null;
};

export type ModelAction = Pick<ModelSliceImpl, keyof ModelSliceImpl>;
export type ModelStore = ModelState & ModelAction;

function mapBackendModel(m: BackendModel): ModelItem {
	return {
		id: m.id,
		runtime: m.runtime,
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

function modelFingerprint(m: ModelItem): string {
	return JSON.stringify([
		m.orgId,
		m.id,
		m.provider,
		m.model,
		m.baseUrl,
		m.baseUrlHasV1,
		m.updatedAt,
	]);
}

const _initialState: ModelState = {
	models: [],
	total: 0,
	loading: false,
	loaded: false,
	error: null,
};

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
	#generation = 0;
	#request = 0;

	constructor(set: SetState) {
		this.#set = set;
	}

	fetchModels = async (options?: { silent?: boolean }) => {
		const generation = this.#generation;
		const request = ++this.#request;
		if (!options?.silent) this.#set({ loading: true });
		try {
			const res = await modelApi.list({ offset: 0, limit: 200 });
			if (generation !== this.#generation || request !== this.#request) return;
			const items = res.data.data?.items ?? [];
			this.#set((state) => ({
				models: items.map((raw) => {
					const next = mapBackendModel(raw);
					const old = state.models.find((m) => m.id === next.id && m.orgId === next.orgId);
					if (old && modelFingerprint(old) === modelFingerprint(next))
						next.connectionTest = old.connectionTest;
					return next;
				}),
				total: res.data.data?.total ?? 0,
				loaded: true,
				error: null,
			}));
		} catch (err) {
			if (generation !== this.#generation || request !== this.#request) return;
			console.error("fetchModels error:", err);
			this.#set({
				error: err instanceof Error ? err.message : "加载模型列表失败",
			});
		} finally {
			if (generation === this.#generation && request === this.#request)
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
		await modelApi.setDefault(id);
		await this.fetchModels();
	};

	setStatus = async (id: number, status: string) => {
		await modelApi.setStatus(id, status);
		await this.fetchModels();
	};

	testModel = async (params: Parameters<typeof modelApi.test>[0]) => {
		const generation = this.#generation;
		let fingerprint: string | undefined;
		this.#set((state) => {
			const model = state.models.find((m) => m.id === params.id);
			if (model) fingerprint = modelFingerprint(model);
			return {};
		});
		const record = (state: "available" | "unavailable" | "unknown") => {
			if (generation !== this.#generation || fingerprint === undefined) return;
			const observedAt = Math.floor(Date.now() / 1000);
			this.#set((current) => ({
				models: current.models.map((model) =>
					model.id === params.id && modelFingerprint(model) === fingerprint
						? { ...model, connectionTest: { state, observedAt, validUntil: observedAt + 60 } }
						: model,
				),
			}));
		};
		try {
			const result = await modelApi.test(params);
			record(result.data.data?.success ? "available" : "unavailable");
			return result;
		} catch (error) {
			record("unknown");
			throw error;
		}
	};

	resetAuthScopedData = () => {
		this.#generation++;
		this.#request++;
		this.#set({ models: [], total: 0, loading: false, loaded: false, error: null });
	};
}

export const modelSlice: SliceCreator<ModelStore> = (...params) => ({
	..._initialState,
	...flattenActions<ModelAction>([createModelSlice(params[0] as SetState)]),
});
