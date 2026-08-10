import { isPrivateDeployment } from "../api/config";
import { type Edition, globalConfigApi } from "../api/globalConfigApi";
import type { SliceCreator } from "../types";
import { flattenActions } from "../utils";

export type GlobalConfigState = {
	edition: Edition | "unknown";
	/** null 表示尚未拉到 GlobalConfig，不做数量上限判断。 */
	maxOrgsPerUser: number | null;
	/**
	 * 是否开启手机号验证码登录。
	 * 公有云默认 true；私有化客户端固定为 false（仅账号密码登录）。
	 */
	phoneCodeLoginEnabled: boolean;
};

export type GlobalConfigAction = Pick<GlobalConfigActionImpl, keyof GlobalConfigActionImpl>;
export type GlobalConfigStore = GlobalConfigState & GlobalConfigAction;

const _initialState: GlobalConfigState = {
	edition: "unknown",
	maxOrgsPerUser: null,
	// 中文注释：私有化默认账号密码登录；GlobalConfig 拉不到时也不应回退出验证码登录。
	phoneCodeLoginEnabled: !isPrivateDeployment,
};

type SetState = (
	partial:
		| GlobalConfigStore
		| Partial<GlobalConfigStore>
		| ((state: GlobalConfigStore) => GlobalConfigStore | Partial<GlobalConfigStore>),
	replace?: boolean,
) => void;

export class GlobalConfigActionImpl {
	readonly #set: SetState;
	#fetchPromise: Promise<boolean> | null = null;

	constructor(set: SetState) {
		this.#set = set;
	}

	fetchGlobalConfig = async (): Promise<boolean> => {
		if (this.#fetchPromise) return this.#fetchPromise;

		this.#fetchPromise = this.#requestGlobalConfig().finally(() => {
			this.#fetchPromise = null;
		});
		return this.#fetchPromise;
	};

	#requestGlobalConfig = async (): Promise<boolean> => {
		try {
			const response = await globalConfigApi.get();
			const result = response.data;
			if (result.code !== 0 || !result.data?.edition) return false;

			const maxOrgsPerUser = result.data.max_orgs_per_user;
			// 中文注释：edition / 组织上限以服务端为准；私有化客户端不展示验证码登录，忽略服务端该开关。
			this.#set({
				edition: result.data.edition,
				maxOrgsPerUser:
					typeof maxOrgsPerUser === "number" && Number.isFinite(maxOrgsPerUser)
						? maxOrgsPerUser
						: null,
				phoneCodeLoginEnabled: isPrivateDeployment
					? false
					: result.data.phone_code_login_enabled !== false,
			});
			return true;
		} catch (error) {
			console.error("fetch global config error:", error);
			return false;
		}
	};
}

export const globalConfigSlice: SliceCreator<GlobalConfigStore> = (...params) => ({
	..._initialState,
	...flattenActions<GlobalConfigAction>([new GlobalConfigActionImpl(params[0] as SetState)]),
});
