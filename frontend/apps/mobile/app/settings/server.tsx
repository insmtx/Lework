import { useEffect, useMemo, useState } from "react";
import { ActivityIndicator, Pressable, Text, TextInput, View } from "react-native";

import { createClient } from "../../src/api/client";
import { type PairPayload, parsePairLink } from "../../src/pairing/link";
import { PairScanner } from "../../src/pairing/scanner";
import { registerForPush } from "../../src/push/register";
import { createSettingsStyles, SettingsPage, SettingsSection } from "../../src/settings/components";
import { buildConnectionSettings, connectionChanged } from "../../src/settings/connect";
import { createSecureSettingsStore } from "../../src/settings/secure-store";
import { useSettings } from "../../src/settings/use-settings";
import { useThemePreference } from "../../src/theme/theme-context";
import { useTheme, useThemedStyles } from "../../src/theme/use-theme";
import { useAppShell, useScreenTitle } from "../../src/ui/app-shell-context";

/**
 * 服务器设置负责「连到哪个控制面」，扫码与手填共用同一套保存逻辑。
 *
 * 保存前先用 listNodes 验证地址与令牌，避免落盘一个连不上的配置；
 * 地址或令牌变化意味着换了控制面，已登记的推送设备不再属于它，需要重登。
 */
export default function ServerScreen() {
	useScreenTitle("服务器设置");
	const theme = useTheme();
	const styles = useThemedStyles(createSettingsStyles);
	const { settings, reload } = useSettings();
	const { preferences } = useThemePreference();
	const { reloadSettings } = useAppShell();
	const store = useMemo(() => createSecureSettingsStore(), []);

	const [baseUrl, setBaseUrl] = useState("");
	const [token, setToken] = useState("");
	const [pairLink, setPairLink] = useState("");
	const [scannerOpen, setScannerOpen] = useState(false);
	const [busy, setBusy] = useState(false);
	const [status, setStatus] = useState<string | null>(null);
	const [failed, setFailed] = useState(false);

	useEffect(() => {
		if (!settings) return;
		setBaseUrl(settings.baseUrl);
		setToken(settings.token);
	}, [settings]);

	const note = (message: string, isError = false) => {
		setFailed(isError);
		setStatus(message);
	};

	const applyPairLink = () => {
		try {
			const payload = parsePairLink(pairLink);
			setBaseUrl(payload.baseUrl);
			setToken(payload.token);
			setFailed(false);
			setStatus("已填入地址与令牌，点「保存并连接」完成。");
		} catch (error) {
			note(error instanceof Error ? error.message : "配对链接无法解析", true);
		}
	};

	const save = async (overrides?: { baseUrl?: string; token?: string }) => {
		setBusy(true);
		setStatus(null);
		setFailed(false);
		try {
			const next = buildConnectionSettings({
				baseUrl: overrides?.baseUrl ?? baseUrl,
				token: overrides?.token ?? token,
				notifications: settings?.notifications ?? true,
				deviceID: settings?.deviceID,
			});
			const client = createClient(next);
			await client.listNodes();

			const moved = connectionChanged(settings, next);
			let deviceID = moved ? undefined : next.deviceID;
			let pushNote: string | null = null;
			if (next.notifications && !deviceID) {
				try {
					const registered = await registerForPush(next, client, preferences.soundEnabled);
					if (registered) {
						deviceID = registered;
					} else {
						pushNote = "推送未登记：请授予通知权限并配置 EAS project id。";
					}
				} catch (error) {
					pushNote = error instanceof Error ? error.message : "推送登记失败";
				}
			}

			const saved = {
				...next,
				// 登记失败时不要留下「推送开启但没有设备」的状态：设置首页会因此说谎。
				notifications: next.notifications && deviceID !== undefined,
				deviceID,
			};
			await store.save(saved);
			setBaseUrl(saved.baseUrl);
			setToken(saved.token);
			reload();
			reloadSettings();
			note(pushNote ? `已连接。${pushNote}` : "已连接，设置已保存。");
		} catch (error) {
			note(error instanceof Error ? error.message : "保存设置失败", true);
		} finally {
			setBusy(false);
		}
	};

	return (
		<SettingsPage>
			<SettingsSection title="配对" hint="对着控制面启动时打印的二维码，识别后自动连接。">
				<Pressable style={styles.pairButton} onPress={() => setScannerOpen(true)}>
					<Text style={styles.pairButtonText}>扫码配对</Text>
				</Pressable>
				<Text style={styles.fieldLabel}>配对链接</Text>
				<TextInput
					style={styles.input}
					value={pairLink}
					onChangeText={setPairLink}
					autoCapitalize="none"
					autoCorrect={false}
					placeholder="lework://connect?url=…"
					placeholderTextColor={theme.colors.placeholder}
					selectionColor={theme.colors.accent}
				/>
				<Pressable style={styles.pairButton} onPress={applyPairLink}>
					<Text style={styles.pairButtonText}>从配对链接填入</Text>
				</Pressable>
			</SettingsSection>

			<SettingsSection title="连接" hint="当前生效的控制面；换服务器只需改下面的地址与令牌。">
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
						{settings ? settings.baseUrl : "尚未连接"}
					</Text>
				</View>
			</SettingsSection>

			<SettingsSection
				title="服务器"
				hint="地址必须是完整的 http(s) URL；令牌只保存在本机 keystore。"
			>
				<Text style={styles.fieldLabel}>控制面地址</Text>
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

				<Text style={styles.fieldLabel}>注册令牌</Text>
				<TextInput
					style={styles.input}
					value={token}
					onChangeText={setToken}
					autoCapitalize="none"
					autoCorrect={false}
					secureTextEntry
					placeholder="shared enrollment token"
					placeholderTextColor={theme.colors.placeholder}
					selectionColor={theme.colors.accent}
				/>
				<Text style={styles.hint}>令牌只通过 Authorization 头发送，不会写进日志。</Text>

				<Pressable
					style={[styles.button, busy && styles.buttonDisabled]}
					onPress={() => void save()}
					disabled={busy}
				>
					{busy ? (
						<ActivityIndicator color={theme.colors.actionText} />
					) : (
						<Text style={styles.buttonText}>保存并连接</Text>
					)}
				</Pressable>

				{status ? (
					<Text style={[styles.status, failed && styles.statusFailed]}>{status}</Text>
				) : null}
			</SettingsSection>

			<PairScanner
				visible={scannerOpen}
				onClose={() => setScannerOpen(false)}
				onScanned={(payload: PairPayload) => {
					setScannerOpen(false);
					// 扫码即连：复用保存流程，避免两套验证逻辑漂移。
					void save({ baseUrl: payload.baseUrl, token: payload.token });
				}}
			/>
		</SettingsPage>
	);
}
