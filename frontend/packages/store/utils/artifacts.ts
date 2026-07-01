import type { ProjectArtifact } from "../slices/layoutSlice";
import type { MessageArtifact } from "../types/chat";

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
		storageUri: artifact.storageUri,
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
