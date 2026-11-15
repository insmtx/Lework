import { useEffect, useMemo, useState } from "react";
import {
	ActivityIndicator,
	Pressable,
	ScrollView,
	StyleSheet,
	Switch,
	Text,
	TextInput,
	View,
} from "react-native";

import { createClient } from "../src/api/client";
import { type PairPayload, parsePairLink } from "../src/pairing/link";
import { PairScanner } from "../src/pairing/scanner";
import { registerForPush, unregisterPush } from "../src/push/register";
import { createSecureSettingsStore } from "../src/settings/secure-store";
import { normalizeBaseUrl } from "../src/settings/settings";
import type { Theme, ThemeMode } from "../src/theme/theme";
import { useThemePreference } from "../src/theme/theme-context";
import { useTheme, useThemedStyles } from "../src/theme/use-theme";
import { useAppShell, useScreenTitle } from "../src/ui/app-shell-context";

const THEME_OPTIONS: { mode: ThemeMode; label: string }[] = [
	{ mode: "system", label: "跟随系统" },
	{ mode: "light", label: "亮色" },
	{ mode: "dark", label: "暗色" },
];

export default function SettingsScreen() {
	useScreenTitle("设置");
	const theme = useTheme();
	const { mode, setMode } = useThemePreference();
	const styles = useThemedStyles(createStyles);
	const store = useMemo(() => createSecureSettingsStore(), []);
	const { projects, reloadSettings } = useAppShell();
	const { nodes, dirByNode, setInboxDir } = projects;
	const [baseUrl, setBaseUrl] = useState("");
	const [token, setToken] = useState("");
	const [pairLink, setPairLink] = useState("");
	const [scannerOpen, setScannerOpen] = useState(false);
	const [notifications, setNotifications] = useState(true);
	const [deviceID, setDeviceID] = useState<string | undefined>(undefined);
	const [busy, setBusy] = useState(false);
	const [status, setStatus] = useState<string | null>(null);
	const [failed, setFailed] = useState(false);

	useEffect(() => {
		void store.load().then((settings) => {
			if (!settings) return;
			setBaseUrl(settings.baseUrl);
			setToken(settings.token);
			setNotifications(settings.notifications);
			setDeviceID(settings.deviceID);
		});
	}, [store]);

	const applyPairLink = () => {
		try {
			const payload = parsePairLink(pairLink);
			setBaseUrl(payload.baseUrl);
			setToken(payload.token);
			setFailed(false);
			setStatus("已填入地址与令牌，点 Save 连接。");
		} catch (pairError) {
			setFailed(true);
			setStatus(pairError instanceof Error ? pairError.message : "配对链接无法解析");
		}
	};

	const save = async (overrides?: { baseUrl?: string; token?: string }) => {
		setBusy(true);
		setStatus(null);
		setFailed(false);
		try {
			const normalized = normalizeBaseUrl(overrides?.baseUrl ?? baseUrl);
			const trimmedToken = (overrides?.token ?? token).trim();
			if (!trimmedToken) throw new Error("token is required");
			const settings = { baseUrl: normalized, token: trimmedToken, notifications, deviceID };
			// 先验证连通性再落盘，避免存下一个打不通的地址。
			const client = createClient(settings);
			await client.listNodes();

			// 推送登记失败不影响连接本身：保留已保存的地址与令牌，
			// 只在状态里说明推送尚未生效。
			let nextDeviceID = deviceID;
			let pushNote: string | null = null;
			if (notifications) {
				try {
					const registered = await registerForPush(settings, client);
					if (registered) {
						nextDeviceID = registered;
					} else {
						pushNote =
							"Push is not registered: grant notification permission and set the EAS project id.";
					}
				} catch (pushError) {
					pushNote = pushError instanceof Error ? pushError.message : "failed to register for push";
				}
			} else if (deviceID) {
				try {
					await unregisterPush(client, deviceID);
					nextDeviceID = undefined;
				} catch (pushError) {
					pushNote = pushError instanceof Error ? pushError.message : "failed to unregister push";
				}
			}

			await store.save({ ...settings, deviceID: nextDeviceID });
			setDeviceID(nextDeviceID);
			setBaseUrl(normalized);
			// 扫码路径只通过 overrides 传令牌，不回填输入框的话，保存成功后
			// 令牌栏仍是空白，看起来像没拿到令牌，再点 Save 还会报 token is required。
			setToken(trimmedToken);
			reloadSettings();
			setStatus(pushNote ? `Connected. Settings saved. ${pushNote}` : "Connected. Settings saved.");
		} catch (error) {
			setFailed(true);
			setStatus(error instanceof Error ? error.message : "failed to save settings");
		} finally {
			setBusy(false);
		}
	};

	return (
		<ScrollView contentContainerStyle={styles.container} keyboardShouldPersistTaps="handled">
			<Pressable style={styles.pairButton} onPress={() => setScannerOpen(true)}>
				<Text style={styles.pairButtonText}>扫码配对</Text>
			</Pressable>
			<Text style={styles.hint}>对着控制面启动时打印的二维码，识别后自动连接。</Text>

			<Text style={styles.label}>配对链接</Text>
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
			<Text style={styles.hint}>粘贴 serve 启动时打印的链接，自动填好下面的地址与令牌。</Text>

			<Text style={styles.label}>Server address</Text>
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

			<Text style={styles.label}>Enrollment token</Text>
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
			<Text style={styles.hint}>
				The token is stored in the Android keystore and sent only in the Authorization header.
			</Text>

			<View style={styles.switchRow}>
				<View style={styles.switchText}>
					<Text style={styles.label}>Notifications</Text>
					<Text style={styles.hint}>Push when a session needs approval.</Text>
				</View>
				<Switch
					value={notifications}
					onValueChange={setNotifications}
					trackColor={{ false: theme.colors.border, true: theme.colors.accent }}
					thumbColor={theme.colors.surface}
				/>
			</View>

			<Text style={[styles.label, styles.sectionTitle]}>外观</Text>
			<Text style={styles.hint}>立即生效，仅影响这台手机；默认跟随系统。</Text>
			<View style={styles.roots}>
				{THEME_OPTIONS.map((option) => {
					const active = mode === option.mode;
					return (
						<Pressable
							key={option.mode}
							style={[styles.root, active && styles.rootActive]}
							onPress={() => setMode(option.mode)}
						>
							<Text style={[styles.rootText, active && styles.rootTextActive]}>{option.label}</Text>
						</Pressable>
					);
				})}
			</View>

			<Text style={[styles.label, styles.sectionTitle]}>我的目录</Text>
			<Text style={styles.hint}>
				首页「我的」指向这个节点上的哪个目录。目录必须位于 agent 的 workspace roots
				之内，否则新建会话会被拒绝。
			</Text>
			{nodes.length === 0 ? <Text style={styles.hint}>还没有已登记的节点。</Text> : null}
			{nodes.map((node) => {
				const current = dirByNode[node.id];
				const roots = node.workspace_roots ?? [];
				return (
					<View key={node.id} style={styles.inbox}>
						<Text style={styles.inboxNode}>
							{node.name}
							{node.connected ? "" : "（离线）"}
						</Text>
						{current ? (
							<Text style={styles.hint} numberOfLines={1}>
								当前：{current}
							</Text>
						) : null}
						{roots.length === 0 ? (
							<Text style={styles.hint}>agent 未上报 workspace roots</Text>
						) : null}
						<View style={styles.roots}>
							{roots.map((root) => {
								const active = root === current;
								return (
									<Pressable
										key={root}
										style={[styles.root, active && styles.rootActive]}
										onPress={() => void setInboxDir(node.id, root)}
									>
										<Text
											style={[styles.rootText, active && styles.rootTextActive]}
											numberOfLines={1}
										>
											{root}
										</Text>
									</Pressable>
								);
							})}
						</View>
					</View>
				);
			})}

			<Pressable
				style={[styles.button, busy && styles.buttonDisabled]}
				onPress={() => void save()}
				disabled={busy}
			>
				{busy ? (
					<ActivityIndicator color={theme.colors.actionText} />
				) : (
					<Text style={styles.buttonText}>Save</Text>
				)}
			</Pressable>

			{status ? <Text style={[styles.status, failed && styles.statusFailed]}>{status}</Text> : null}

			<PairScanner
				visible={scannerOpen}
				onClose={() => setScannerOpen(false)}
				onScanned={(payload: PairPayload) => {
					setScannerOpen(false);
					// 扫码即连：复用 Save 的验证与推送登记流程，避免两套逻辑漂移。
					void save({ baseUrl: payload.baseUrl, token: payload.token });
				}}
			/>
		</ScrollView>
	);
}

