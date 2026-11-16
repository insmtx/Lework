import { router } from "expo-router";
import { useMemo, useState } from "react";
import { Pressable, Text, TextInput, View } from "react-native";

import { createClient } from "../../src/api/client";
import { createSettingsStyles, SettingsPage, SettingsSection } from "../../src/settings/components";
import { createSecureSettingsStore } from "../../src/settings/secure-store";
import {
	authErrorMessage,
	changePassword,
	signOut,
	type SessionDeps,
} from "../../src/settings/session";
import { useSettings } from "../../src/settings/use-settings";
import { useTheme, useThemedStyles } from "../../src/theme/use-theme";
import { useAppShell, useScreenTitle } from "../../src/ui/app-shell-context";

/**
 * 账号页只做三件事：看清当前身份、改密码、退出登录。
 *
 * 改密码要求旧密码：默认管理员密码是公开的，没有旧密码校验就等于任何拿到
 * 手机的人都能改掉部署账号。成功后后端会吊销其他设备的会话，这里不动本地
 * 会话——当前设备应当继续可用。
 */
export default function AccountScreen() {
	useScreenTitle("账号");
	const theme = useTheme();
	const styles = useThemedStyles(createSettingsStyles);
	const { settings, reload } = useSettings();
	const { reloadSettings } = useAppShell();
	const store = useMemo(() => createSecureSettingsStore(), []);

	const [currentPassword, setCurrentPassword] = useState("");
	const [newPassword, setNewPassword] = useState("");
	const [busy, setBusy] = useState(false);
	const [status, setStatus] = useState<string | null>(null);
	const [failed, setFailed] = useState(false);

	const note = (message: string, isError = false) => {
		setFailed(isError);
		setStatus(message);
	};

	if (!settings?.sessionToken) {
		return (
			<SettingsPage>
				<SettingsSection title="账号" hint="登录后才能管理账号与节点。">
					<Pressable style={styles.button} onPress={() => router.replace("/login")}>
						<Text style={styles.buttonText}>去登录</Text>
					</Pressable>
				</SettingsSection>
			</SettingsPage>
		);
	}

	const savePassword = async () => {
		setBusy(true);
		setStatus(null);
		setFailed(false);
		try {
			const deps: SessionDeps = { store, client: buildClient };
			await changePassword(deps, settings, { currentPassword, newPassword });
			setCurrentPassword("");
			setNewPassword("");
			note("密码已更新；其他设备需要重新登录。");
		} catch (caught) {
			note(authErrorMessage(caught), true);
		} finally {
			setBusy(false);
		}
	};

	const logout = async () => {
		setBusy(true);
		setStatus(null);
		try {
			const deps: SessionDeps = { store, client: buildClient };
			await signOut(deps, settings);
			reload();
			reloadSettings();
			router.replace("/login");
		} catch (caught) {
			note(authErrorMessage(caught), true);
		} finally {
			setBusy(false);
		}
	};

	const canSave = currentPassword !== "" && newPassword.length >= 8 && !busy;

	return (
		<SettingsPage>
			<SettingsSection title="当前账号" hint="节点与项目都归属于这个账号。">
				<View style={styles.card}>
					<Text style={styles.label}>{settings.email}</Text>
					<Text style={styles.hint}>{settings.baseUrl}</Text>
				</View>
			</SettingsSection>

			<SettingsSection title="修改密码" hint="密码至少 8 位；成功后其他设备会被登出。">
				<Text style={styles.fieldLabel}>当前密码</Text>
				<TextInput
					style={styles.input}
					value={currentPassword}
					onChangeText={setCurrentPassword}
					secureTextEntry
					placeholderTextColor={theme.colors.placeholder}
				/>
				<Text style={styles.fieldLabel}>新密码</Text>
				<TextInput
					style={styles.input}
					value={newPassword}
					onChangeText={setNewPassword}
					secureTextEntry
					placeholder="至少 8 位"
					placeholderTextColor={theme.colors.placeholder}
				/>
				<Pressable
					disabled={!canSave}
					onPress={() => void savePassword()}
					style={[styles.button, !canSave && styles.buttonDisabled]}
				>
					<Text style={styles.buttonText}>保存新密码</Text>
				</Pressable>
				{status ? (
					<Text style={[styles.status, failed && styles.statusFailed]}>{status}</Text>
				) : null}
			</SettingsSection>

			<SettingsSection title="退出登录" hint="只清除这台手机上的登录态，不影响其他设备。">
				<Pressable
					disabled={busy}
					onPress={() => void logout()}
					style={[styles.pairButton, busy && styles.buttonDisabled]}
				>
					<Text style={styles.pairButtonText}>退出登录</Text>
				</Pressable>
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
