import { readServerBaseURL } from "@lework/store";

/**
 * 私有化地址保存后交给主进程切换 /v1 上游，再刷新渲染层。
 *
 * 渲染层自己无法从 localStorage 改代理目标；不等 IPC 完成就刷新会让新地址
 * 晚一个请求生效，所以先等 setApiBaseURL 落盘。
 */
export function reloadDesktopRenderer() {
	const bridge = window.leworkDesktop;
	if (!bridge) {
		window.location.reload();
		return;
	}

	void bridge
		.setApiBaseURL(readServerBaseURL() ?? "")
		.catch(() => undefined)
		.finally(() => window.location.reload());
}
