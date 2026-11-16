// 版本比较逻辑与桌面端 apps/desktop/src/main/linux-update.ts 保持等价。
// 两个应用包相互独立、没有共享运行时包，这里刻意保留一份实现（见设计决策 R4），
// 修改任意一份时必须同步另一份，避免移动端与桌面端对"是否有新版本"判断不一致。

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

/** compareVersions 返回 -1 / 0 / 1；非法版本抛错，由调用方决定如何降级。 */
export function compareVersions(left: string, right: string): number {
	const leftVersion = parseVersion(left);
	const rightVersion = parseVersion(right);
	if (!leftVersion || !rightVersion) {
		throw new Error(`无法比较版本号：${left} / ${right}`);
	}

	const length = Math.max(leftVersion.main.length, rightVersion.main.length);
	for (let index = 0; index < length; index += 1) {
		const leftPart = leftVersion.main[index] ?? 0;
		const rightPart = rightVersion.main[index] ?? 0;
		if (leftPart !== rightPart) {
			return leftPart > rightPart ? 1 : -1;
		}
	}

	return comparePrerelease(leftVersion.prerelease, rightVersion.prerelease);
}

/** isVersionNewer 判断 candidate 是否比 current 更新；非法版本抛错。 */
export function isVersionNewer(candidate: string, current: string): boolean {
	return compareVersions(candidate, current) > 0;
}
