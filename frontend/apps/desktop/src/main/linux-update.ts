export type LinuxUpdateMetadata = {
	version: string;
	releaseDate?: string;
};

function readYamlScalar(source: string, key: string): string | undefined {
	const match = source.match(new RegExp(`^${key}:\\s*(.+?)\\s*$`, "m"));
	if (!match?.[1]) {
		return undefined;
	}

	const value = match[1].trim();
	if (
		(value.startsWith('"') && value.endsWith('"')) ||
		(value.startsWith("'") && value.endsWith("'"))
	) {
		return value.slice(1, -1);
	}

	return value;
}

export function parseLinuxUpdateMetadata(source: string): LinuxUpdateMetadata {
	const version = readYamlScalar(source, "version");
	if (!version) {
		throw new Error("Linux 更新元数据缺少 version");
	}

	return {
		version,
		releaseDate: readYamlScalar(source, "releaseDate"),
	};
}

type ParsedVersion = {
	main: number[];
	prerelease: string[];
};

function parseVersion(value: string): ParsedVersion | null {
	const match = value
		.trim()
		.match(/^v?(\d+(?:\.\d+)*)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$/);
	if (!match) {
		return null;
	}

	return {
		main: match[1].split(".").map(Number),
		prerelease: match[2]?.split(".") ?? [],
	};
}

function comparePrerelease(left: string[], right: string[]): number {
	if (left.length === 0 || right.length === 0) {
		if (left.length === right.length) return 0;
		return left.length === 0 ? 1 : -1;
	}

	const length = Math.max(left.length, right.length);
	for (let index = 0; index < length; index += 1) {
		const leftValue = left[index];
		const rightValue = right[index];
		if (leftValue === undefined || rightValue === undefined) {
			if (leftValue === rightValue) return 0;
			return leftValue === undefined ? -1 : 1;
		}
		if (leftValue === rightValue) continue;

		const leftNumber = /^\d+$/.test(leftValue) ? Number(leftValue) : null;
		const rightNumber = /^\d+$/.test(rightValue) ? Number(rightValue) : null;
		if (leftNumber !== null && rightNumber !== null) {
			return leftNumber > rightNumber ? 1 : -1;
		}
		if (leftNumber !== null || rightNumber !== null) {
			return leftNumber !== null ? -1 : 1;
		}
		return leftValue > rightValue ? 1 : -1;
	}

	return 0;
}

export function isVersionNewer(candidate: string, current: string): boolean {
	const candidateVersion = parseVersion(candidate);
	const currentVersion = parseVersion(current);
	if (!candidateVersion || !currentVersion) {
		throw new Error(`无法比较版本号：${candidate} / ${current}`);
	}

	const length = Math.max(candidateVersion.main.length, currentVersion.main.length);
	for (let index = 0; index < length; index += 1) {
		const candidatePart = candidateVersion.main[index] ?? 0;
		const currentPart = currentVersion.main[index] ?? 0;
		if (candidatePart === currentPart) continue;
		return candidatePart > currentPart;
	}

	return comparePrerelease(candidateVersion.prerelease, currentVersion.prerelease) > 0;
}
