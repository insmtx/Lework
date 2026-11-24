/**
 * 旧版桌面从 file:// 加载，localStorage 按 origin 隔离；切到回环 http 源后
 * 原来的登录态、私有化地址与侧栏偏好都在另一个 origin 里。启动时读取一次
 * 旧快照并写回新 origin，避免升级后要求用户重新登录。
 */
export function parseLocalStorageSnapshot(raw: string): Record<string, string> {
	let parsed: unknown;
	try {
		parsed = JSON.parse(raw);
	} catch {
		return {};
	}
	if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
		return {};
	}

	const entries: Record<string, string> = {};
	for (const [key, value] of Object.entries(parsed)) {
		if (typeof value === "string") entries[key] = value;
	}
	return entries;
}

/**
 * 迁移失败不能让桌面起不来：读旧数据、写新配置都可能因权限/磁盘出错，
 * 这里统一兜住并留一条日志，窗口照常创建。
 */
export async function runBestEffortMigration(
	migrate: () => Promise<void>,
	onError: (error: unknown) => void = (error) => {
		console.error("[desktop] 旧本地数据迁移失败", error);
	},
): Promise<void> {
	try {
		await migrate();
	} catch (error) {
		onError(error);
	}
}

export function buildRestoreScript(entries: Record<string, string>): string {
	return Object.entries(entries)
		.map(
			([key, value]) =>
				`localStorage.setItem(${JSON.stringify(key)}, ${JSON.stringify(value)});`,
		)
		.join("\n");
}
