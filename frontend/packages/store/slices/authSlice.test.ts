import { afterEach, describe, expect, it, vi } from "vitest";

import { authApi } from "../api/authApi";
import { AuthActionImpl, type AuthStore } from "./authSlice";

function createAuthActions() {
	let state: Partial<AuthStore> = {
		authUser: {
			publicId: "user-1",
			name: "测试用户",
			email: "test@example.com",
			jwtToken: "old-token",
			refreshToken: "old-refresh-token",
			expiredAt: 1,
			uin: 1,
			currentOrg: { id: 1, uin: 10001, publicId: "org-1", code: "org-1", name: "旧组织" },
			organizations: [],
		},
	};
	const setState = (partial: unknown) => {
		const update =
			typeof partial === "function"
				? (partial as (current: Partial<AuthStore>) => Partial<AuthStore>)(state)
				: (partial as Partial<AuthStore>);
		state = { ...state, ...update };
	};
	return { actions: new AuthActionImpl(setState as never), getState: () => state };
}

describe("AuthActionImpl", () => {
	afterEach(() => {
		vi.restoreAllMocks();
		localStorage.clear();
	});

	it("组织切换后忽略仍在途的旧 AuthSession 响应", async () => {
		let resolveSession: ((value: unknown) => void) | undefined;
		vi.spyOn(authApi, "authSession")
			.mockReturnValueOnce(
				new Promise((resolve) => {
					resolveSession = resolve;
				}) as never,
			)
			.mockResolvedValueOnce({
				data: {
					code: 0,
					message: "success",
					data: {
						user_info: {
							id: 1,
							public_id: "user-1",
							name: "测试用户",
							email: "test@example.com",
						},
						org: { id: 2, uin: 20002, public_id: "org-2", code: "org-2", name: "AI冲锋队" },
						organizations: [],
					},
				},
			} as never);
		vi.spyOn(authApi, "switchOrganization").mockResolvedValue({
			data: {
				code: 0,
				message: "success",
				data: {
					login_status: "success",
					jwt_token: "new-token",
				},
			},
		} as never);
		const { actions, getState } = createAuthActions();

		const sessionRefresh = actions.refreshAuthSession();
		const switchOrganization = actions.switchOrganization(20002);
		resolveSession?.({
			data: {
				code: 0,
				message: "success",
				data: {
					user_info: {
						id: 1,
						public_id: "user-1",
						name: "测试用户",
						email: "test@example.com",
					},
					org: { id: 1, public_id: "org-1", code: "org-1", name: "旧组织" },
					organizations: [],
				},
			},
		});
		await Promise.all([sessionRefresh, switchOrganization]);

		expect(getState().authUser?.currentOrg?.id).toBe(2);
		expect(getState().authUser?.currentOrg?.uin).toBe(20002);
	});
});
