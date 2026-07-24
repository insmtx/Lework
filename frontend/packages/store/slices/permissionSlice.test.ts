import { afterEach, describe, expect, it, vi } from "vitest";

import { permissionApi } from "../api/permissionApi";
import type { BatchCheckItem } from "../permission/types";
import { PermissionActionImpl, type PermissionState } from "./permissionSlice";

type TestState = PermissionState & {
	authUser: {
		currentOrg: { id: number };
	};
};

function createPermissionActions() {
	let state: TestState = {
		decisions: {},
		permissionRevision: 0,
		authUser: { currentOrg: { id: 1 } },
	};
	const setState = (partial: unknown) => {
		const update =
			typeof partial === "function"
				? (partial as (current: TestState) => Partial<TestState>)(state)
				: (partial as Partial<TestState>);
		state = { ...state, ...update };
	};
	const actions = new PermissionActionImpl(setState as never, (() => state) as never);
	return { actions };
}

describe("PermissionActionImpl.ensureCapabilities", () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it("同一权限处于 pending 时复用在途请求，不会重复调用接口", async () => {
		const item: BatchCheckItem = {
			action: "project:update",
			resource: { type: "project", publicId: "project-1" },
		};
		let resolveRequest: ((value: unknown) => void) | undefined;
		const request = new Promise((resolve) => {
			resolveRequest = resolve;
		});
		const batchCheck = vi.spyOn(permissionApi, "batchCheck").mockReturnValue(request as never);
		const { actions } = createPermissionActions();

		const first = actions.ensureCapabilities([item]);
		const second = actions.ensureCapabilities([item]);

		expect(batchCheck).toHaveBeenCalledTimes(1);
		resolveRequest?.({
			data: {
				code: 0,
				message: "success",
				data: [
					{
						action: item.action,
						resource: { type: item.resource.type, public_id: item.resource.publicId },
						allowed: true,
					},
				],
			},
		});
		await Promise.all([first, second]);

		expect(actions.can(item.action, item.resource)).toBe(true);
	});
});

describe("PermissionActionImpl.invalidate", () => {
	it("失效项目权限缓存时会递增版本号，通知列表重新预取权限", () => {
		const setState = vi.fn();
		const state = {
			decisions: {
				"1:project:project-1:project:update": { allowed: true },
				"1:project:project-2:project:update": { allowed: true },
			},
			permissionRevision: 3,
			authUser: { currentOrg: { id: 1 } },
		};
		const actions = new PermissionActionImpl(setState, (() => state) as never);

		actions.invalidate({ type: "project", publicId: "project-1" });

		const update = setState.mock.calls[0]?.[0] as (current: typeof state) => typeof state;
		expect(update(state)).toEqual({
			decisions: {
				"1:project:project-2:project:update": { allowed: true },
			},
			permissionRevision: 4,
		});
	});
});
