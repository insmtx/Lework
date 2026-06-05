import type { AuthTokenResponse } from "../api/authApi";
import type { SliceCreator } from "../types";
import { flattenActions } from "../utils";
import {
	clearStoredAuthUser,
	readStoredAuthUser,
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
			name: token.user_info.name || token.user_info.email || "Leros 用户",
			email: token.user_info.email,
			jwtToken: token.jwt_token,
			refreshToken: token.refresh_token,
			expiredAt: token.expired_at,
			uin: token.uin,
		});
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
