import { apiClient } from "./client";
import type { BackendDataResponse } from "./types";

export type Edition = "oss" | "enterprise";

export type GlobalConfig = {
	edition: Edition;
	/** 每个用户可创建的组织上限；达到后前端禁用创建组织入口。 */
	max_orgs_per_user: number;
	/** 是否开启手机号验证码登录（服务端透传；前端登录 UI 以私有化客户端标记为准） */
	phone_code_login_enabled: boolean;
};

export const globalConfigApi = {
	get: () => apiClient.get<BackendDataResponse<GlobalConfig>>("/GlobalConfig"),
};
