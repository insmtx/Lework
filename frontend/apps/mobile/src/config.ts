/** 深链 scheme 与推送一致；改这里必须同步 app.json。 */
export const APP_SCHEME = "lework";

// sessionRoute 返回 Expo Router 的会话路径；两段都必须编码，否则含斜杠的
// 节点名会被路由当成额外层级。
export function sessionRoute(nodeID: string, sessionID: string): string {
	return `/session/${encodeURIComponent(nodeID)}/${encodeURIComponent(sessionID)}`;
}
