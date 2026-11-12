import { useFocusEffect } from "expo-router";
import { useCallback, useMemo, useState } from "react";

import { createSecureSettingsStore } from "../settings/secure-store";
import type { Settings } from "../settings/settings";

// useSettings 在页面每次获得焦点时重读设置：从设置页返回后，
// 其他页面无需重启就能用上新地址或令牌。
export function useSettings(): { settings: Settings | null; loading: boolean; reload: () => void } {
	const store = useMemo(() => createSecureSettingsStore(), []);
	const [settings, setSettings] = useState<Settings | null>(null);
	const [loading, setLoading] = useState(true);

	const reload = useCallback(() => {
		void store
			.load()
			.then((loaded) => {
				setSettings(loaded);
			})
			.catch(() => {
				// keystore 读取失败（设备锁定、条目损坏）不能让页面永远停在
				// loading：当作未配置，让用户重新填写。
				setSettings(null);
			})
			.finally(() => {
				setLoading(false);
			});
	}, [store]);

	useFocusEffect(
		useCallback(() => {
			reload();
		}, [reload]),
	);

	return { settings, loading, reload };
}
