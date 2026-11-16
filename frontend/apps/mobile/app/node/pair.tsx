import { router, useLocalSearchParams } from "expo-router";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ActivityIndicator, Alert, Pressable, StyleSheet, Text, View } from "react-native";

import { createClient } from "../../src/api/client";
import { buildPairPayload, type PairPayload } from "../../src/pairing/link";
import { createSecureSettingsStore } from "../../src/settings/secure-store";
import { authErrorMessage, saveServerAddress, type SessionDeps } from "../../src/settings/session";
import type { Theme } from "../../src/theme/theme";
import { useTheme, useThemedStyles } from "../../src/theme/use-theme";
import { useAppShell } from "../../src/ui/app-shell-context";

/**
 * 配对深链的落点：`lework://node/pair?url=…&code=…`。
 *
 * 这个页面本身没有界面，只做三件事：解析参数、确保地址与会话匹配、把用户
 * 送到确认页。判断统一放在这里，扫码与深链因此走同一套「切控制面」语义。
 */
export default function PairDeepLinkScreen() {
	const theme = useTheme();
	const styles = useThemedStyles(createStyles);
	const { settings, settingsLoading, reloadSettings } = useAppShell();
	const params = useLocalSearchParams<{ code?: string; url?: string }>();
	const settingsStore = useMemo(() => createSecureSettingsStore(), []);
	const [error, setError] = useState<string | null>(null);
	const handled = useRef(false);

	const goLogin = useCallback((payload: PairPayload) => {
		router.replace({
			pathname: "/login",
			params: { code: payload.code, url: payload.baseUrl },
		});
	}, []);

	const switchAndLogin = useCallback(
		async (payload: PairPayload) => {
			try {
				const deps: SessionDeps = {
					store: settingsStore,
					client: (baseUrl, token) =>
						createClient({ baseUrl, email: "", sessionToken: token ?? "", notifications: false }),
				};
				await saveServerAddress(deps, settings, payload.baseUrl);
				reloadSettings();
			} catch (caught) {
				setError(authErrorMessage(caught));
				return;
			}
			goLogin(payload);
		},
		[settingsStore, settings, reloadSettings, goLogin],
	);

	useEffect(() => {
		if (settingsLoading || handled.current) return;

		let payload: PairPayload;
		try {
			payload = buildPairPayload(
				typeof params.url === "string" ? params.url : "",
				typeof params.code === "string" ? params.code : "",
			);
		} catch (caught) {
			setError(caught instanceof Error ? caught.message : "配对链接无效");
			return;
		}

		handled.current = true;
		if (!settings?.sessionToken) {
			// 没有登录态时切地址不丢任何东西，直接带着码去登录。
			void switchAndLogin(payload);
			return;
		}
		if (settings.baseUrl !== payload.baseUrl) {
			Alert.alert(
				"这是另一个控制面的码",
				`${payload.baseUrl}\n\n切换控制面会退出当前登录。`,
				[
					{
						text: "取消",
						style: "cancel",
						onPress: () => router.replace("/nodes"),
					},
					{
						text: "切换",
						style: "destructive",
						onPress: () => void switchAndLogin(payload),
					},
				],
			);
			return;
		}
		router.replace({ pathname: "/nodes/add", params: { code: payload.code } });
	}, [settingsLoading, settings, params.code, params.url, switchAndLogin]);

	if (error) {
		return (
			<View style={styles.center}>
				<Text style={styles.title}>无法配对</Text>
				<Text style={styles.hint}>{error}</Text>
				<Pressable style={styles.button} onPress={() => router.replace("/nodes/add")}>
					<Text style={styles.buttonText}>手动输入配对码</Text>
				</Pressable>
			</View>
		);
	}

	return (
		<View style={styles.center}>
			<ActivityIndicator color={theme.colors.accent} />
			<Text style={styles.hint}>正在打开配对…</Text>
		</View>
	);
}

function createStyles(theme: Theme) {
	const { colors } = theme;
	return StyleSheet.create({
		center: { flex: 1, alignItems: "center", justifyContent: "center", padding: 24, gap: 10 },
		title: { fontSize: 18, fontWeight: "600", color: colors.text },
		hint: { color: colors.textSecondary, textAlign: "center" },
		button: {
			marginTop: 4,
			backgroundColor: colors.action,
			borderRadius: 8,
			paddingHorizontal: 16,
			paddingVertical: 10,
		},
		buttonText: { color: colors.actionText, fontWeight: "600" },
	});
}
