import { router, useLocalSearchParams } from "expo-router";
import { useEffect, useMemo, useRef, useState } from "react";
import { ActivityIndicator, Pressable, StyleSheet, Text, View } from "react-native";

import { createClient } from "../src/api/client";
import { buildPairPayload } from "../src/pairing/link";
import { connectToControlPlane } from "../src/settings/connect";
import { createSecureSettingsStore } from "../src/settings/secure-store";
import type { Theme } from "../src/theme/theme";
import { useTheme, useThemedStyles } from "../src/theme/use-theme";
import { useAppShell } from "../src/ui/app-shell-context";

function firstParam(value: string | string[] | undefined): string {
	if (Array.isArray(value)) return value[0] ?? "";
	return value ?? "";
}

/** 配对：接收 serve 打印的深链，验证通过后直接进入首页。 */
export default function ConnectScreen() {
	const params = useLocalSearchParams<{ url?: string; token?: string }>();
	const theme = useTheme();
	const styles = useThemedStyles(createStyles);
	const store = useMemo(() => createSecureSettingsStore(), []);
	const { reloadSettings } = useAppShell();
	const [error, setError] = useState<string | null>(null);
	const [connecting, setConnecting] = useState(true);
	// 深链在冷启动时可能重复触发，只跑一次，避免并存两次写入。
	const started = useRef(false);

	const rawUrl = firstParam(params.url);
	const rawToken = firstParam(params.token);

	useEffect(() => {
		if (started.current) return;
		started.current = true;

		void (async () => {
			try {
				const payload = buildPairPayload(rawUrl, rawToken);
				const existing = await store.load();
				await connectToControlPlane(payload, {
					verify: (candidate) => createClient(candidate).listNodes(),
					save: async (settings) => {
						await store.save(settings);
						reloadSettings();
					},
					notifications: existing?.notifications ?? true,
				});
				router.replace("/");
			} catch (connectError) {
				setError(connectError instanceof Error ? connectError.message : "连接失败");
				setConnecting(false);
			}
		})();
	}, [rawToken, rawUrl, reloadSettings, store]);

	return (
		<View style={styles.screen}>
			{connecting ? (
				<>
					<ActivityIndicator color={theme.colors.accent} />
					<Text style={styles.hint}>正在连接控制面…</Text>
				</>
			) : (
				<>
					<Text style={styles.title}>配对失败</Text>
					<Text style={styles.error}>{error}</Text>
					<Pressable style={styles.primary} onPress={() => router.replace("/settings")}>
						<Text style={styles.primaryText}>手动填写</Text>
					</Pressable>
				</>
			)}
		</View>
	);
}

function createStyles(theme: Theme) {
	const { colors } = theme;
	return StyleSheet.create({
		screen: {
			flex: 1,
			alignItems: "center",
			justifyContent: "center",
			paddingHorizontal: 24,
			gap: 12,
			backgroundColor: colors.background,
		},
		title: { fontSize: 18, fontWeight: "600", color: colors.text },
		hint: { fontSize: 14, color: colors.textSecondary },
		error: { fontSize: 14, color: colors.danger, textAlign: "center" },
		primary: {
			marginTop: 8,
			paddingHorizontal: 20,
			paddingVertical: 12,
			borderRadius: 12,
			backgroundColor: colors.action,
		},
		primaryText: { color: colors.actionText, fontSize: 15, fontWeight: "600" },
	});
}
