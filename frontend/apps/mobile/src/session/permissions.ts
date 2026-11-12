import type { ControlEvent, PendingPermission } from "@lework/api";

function parsePermission(event: ControlEvent): PendingPermission | null {
	if (!event.data || typeof event.data !== "object") return null;
	const data = event.data as {
		id?: unknown;
		action?: unknown;
		resources?: unknown;
		message?: unknown;
	};
	if (typeof data.id !== "string" || data.id === "") return null;
	const resources = Array.isArray(data.resources)
		? data.resources.filter((item): item is string => typeof item === "string")
		: [];
	return {
		id: data.id,
		session_id: event.session_id ?? "",
		action: typeof data.action === "string" ? data.action : "",
		resources,
		message: typeof data.message === "string" ? data.message : undefined,
	};
}

function repliedID(event: ControlEvent): string | null {
	if (!event.data || typeof event.data !== "object") return null;
	const id = (event.data as { id?: unknown }).id;
	return typeof id === "string" && id !== "" ? id : null;
}

// mergePermission 把审批事件并入本地卡片列表。
//
// 列表只负责渲染；真正的审批事实是 permission_id。重复事件靠 ID 去重，
// 因此重连重放不会出现两张一样的卡。
export function mergePermission(list: PendingPermission[], event: ControlEvent): PendingPermission[] {
	switch (event.type) {
		case "permission.asked": {
			const permission = parsePermission(event);
			if (!permission) return list;
			if (list.some((item) => item.id === permission.id)) return list;
			return [...list, permission];
		}
		case "permission.replied":
		case "permission.resolved": {
			const id = repliedID(event);
			if (!id) return list;
			return list.filter((item) => item.id !== id);
		}
		default:
			return list;
	}
}
