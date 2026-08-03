import {
	type AuthOrgInfo,
	type AuthSessionResponse,
	type AuthTokenResponse,
	authApi,
	type SwitchOrganizationResponse,
} from "../api/authApi";
import type { SliceCreator } from "../types";
import { flattenActions } from "../utils";
import {
	clearStoredAuthUser,
	readStoredAuthUser,
	type StoredAuthOrg,
	type StoredAuthUser,
	writeStoredAuthUser,
} from "../utils/authStorage";

export type AuthUser = StoredAuthUser;

export type AuthState = {
	authUser: AuthUser | null;
};

export type AuthAction = Pick<AuthActionImpl, keyof AuthActionImpl>;
export type AuthStore = AuthState & AuthAction;

const _initialState: AuthState = {
	authUser: readStoredAuthUser(),
};

type SetState = (
	partial: AuthStore | Partial<AuthStore> | ((state: AuthStore) => AuthStore | Partial<AuthStore>),
	replace?: boolean,
) => void;

export class AuthActionImpl {
	readonly #set: SetState;
	#refreshAuthSessionPromise: Promise<boolean> | null = null;
	#authContextVersion = 0;

	constructor(set: SetState) {
		this.#set = set;
	}

	setAuthUser = (user: AuthUser | null) => {
		if (user) {
			writeStoredAuthUser(user);
		} else {
			clearStoredAuthUser();
		}
		this.#set({ authUser: user });
	};

	setAuthToken = (token: AuthTokenResponse) => {
		this.#authContextVersion += 1;
		this.#set((state) => {
			const previousUser = state.authUser;
			const nextUser: AuthUser = {
				publicId: token.user_info.public_id,
				name:
					token.user_info.name || token.user_info.phone || token.user_info.email || "Lework 用户",
				uinName: token.user_info.uin_name,
				email: token.user_info.email,
				phone: token.user_info.phone,
				avatarUrl: token.user_info.avatar_url,
				jwtToken: token.jwt_token,
				refreshToken: token.refresh_token,
				expiredAt: token.expired_at,
				uin: token.uin,
				userId: token.user_id,
				loginWay: token.login_way ?? previousUser?.loginWay,
				currentOrg: toStoredAuthOrg(token.org),
				organizations: toStoredAuthOrgs(token.organizations),
			};
			writeStoredAuthUser(nextUser);
			return { authUser: nextUser };
		});
	};

	setAuthSession = (session: AuthSessionResponse) => {
		this.#set((state) => {
			if (!state.authUser) return {};
			const nextUser: AuthUser = {
				...state.authUser,
				publicId: session.user_info.public_id || state.authUser.publicId,
				name:
					session.user_info.name ||
					session.user_info.phone ||
					session.user_info.email ||
					state.authUser.name,
				uinName: session.user_info.uin_name || state.authUser.uinName,
				email: session.user_info.email || state.authUser.email,
				phone: session.user_info.phone || state.authUser.phone,
				avatarUrl: session.user_info.avatar_url || state.authUser.avatarUrl,
				currentOrg: toStoredAuthOrg(session.org),
				organizations: toStoredAuthOrgs(session.organizations),
			};
			writeStoredAuthUser(nextUser);
			return { authUser: nextUser };
		});
	};

	refreshAuthSession = async () => {
		if (this.#refreshAuthSessionPromise) {
			return this.#refreshAuthSessionPromise;
		}

		this.#refreshAuthSessionPromise = this.#fetchAuthSession().finally(() => {
			this.#refreshAuthSessionPromise = null;
		});
		return this.#refreshAuthSessionPromise;
	};

	#fetchAuthSession = async (): Promise<boolean> => {
		const authContextVersion = this.#authContextVersion;
		try {
			const response = await authApi.authSession();
			const result = response.data;
			if (result.code !== 0) return false;
			// 中文注释：组织切换后忽略旧会话响应，防止旧组织状态覆盖新 Token。
			if (authContextVersion !== this.#authContextVersion) return false;
			this.setAuthSession(result.data);
			return true;
		} catch (error) {
			console.error("refresh auth session error:", error);
			return false;
		}
	};

	syncOrganizationProfile = (orgId: number, profile: { name: string; logo?: string }) => {
		this.#set((state) => {
			if (!state.authUser) return {};
			const user = state.authUser;
			const nextUser: AuthUser = {
				...user,
				currentOrg:
					user.currentOrg?.id === orgId
						? { ...user.currentOrg, name: profile.name, logo: profile.logo }
						: user.currentOrg,
				organizations: user.organizations?.map((item) =>
					item.id === orgId ? { ...item, name: profile.name, logo: profile.logo } : item,
				),
			};
			writeStoredAuthUser(nextUser);
			return { authUser: nextUser };
		});
	};

	switchOrganization = async (uin: number) => {
		try {
			this.#authContextVersion += 1;
			const response = await authApi.switchOrganization(uin);
			const result = response.data;
			if (result.code !== 0) {
				throw new Error(result.message || "切换组织失败");
			}
			this.#setSwitchOrganizationToken(result.data, uin);
			// 中文注释：切换接口不再返回组织资料，必须用新 JWT 单独拉取最新会话，且不能复用切换前仍在途的请求。
			if (!(await this.#fetchAuthSession())) {
				throw new Error("切换组织后刷新会话失败");
			}
			return result.data;
		} catch (error) {
			console.error("switch organization error:", error);
			throw error;
		}
	};

	#setSwitchOrganizationToken = (token: SwitchOrganizationResponse, uin: number) => {
		this.#set((state) => {
			if (!state.authUser) return {};
			const nextUser: AuthUser = {
				...state.authUser,
				jwtToken: token.jwt_token,
				uin,
			};
			writeStoredAuthUser(nextUser);
			return { authUser: nextUser };
		});
	};

	createOrganization = async (name: string, userDisplayName: string) => {
		try {
			this.#authContextVersion += 1;
			const authUser = readStoredAuthUser();
			if (!authUser?.userId) {
				throw new Error("当前登录用户信息缺失");
			}
			// 中文注释：创建组织需要携带当前用户 ID 和昵称，数据取自已登录会话。
			const response = await authApi.createOrganization({
				name,
				user_id: authUser.userId,
				// 中文注释：昵称由创建组织表单提供，避免使用可能为空或过期的会话名称。
				user_display_name: userDisplayName,
			});
			const result = response.data;
			if (result.code !== 0) {
				throw new Error(result.message || "创建组织失败");
			}

			return await this.switchOrganization(result.data.uin);
		} catch (error) {
			console.error("create organization error:", error);
			throw error;
		}
	};

	logout = () => {
		this.#authContextVersion += 1;
		this.setAuthUser(null);
	};
}

export const createAuthSlice = (set: SetState) => new AuthActionImpl(set);

export const authSlice: SliceCreator<AuthStore> = (...params) => ({
	..._initialState,
	...flattenActions<AuthAction>([createAuthSlice(params[0] as SetState)]),
});

function toStoredAuthOrg(org: AuthOrgInfo): StoredAuthOrg {
	return {
		id: org.id,
		uin: org.uin,
		publicId: org.public_id,
		code: org.code,
		name: org.name,
		logo: org.logo,
		isDefault: org.is_default,
		createdByUin: org.created_by_uin,
		createdByUserId: org.created_by_user_id,
	};
}

function toStoredAuthOrgs(orgs: AuthOrgInfo[] | undefined): StoredAuthOrg[] | undefined {
	return orgs?.map(toStoredAuthOrg).filter((org): org is StoredAuthOrg => Boolean(org));
}
