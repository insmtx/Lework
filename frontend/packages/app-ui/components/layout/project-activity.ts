import type { ProjectActivityActor, ProjectActivityItem, ProjectActivitySkill } from "@leros/store";

export type ProjectActivityTextPart =
	| { type: "text"; text: string; bold?: boolean }
	| {
			type: "actor-list";
			label: "成员" | "AI队友";
			actors: ProjectActivityActor[];
			participantType: "user" | "assistant";
	  };

function formatParticipantContent(kind: "成员" | "AI队友", items: ProjectActivityActor[]): string {
	const names = items
		.map((item) => item.name?.trim())
		.filter(Boolean)
		.join("，");
	return names ? `${kind} ${names}` : kind;
}

function addRemoveSkillParts(
	verb: string,
	items: ProjectActivitySkill[],
): ProjectActivityTextPart[] {
	const names = items
		.map((item) => item.name?.trim())
		.filter(Boolean)
		.join("，");
	const segment: ProjectActivityTextPart[] = [{ type: "text", text: `${verb} 技能 ` }];
	if (names) {
		segment.push({ type: "text", text: names, bold: true });
	}
	return segment;
}

function addRemoveMCPParts(verb: string, items: ProjectActivitySkill[]): ProjectActivityTextPart[] {
	const names = items
		.map((item) => item.name?.trim())
		.filter(Boolean)
		.join("，");
	const segment: ProjectActivityTextPart[] = [{ type: "text", text: `${verb} MCP 连接器 ` }];
	if (names) {
		segment.push({ type: "text", text: names, bold: true });
	}
	return segment;
}

function addRemoveParticipantNameParts(
	verb: string,
	label: "AI队友",
	items: ProjectActivityActor[],
): ProjectActivityTextPart[] {
	const names = items
		.map((item) => item.name?.trim())
		.filter(Boolean)
		.join("，");
	const segment: ProjectActivityTextPart[] = [{ type: "text", text: `${verb} ${label} ` }];
	if (names) {
		segment.push({ type: "text", text: names, bold: true });
	}
	return segment;
}

function addRemoveActorParts(
	verb: string,
	label: "成员" | "AI队友",
	actors: ProjectActivityActor[],
	participantType: "user" | "assistant",
): ProjectActivityTextPart[] {
	return [
		{ type: "text", text: `${verb} ` },
		{ type: "actor-list", label, actors, participantType },
	];
}

function appendActionSegment(parts: ProjectActivityTextPart[], segment: ProjectActivityTextPart[]) {
	if (parts.length > 0) {
		parts.push({ type: "text", text: "； " });
	}
	parts.push(...segment);
}

export function buildProjectActivityActionParts(
	item: ProjectActivityItem,
): ProjectActivityTextPart[] {
	const { action_type, payload } = item;
	const parts: ProjectActivityTextPart[] = [];

	if (action_type === "project.created") {
		return [{ type: "text", text: "创建了项目" }];
	}

	if (action_type === "project.skills.changed") {
		if (payload.added_skills.length > 0) {
			appendActionSegment(parts, addRemoveSkillParts("添加了", payload.added_skills));
		}
		if (payload.removed_skills.length > 0) {
			appendActionSegment(parts, addRemoveSkillParts("移除了", payload.removed_skills));
		}
		return parts.length > 0 ? parts : [{ type: "text", text: "更新了技能" }];
	}

	if (action_type === "project.mcps.changed") {
		const addedMCPs = payload.added_mcps ?? [];
		const removedMCPs = payload.removed_mcps ?? [];
		if (addedMCPs.length > 0) {
			appendActionSegment(parts, addRemoveMCPParts("添加了", addedMCPs));
		}
		if (removedMCPs.length > 0) {
			appendActionSegment(parts, addRemoveMCPParts("移除了", removedMCPs));
		}
		return parts.length > 0 ? parts : [{ type: "text", text: "更新了 MCP 连接器" }];
	}

	if (action_type === "project.participants.changed") {
		if (payload.added_members.length > 0) {
			appendActionSegment(
				parts,
				addRemoveActorParts("添加了", "成员", payload.added_members, "user"),
			);
		}
		if (payload.added_ai_teammates.length > 0) {
			appendActionSegment(
				parts,
				addRemoveParticipantNameParts("添加了", "AI队友", payload.added_ai_teammates),
			);
		}
		if (payload.removed_members.length > 0) {
			appendActionSegment(
				parts,
				addRemoveActorParts("移除了", "成员", payload.removed_members, "user"),
			);
		}
		if (payload.removed_ai_teammates.length > 0) {
			appendActionSegment(
				parts,
				addRemoveParticipantNameParts("移除了", "AI队友", payload.removed_ai_teammates),
			);
		}
		return parts.length > 0 ? parts : [{ type: "text", text: "更新了项目成员" }];
	}

	return [{ type: "text", text: "更新了项目" }];
}

export function formatProjectActivityAction(item: ProjectActivityItem): string {
	return buildProjectActivityActionParts(item)
		.map((part) => {
			if (part.type === "text") return part.text;
			return formatParticipantContent(part.label, part.actors);
		})
		.join("");
}

export function formatProjectActivityTime(isoTime: string): string {
	if (!isoTime) return "";
	const timestamp = new Date(isoTime).getTime();
	if (Number.isNaN(timestamp)) return "";

	const diffMs = Date.now() - timestamp;
	if (!Number.isFinite(diffMs) || diffMs < 0) return "";

	const minute = 60 * 1000;
	const hour = 60 * minute;
	const day = 24 * hour;

	if (diffMs < hour) return `${Math.max(1, Math.round(diffMs / minute))} 分`;
	if (diffMs < day) return `${Math.round(diffMs / hour)} 小时`;
	return `${Math.round(diffMs / day)} 天`;
}

export function formatProjectActivitySummary(item: ProjectActivityItem): string {
	return `${resolveProjectActivityOperatorName(item)} ${formatProjectActivityAction(item)}`;
}

export function resolveProjectActivityOperatorName(item: ProjectActivityItem): string {
	return item.operator?.name?.trim() || "未知用户";
}

export function resolveProjectActivityOperatorAvatar(
	item: ProjectActivityItem,
): string | undefined {
	return item.operator?.avatar_url?.trim() || undefined;
}
