import type { ProjectMember } from "@lework/store";

/** 判断右侧项目队友卡片是否允许走快速删除流程。 */
export function canQuickRemoveProjectMember(
	member: ProjectMember,
	currentUserPublicId: string | undefined,
): boolean {
	// 中文注释：登录身份未补齐时统一关闭删除入口，避免误删当前用户后继续沿用旧权限。
	if (!currentUserPublicId || member.role === "owner") return false;
	return !(member.type === "user" && member.publicId === currentUserPublicId);
}
