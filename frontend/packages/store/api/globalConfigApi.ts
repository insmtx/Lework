import { apiClient } from "./client";
import type { BackendDataResponse } from "./types";

export type Edition = "oss" | "enterprise";

export type GlobalConfig = {
	edition: Edition;
	/** 每个用户可创建的组织上限；达到后前端禁用创建组织入口。 */
	max_orgs_per_user: number;
	/** 是否开启手机号验证码登录；私有化客户端会忽略该字段，固定走账号密码登录 */
	phone_code_login_enabled: boolean;
};

export const globalConfigApi = {
	get: () => apiClient.get<BackendDataResponse<GlobalConfig>>("/GlobalConfig"),
};
