import { router } from "expo-router";
import { useEffect, useMemo, useState } from "react";
import { ActivityIndicator, Pressable, Text, TextInput, View } from "react-native";

import { createClient } from "../../src/api/client";
import { createSettingsStyles, SettingsPage, SettingsSection } from "../../src/settings/components";
import { createSecureSettingsStore } from "../../src/settings/secure-store";
import {
	authErrorMessage,
	saveServerAddress,
	type SessionDeps,
} from "../../src/settings/session";
import { useSettings } from "../../src/settings/use-settings";
import { useTheme, useThemedStyles } from "../../src/theme/use-theme";
import { useAppShell, useScreenTitle } from "../../src/ui/app-shell-context";

/**
 * 服务器设置只回答一个问题：连到哪个控制面。
 *
 * 账号与令牌不在这里填——它们由登录页产生。地址变化意味着换了一台服务器，
 * 旧会话不再通用，保存后必须重新登录；地址没变则保留登录态。
 */
export default function ServerScreen() {
	useScreenTitle("服务器设置");
	const theme = useTheme();
	const styles = useThemedStyles(createSettingsStyles);
	const { settings, reload } = useSettings();
	const { reloadSettings } = useAppShell();
	const store = useMemo(() => createSecureSettingsStore(), []);

	const [baseUrl, setBaseUrl] = useState("");
	const [busy, setBusy] = useState(false);
	const [status, setStatus] = useState<string | null>(null);
	const [failed, setFailed] = useState(false);

	useEffect(() => {
		if (!settings) return;
		setBaseUrl(settings.baseUrl);
	}, [settings]);

	const note = (message: string, isError = false) => {
		setFailed(isError);
		setStatus(message);
	};

	const save = async () => {
		setBusy(true);
		setStatus(null);
		setFailed(false);
		try {
			const deps: SessionDeps = { store, client: buildClient };
			const next = await saveServerAddress(deps, settings, baseUrl);
			setBaseUrl(next.baseUrl);
			reload();
			reloadSettings();
			if (!next.sessionToken) {
				router.replace("/login");
				return;
			}
			note("已保存。");
		} catch (error) {
			note(authErrorMessage(error), true);
		} finally {
			setBusy(false);
		}
	};

	const canSave = baseUrl.trim() !== "" && !busy;

	return (
		<SettingsPage>
			<SettingsSection title="当前连接" hint="账号与节点都属于这个控制面。">
				<View style={styles.statusRow}>
					<View
						style={[
							styles.statusDot,
							{
								backgroundColor: settings ? theme.colors.success : theme.colors.offline,
							},
						]}
					/>
					<Text style={styles.statusText} numberOfLines={1}>
						{settings ? settings.baseUrl : "尚未设置"}
					</Text>
				</View>
				<Text style={styles.hint}>
					{settings?.sessionToken ? `已登录：${settings.email}` : "未登录"}
				</Text>
			</SettingsSection>

			<SettingsSection
				title="控制面地址"
				hint="必须是完整的 http(s) URL；换地址后需要重新登录。"
			>
				<TextInput
					style={styles.input}
					value={baseUrl}
					onChangeText={setBaseUrl}
					autoCapitalize="none"
					autoCorrect={false}
					keyboardType="url"
					placeholder="https://box:8787"
					placeholderTextColor={theme.colors.placeholder}
					selectionColor={theme.colors.accent}
				/>

				<Pressable
					style={[styles.button, !canSave && styles.buttonDisabled]}
					onPress={() => void save()}
					disabled={!canSave}
				>
					{busy ? (
						<ActivityIndicator color={theme.colors.actionText} />
					) : (
						<Text style={styles.buttonText}>保存</Text>
					)}
				</Pressable>

				{status ? (
					<Text style={[styles.status, failed && styles.statusFailed]}>{status}</Text>
				) : null}
			</SettingsSection>
		</SettingsPage>
	);
}

function buildClient(baseUrl: string, token?: string) {
	return createClient({
		baseUrl,
		email: "",
		sessionToken: token ?? "",
		notifications: false,
	});
}
