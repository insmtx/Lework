// 安装适配器：把下载完成的 file:// URI 转成 content:// 并调起系统安装器。
// 原生模块按 ruling R3 处理：类型用 import type 引入（编译期擦除），运行时在
// async 函数内动态 import；buildInstallIntent 是纯函数，可脱离原生模块测试。
import type { IntentLauncherParams } from "expo-intent-launcher";

// FLAG_GRANT_READ_URI_PERMISSION：授予安装器读取 content:// 的临时权限；按 brief
// 约定保留字面量 1。
const FLAG_GRANT_READ_URI_PERMISSION = 1;

const INSTALL_ACTION = "android.intent.action.VIEW";
const APK_MIME_TYPE = "application/vnd.android.package-archive";

export type InstallIntent = {
	action: string;
	params: {
		data: string;
		type: string;
		flags: number;
	};
};

// buildInstallIntent 返回调起系统安装器所需的完整 intent 参数；形状与 brief 的
// 测试逐字对应，调用方不应自行拼装。
export function buildInstallIntent(contentUri: string): InstallIntent {
	return {
		action: INSTALL_ACTION,
		params: {
			data: contentUri,
			type: APK_MIME_TYPE,
			flags: FLAG_GRANT_READ_URI_PERMISSION,
		},
	};
}

// installApk 把 file:// 转成 content:// 后拉起系统安装器。安装器接管前台后本进程
// 通常被系统结束，无需监听安装结果；调用失败（含用户未授予「安装未知应用」）向上
// 抛出原始错误，由上层 dispatch failed 并提示去系统设置开启。
export async function installApk(fileUri: string): Promise<void> {
	const { getContentUriAsync } = await import("expo-file-system/legacy");
	const IntentLauncherModule = await import("expo-intent-launcher");

	const contentUri = await getContentUriAsync(fileUri);
	const intent = buildInstallIntent(contentUri);
	const params: IntentLauncherParams = intent.params;
	await IntentLauncherModule.startActivityAsync(intent.action, params);
}
