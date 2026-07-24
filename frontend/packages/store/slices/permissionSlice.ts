import { mapBatchCheckResults, permissionApi } from "../api/permissionApi";
import { decisionFromBatchResult } from "../permission/errors";
import {
	type Action,
	type BatchCheckItem,
	buildPermissionCacheKey,
	type PermissionCheckValue,
	type PermissionDecision,
	type ResourceRef,
} from "../permission/types";
import type { SliceCreator } from "../types";
import { flattenActions } from "../utils";
import type { AuthUser } from "./authSlice";

export type PermissionState = {
	decisions: Record<string, PermissionDecision | "pending">;
	// 中文注释：权限缓存失效后通知依赖方重新预取，避免入口长期停留在 unknown 状态。
	permissionRevision: number;
};

export type PermissionAction = Pick<PermissionActionImpl, keyof PermissionActionImpl>;
export type PermissionStore = PermissionState & PermissionAction;

const _initialState: PermissionState = {
	decisions: {},
	permissionRevision: 0,
};

type SetState = (
	partial:
		| PermissionStore
		| Partial<PermissionStore>
		| ((state: PermissionStore) => PermissionStore | Partial<PermissionStore>),
	replace?: boolean,
) => void;

type GetState = () => PermissionStore;

function dedupeBatchItems(items: BatchCheckItem[]): BatchCheckItem[] {
	const seen = new Set<string>();
	const result: BatchCheckItem[] = [];
	for (const item of items) {
		const key = `${item.resource.type}:${item.resource.publicId}:${item.action}`;
		if (seen.has(key)) continue;
		seen.add(key);
		result.push(item);
	}
	return result;
}

export class PermissionActionImpl {
	readonly #set: SetState;
	readonly #get: GetState;

	constructor(set: SetState, get: GetState) {
		this.#set = set;
		this.#get = get;
	}

	can = (action: Action, resource: ResourceRef | null | undefined): PermissionCheckValue => {
		if (!resource?.publicId) return false;
		const orgId = this.#getOrgId();
		if (!orgId) return false;
		const key = buildPermissionCacheKey(orgId, resource, action);
		const cached = this.#get().decisions[key];
		if (cached === "pending") return "unknown";
		if (!cached) return "unknown";
		return cached.allowed;
	};

	ensureCapabilities = async (items: BatchCheckItem[]): Promise<void> => {
		const orgId = this.#getOrgId();
		if (!orgId || items.length === 0) return;

		const deduped = dedupeBatchItems(items).filter((item) => Boolean(item.resource.publicId));
		if (deduped.length === 0) return;

		const pendingKeys: string[] = [];
		const toFetch: BatchCheckItem[] = [];
		const state = this.#get();

		for (const item of deduped) {
			const key = buildPermissionCacheKey(orgId, item.resource, item.action);
			// 中文注释：pending 表示同一权限已在请求中，必须与已完成结果一样复用，避免并发重复查询。
			if (state.decisions[key]) {
				continue;
			}
			pendingKeys.push(key);
			toFetch.push(item);
		}

		if (toFetch.length === 0) return;

		this.#set((current) => {
			const next = { ...current.decisions };
			for (const key of pendingKeys) {
				next[key] = "pending";
			}
			return { decisions: next };
		});

		try {
			const response = await permissionApi.batchCheck(toFetch);
			const payload = response.data;
			if (payload.code !== 0) {
				throw new Error(payload.message || "batch permission check failed");
			}
			const results = mapBatchCheckResults(toFetch, payload.data ?? []);
			this.#set((current) => {
				const next = { ...current.decisions };
				for (let i = 0; i < toFetch.length; i++) {
					const item = toFetch[i];
					if (!item) continue;
					const key = buildPermissionCacheKey(orgId, item.resource, item.action);
					next[key] = decisionFromBatchResult(results[i] ?? { allowed: false });
				}
				return { decisions: next };
			});
		} catch (error) {
			console.error("ensureCapabilities error:", error);
			this.#set((current) => {
				const next = { ...current.decisions };
				for (const key of pendingKeys) {
					delete next[key];
				}
				return { decisions: next };
			});
		}
	};

	invalidate = (resource?: ResourceRef): void => {
		const orgId = this.#getOrgId();
		if (!orgId) {
			this.#set((current) => ({
				decisions: {},
				permissionRevision: current.permissionRevision + 1,
			}));
			return;
		}
		if (!resource?.publicId) {
			this.#set((current) => ({
				decisions: {},
				permissionRevision: current.permissionRevision + 1,
			}));
			return;
		}
		const prefix = `${orgId}:${resource.type}:${resource.publicId}:`;
		this.#set((current) => {
			const next: PermissionState["decisions"] = {};
			for (const [key, value] of Object.entries(current.decisions)) {
				if (!key.startsWith(prefix)) {
					next[key] = value;
				}
			}
			return {
				decisions: next,
				permissionRevision: current.permissionRevision + 1,
			};
		});
	};

	invalidateAll = (): void => {
		this.#set((current) => ({
			decisions: {},
			permissionRevision: current.permissionRevision + 1,
		}));
	};

	#getOrgId(): number | null {
		const authUser = (this.#get() as PermissionStore & { authUser?: AuthUser | null }).authUser;
		const orgId = authUser?.currentOrg?.id;
		return typeof orgId === "number" && orgId > 0 ? orgId : null;
	}
}

export const createPermissionSlice = (set: SetState, get: GetState) =>
	new PermissionActionImpl(set, get);

export const permissionSlice: SliceCreator<PermissionStore> = (...params) => ({
	..._initialState,
	...flattenActions<PermissionAction>([
		createPermissionSlice(params[0] as SetState, params[1] as GetState),
	]),
});

export const PROJECT_PAGE_ACTIONS = [
	"project:update",
	"project:delete",
	"project:member.create",
	"project:member.update",
	"project:member.delete",
	"task:create",
] as const satisfies readonly Action[];

export function buildProjectCapabilityItems(projectPublicId: string): BatchCheckItem[] {
	return PROJECT_PAGE_ACTIONS.map((action) => ({
		action,
		resource: { type: "project", publicId: projectPublicId },
	}));
}

export function buildTaskCapabilityItems(taskPublicId: string): BatchCheckItem[] {
	return (["task:view", "task:update", "task:delete"] as const).map((action) => ({
		action,
		resource: { type: "task", publicId: taskPublicId },
	}));
}
