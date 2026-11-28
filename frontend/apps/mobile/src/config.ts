/** 深链 scheme 与推送一致；改这里必须同步 app.json。 */
export const APP_SCHEME = "lework";

// sessionRoute 返回 Expo Router 的会话路径；两段都必须编码，否则含斜杠的
// 节点名会被路由当成额外层级。
export function sessionRoute(nodeID: string, sessionID: string): string {
	return `/session/${encodeURIComponent(nodeID)}/${encodeURIComponent(sessionID)}`;
}

// projectRoute 返回项目页路径。项目 key 可能是 "global:/abs/path"，
// 必须整体编码成一个路径段。
export function projectRoute(key: string): string {
	return `/projects/${encodeURIComponent(key)}`;
}

// filesRoute 返回项目文件页路径；key 里的 ":" 与 "/" 同样要整体编码。
export function filesRoute(key: string): string {
	return `/files/${encodeURIComponent(key)}`;
}

// fileRoute 返回文件预览页路径；directory / path 走 query，避免斜杠被当成
// 路由层级（expo-router 会自动解码 query）。
export function fileRoute(nodeID: string, directory: string, path: string): string {
	return `/file/${encodeURIComponent(nodeID)}?directory=${encodeURIComponent(directory)}&path=${encodeURIComponent(path)}`;
}
