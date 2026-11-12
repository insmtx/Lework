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
import { registerForPush, unregisterPush } from "../src/push/register";
import { createSecureSettingsStore } from "../src/settings/secure-store";
import { normalizeBaseUrl } from "../src/settings/settings";

export default function SettingsScreen() {
	const store = useMemo(() => createSecureSettingsStore(), []);
	const [baseUrl, setBaseUrl] = useState("");
	const [token, setToken] = useState("");
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

	const save = async () => {
		setBusy(true);
		setStatus(null);
		setFailed(false);
		try {
			const normalized = normalizeBaseUrl(baseUrl);
			const trimmedToken = token.trim();
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
						pushNote = "Push is not registered: grant notification permission and set the EAS project id.";
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
			<Text style={styles.label}>Server address</Text>
			<TextInput
				style={styles.input}
				value={baseUrl}
				onChangeText={setBaseUrl}
				autoCapitalize="none"
				autoCorrect={false}
				keyboardType="url"
				placeholder="https://box:8787"
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
			/>
			<Text style={styles.hint}>
				The token is stored in the Android keystore and sent only in the Authorization header.
			</Text>

			<View style={styles.switchRow}>
				<View style={styles.switchText}>
					<Text style={styles.label}>Notifications</Text>
					<Text style={styles.hint}>Push when a session needs approval.</Text>
				</View>
				<Switch value={notifications} onValueChange={setNotifications} />
			</View>

			<Pressable style={[styles.button, busy && styles.buttonDisabled]} onPress={save} disabled={busy}>
				{busy ? <ActivityIndicator color="#fff" /> : <Text style={styles.buttonText}>Save</Text>}
			</Pressable>

			{status ? <Text style={[styles.status, failed && styles.statusFailed]}>{status}</Text> : null}
		</ScrollView>
	);
}

const styles = StyleSheet.create({
	container: { padding: 16, gap: 8 },
	label: { fontSize: 14, fontWeight: "600" },
	hint: { fontSize: 12, color: "#666" },
	input: {
		borderWidth: 1,
		borderColor: "#ccc",
		borderRadius: 8,
		paddingHorizontal: 12,
		paddingVertical: 10,
		fontSize: 16,
	},
	switchRow: { flexDirection: "row", alignItems: "center", justifyContent: "space-between", marginTop: 8 },
	switchText: { flex: 1, gap: 2 },
	button: {
		marginTop: 16,
		backgroundColor: "#0f172a",
		borderRadius: 8,
		paddingVertical: 12,
		alignItems: "center",
	},
	buttonDisabled: { opacity: 0.6 },
	buttonText: { color: "#fff", fontSize: 16, fontWeight: "600" },
	status: { marginTop: 12, color: "#166534" },
	statusFailed: { color: "#b91c1c" },
});
