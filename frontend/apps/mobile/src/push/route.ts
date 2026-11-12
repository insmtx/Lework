import { sessionRoute } from "../config";

// routeFromNotification 把推送负载翻译成应用内路径。
//
// 负载里只有标识符，绝不含令牌；任何字段缺失或类型不对都返回 null，
// 让调用方留在当前页面，而不是跳到一个拼错的深链上。
export function routeFromNotification(data: unknown): string | null {
	if (!data || typeof data !== "object") return null;

	const payload = data as { node?: unknown; session?: unknown; permission_id?: unknown };
	if (typeof payload.node !== "string" || payload.node === "") return null;
	if (typeof payload.session !== "string" || payload.session === "") return null;

	const base = sessionRoute(payload.node, payload.session);
	if (typeof payload.permission_id === "string" && payload.permission_id !== "") {
		return `${base}?permission=${encodeURIComponent(payload.permission_id)}`;
	}
	return base;
}
