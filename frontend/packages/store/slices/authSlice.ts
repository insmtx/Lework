import {
	type AuthOrgInfo,
	type AuthSessionResponse,
	type AuthTokenResponse,
	authApi,
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
		this.setAuthUser({
			publicId: token.user_info.public_id,
			name: token.user_info.name || token.user_info.phone || token.user_info.email || "Lework 用户",
			email: token.user_info.email,
			phone: token.user_info.phone,
			avatarUrl: token.user_info.avatar_url,
			jwtToken: token.jwt_token,
			refreshToken: token.refresh_token,
			expiredAt: token.expired_at,
			uin: token.uin,
			currentOrg: toStoredAuthOrg(token.org),
			organizations: toStoredAuthOrgs(token.organizations),
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
		try {
			const response = await authApi.authSession();
			const result = response.data;
			if (result.code !== 0) return false;
			this.setAuthSession(result.data);
			return true;
		} catch (error) {
			console.error("refresh auth session error:", error);
			return false;
		}
	};

	switchOrganization = async (orgId: number) => {
		try {
			const response = await authApi.switchOrganization(orgId);
			const result = response.data;
			if (result.code !== 0) {
				throw new Error(result.message || "切换组织失败");
			}
			this.setAuthToken(result.data);
			return result.data;
		} catch (error) {
			console.error("switch organization error:", error);
			throw error;
		}
	};

	createOrganization = async (name: string) => {
		try {
			const response = await authApi.createOrganization({ name });
			const result = response.data;
			if (result.code !== 0) {
				throw new Error(result.message || "创建组织失败");
			}
			this.setAuthToken(result.data);
			return result.data;
		} catch (error) {
			console.error("create organization error:", error);
			throw error;
		}
	};

	logout = () => {
		this.setAuthUser(null);
	};
}

export const createAuthSlice = (set: SetState) => new AuthActionImpl(set);

export const authSlice: SliceCreator<AuthStore> = (...params) => ({
	..._initialState,
	...flattenActions<AuthAction>([createAuthSlice(params[0] as SetState)]),
});

function toStoredAuthOrg(org: AuthOrgInfo | undefined): StoredAuthOrg | undefined {
	if (!org) return undefined;
	return {
		id: org.id,
		publicId: org.public_id,
		code: org.code,
		name: org.name,
		logo: org.logo,
		isDefault: org.is_default,
	};
}

function toStoredAuthOrgs(orgs: AuthOrgInfo[] | undefined): StoredAuthOrg[] | undefined {
	return orgs?.map(toStoredAuthOrg).filter((org): org is StoredAuthOrg => Boolean(org));
}
