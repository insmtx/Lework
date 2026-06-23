import type { ProjectArtifact } from "../slices/layoutSlice";
import type { Message, MessageArtifact } from "../types/chat";

/** Converts a message-scoped artifact into the project artifact shape used by UI panels. */
export function messageArtifactToProjectArtifact(artifact: MessageArtifact): ProjectArtifact {
	return {
		id: artifact.id,
		name: artifact.name,
		title: artifact.title,
		description: artifact.description,
		type: artifact.type,
		artifactType: artifact.artifactType,
		mimeType: artifact.mimeType,
		size: artifact.size,
		updatedAt: artifact.updatedAt,
		downloadUrl: artifact.downloadUrl,
		sha256: artifact.sha256,
	};
}

function artifactTimestamp(artifact: Pick<ProjectArtifact, "updatedAt">): number {
	return artifact.updatedAt ?? 0;
}

export function sortProjectArtifactsByNewestFirst<
	T extends Pick<ProjectArtifact, "id" | "updatedAt">,
>(artifacts: T[]): T[] {
	return [...artifacts].sort((left, right) => {
		const timeDiff = artifactTimestamp(right) - artifactTimestamp(left);
		if (timeDiff !== 0) return timeDiff;
		return right.id.localeCompare(left.id);
	});
}

/**
 * Merges session message artifacts with task API artifacts.
 * Task API records enrich message artifacts when both exist for the same id.
 */
export function mergeProjectArtifacts(
	taskArtifacts: ProjectArtifact[],
	sessionArtifacts: ProjectArtifact[],
): ProjectArtifact[] {
	const merged = new Map<string, ProjectArtifact>();
	for (const artifact of sessionArtifacts) {
		merged.set(artifact.id, artifact);
	}
	for (const artifact of taskArtifacts) {
		const existing = merged.get(artifact.id);
		merged.set(artifact.id, existing ? { ...existing, ...artifact } : artifact);
	}
	// 中文注释：任务接口与会话消息的文件合并后，统一在这里做“最新优先”排序，避免页面各自处理。
	return sortProjectArtifactsByNewestFirst([...merged.values()]);
}

/** Collects declared artifacts from assistant messages in one session. */
export function collectSessionArtifacts(
	messagesMap: Record<string, Message>,
	messageIds: string[],
	sessionId: string | null | undefined,
): ProjectArtifact[] {
	if (!sessionId) return [];

	const merged = new Map<string, ProjectArtifact>();
	for (const id of messageIds) {
		const message = messagesMap[id];
		if (
			!message ||
			message.conversationId !== sessionId ||
			message.role !== "assistant" ||
			!message.artifacts?.length
		) {
			continue;
		}
		for (const artifact of message.artifacts) {
			// 中文注释：流式消息里的 artifact 事件没有独立时间时，先复用所属消息时间作为排序与展示兜底。
			merged.set(artifact.id, {
				...messageArtifactToProjectArtifact(artifact),
				updatedAt: artifact.updatedAt ?? message.timestamp,
			});
		}
	}
	return sortProjectArtifactsByNewestFirst([...merged.values()]);
}
