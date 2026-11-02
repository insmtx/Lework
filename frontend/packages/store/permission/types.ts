// Action 常量与 backend/types/permission.go 保持同步。
export const Action = {
	ProjectView: "project:view",
	ProjectUpdate: "project:update",
	ProjectDelete: "project:delete",
	ProjectMemberCreate: "project:member.create",
	ProjectMemberUpdate: "project:member.update",
	ProjectMemberDelete: "project:member.delete",
	ProjectMemberList: "project:member.list",
	ProjectMemberLeave: "project:member.leave",
	FileView: "file:view",
	FileDownload: "file:download",
	ArtifactView: "artifact:view",
	ArtifactDownload: "artifact:download",
	TaskCreate: "task:create",
	TaskView: "task:view",
	TaskUpdate: "task:update",
	TaskDelete: "task:delete",
	AssistantView: "assistant:view",
	AssistantUse: "assistant:use",
	AssistantUpdate: "assistant:update",
	AssistantStatusUpdate: "assistant:status.update",
	AssistantDelete: "assistant:delete",
	AssistantPermissionRead: "assistant:permission.read",
	AssistantPermissionUpdate: "assistant:permission.update",
	AssistantVisibilityUpdate: "assistant:visibility.update",
} as const;

export type Action = (typeof Action)[keyof typeof Action];

export const CODE_FORBIDDEN = 40301;

export const PERMISSION_DENIED_EVENT = "lework-permission-denied";

export type ResourceType = "project" | "task" | "file" | "artifact" | "assistant";

export type ResourceRef = {
	type: ResourceType;
	publicId: string;
};

export type PermissionDecision = {
	allowed: boolean;
	reason?: string;
	role?: string;
	inherited?: boolean;
};

export type PermissionCheckValue = boolean | "unknown";

export type BatchCheckItem = {
	action: Action;
	resource: ResourceRef;
};

export type BatchCheckResult = {
	action: Action;
	resource: ResourceRef;
	allowed: boolean;
	reason?: string;
	role?: string;
	inherited?: boolean;
};

const REASON_MESSAGES: Record<string, string> = {
	allowed: "允许",
	no_binding: "您不是该资源的成员",
	org_mismatch: "组织不匹配",
	resource_not_found: "资源不存在",
	policy_denied: "当前身份无权执行此操作",
	member_context_denied: "无法执行该成员操作",
};

export function toPermissionMessage(reason?: string): string {
	if (!reason) return "权限不足";
	return REASON_MESSAGES[reason] ?? "权限不足";
}

export function buildPermissionCacheKey(
	orgId: number,
	resource: ResourceRef,
	action: Action,
): string {
	return `${orgId}:${resource.type}:${resource.publicId}:${action}`;
}

export function dispatchPermissionDenied(message?: string) {
	if (typeof window === "undefined") return;
	window.dispatchEvent(
		new CustomEvent(PERMISSION_DENIED_EVENT, {
			detail: { message: message ?? "权限不足" },
		}),
	);
}
