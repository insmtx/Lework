import type { ProjectMember } from "@lework/store";

export type CurrentUserProjectProfile = {
	publicId?: string;
	uin?: number;
	userId?: number;
	name?: string;
	uinName?: string;
	avatarUrl?: string;
};

function isSameCurrentUserMember(
	member: ProjectMember,
	currentUser: CurrentUserProjectProfile,
): boolean {
	if (member.type !== "user") return false;
	const publicId = currentUser.publicId?.trim();
	if (publicId && member.publicId === publicId) return true;
	// 中文注释：真人成员的 memberId 存的是 uin，登录态 publicId 未补齐时用它识别本人。
	if (currentUser.uin && currentUser.uin !== 0 && member.memberId === currentUser.uin) return true;
	return false;
}

/** 用当前登录用户的最新资料覆盖项目队友快照里的本人名称/头像。 */
export function applyCurrentUserProfileToProjectMembers(
	members: ProjectMember[],
	currentUser: CurrentUserProjectProfile | null | undefined,
): ProjectMember[] {
	if (!currentUser) return members;
	const nextName = currentUser.uinName?.trim() || currentUser.name?.trim();
	const nextAvatarUrl = currentUser.avatarUrl?.trim();
	if (!nextName && !nextAvatarUrl) return members;

	return members.map((member) => {
		if (!isSameCurrentUserMember(member, currentUser)) return member;
		return {
			...member,
			name: nextName || member.name,
			avatarUrl: nextAvatarUrl || member.avatarUrl,
		};
	});
}