function createStyles(theme: Theme) {
	const { colors } = theme;
	return StyleSheet.create({
		container: { padding: 16, gap: 8 },
		label: { fontSize: 14, fontWeight: "600", color: colors.text },
		hint: { fontSize: 12, color: colors.textSecondary },
		input: {
			borderWidth: 1,
			borderColor: colors.inputBorder,
			backgroundColor: colors.inputBackground,
			color: colors.text,
			borderRadius: 8,
			paddingHorizontal: 12,
			paddingVertical: 10,
			fontSize: 16,
		},
		switchRow: {
			flexDirection: "row",
			alignItems: "center",
			justifyContent: "space-between",
			marginTop: 8,
		},
		switchText: { flex: 1, gap: 2 },
		button: {
			marginTop: 16,
			backgroundColor: colors.action,
			borderRadius: 8,
			paddingVertical: 12,
			alignItems: "center",
		},
		buttonDisabled: { opacity: 0.6 },
		buttonText: { color: colors.actionText, fontSize: 16, fontWeight: "600" },
		pairButton: {
			alignSelf: "flex-start",
			borderWidth: 1,
			borderColor: colors.borderStrong,
			borderRadius: 8,
			paddingHorizontal: 10,
			paddingVertical: 6,
		},
		pairButtonText: { fontSize: 13, color: colors.text, fontWeight: "600" },
		status: { marginTop: 12, color: colors.success },
		statusFailed: { color: colors.danger },
		sectionTitle: { marginTop: 16 },
		inbox: { gap: 4, marginTop: 4 },
		inboxNode: { fontSize: 13, fontWeight: "600", color: colors.textStrong },
		roots: { flexDirection: "row", flexWrap: "wrap", gap: 8 },
		root: {
			borderWidth: 1,
			borderColor: colors.borderStrong,
			borderRadius: 8,
			paddingHorizontal: 10,
			paddingVertical: 6,
			maxWidth: "100%",
		},
		rootActive: { backgroundColor: colors.action, borderColor: colors.action },
		rootText: { fontSize: 12, color: colors.textStrong },
		rootTextActive: { color: colors.actionText },
	});
}
