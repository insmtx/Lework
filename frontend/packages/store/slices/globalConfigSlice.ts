import { type Edition, globalConfigApi } from "../api/globalConfigApi";
import type { SliceCreator } from "../types";
import { flattenActions } from "../utils";

export type GlobalConfigState = {
	edition: Edition | "unknown";
};

export type GlobalConfigAction = Pick<GlobalConfigActionImpl, keyof GlobalConfigActionImpl>;
export type GlobalConfigStore = GlobalConfigState & GlobalConfigAction;

const _initialState: GlobalConfigState = {
	edition: "unknown",
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

			// 中文注释：服务端是 edition 的唯一来源，前端只保存合法版本值供全局策略读取。
			this.#set({ edition: result.data.edition });
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
