import { router, useLocalSearchParams } from "expo-router";
import { useEffect, useMemo, useState } from "react";
import { ActivityIndicator, Pressable, Text, TextInput, View } from "react-native";

import { createClient } from "../src/api/client";
import { createSettingsStyles, SettingsPage, SettingsSection } from "../src/settings/components";
import { createSecureSettingsStore } from "../src/settings/secure-store";
import {
	authErrorMessage,
	canRegister,
	loadAuthMethods,
	signIn,
	signUp,
	type SessionDeps,
} from "../src/settings/session";
import { normalizeBaseUrl } from "../src/settings/settings";
import { useSettings } from "../src/settings/use-settings";
import { useTheme, useThemedStyles } from "../src/theme/use-theme";
import { useAppShell } from "../src/ui/app-shell-context";

type Mode = "login" | "register";

/**
 * 登录页同时承担注册：控制面开放注册时是同一个表单的两个按钮。
 *
 * 手机没有 cookie 罐，登录成功后把服务端返回的会话令牌存进 SecureStore；
 * 深链带 code 时登录完直接进入添加电脑的确认页，不让用户重新扫一次。
 */
export default function LoginScreen() {
	const theme = useTheme();
	const styles = useThemedStyles(createSettingsStyles);
	const { settings } = useSettings();
	const { reloadSettings } = useAppShell();
	const params = useLocalSearchParams<{ url?: string; code?: string }>();
	const store = useMemo(() => createSecureSettingsStore(), []);

	const initialUrl = typeof params.url === "string" && params.url !== "" ? params.url : undefined;
	const [baseUrl, setBaseUrl] = useState(initialUrl ?? settings?.baseUrl ?? "");
	const [email, setEmail] = useState(settings?.email ?? "");
	const [password, setPassword] = useState("");
	const [mode, setMode] = useState<Mode>("login");
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const [methods, setMethods] = useState<{
		initialized: boolean;
		registration_mode: "open" | "closed";
	} | null>(null);

	useEffect(() => {
		if (!settings) return;
		if (!initialUrl) setBaseUrl((current) => (current === "" ? settings.baseUrl : current));
		setEmail((current) => (current === "" ? settings.email : current));
	}, [settings, initialUrl]);

	// 地址一改就问一次公开信息：登录页按钮与「未初始化」提示都取决于它。
	useEffect(() => {
		let normalized: string;
		try {
			normalized = normalizeBaseUrl(baseUrl);
		} catch {
			setMethods(null);
			return;
		}
		let cancelled = false;
		const deps: Pick<SessionDeps, "client"> = { client: buildClient };
		void loadAuthMethods(deps, normalized)
			.then((loaded) => {
				if (!cancelled) setMethods(loaded);
			})
			.catch(() => {
				if (!cancelled) setMethods(null);
			});
		return () => {
			cancelled = true;
		};
	}, [baseUrl]);

	const submit = async () => {
		setBusy(true);
		setError(null);
		try {
			const deps: SessionDeps = { store, client: buildClient };
			const input = { baseUrl, email, password };
			await (mode === "login" ? signIn(deps, input) : signUp(deps, input));
			reloadSettings();
			if (typeof params.code === "string" && params.code !== "") {
				router.replace({
					pathname: "/nodes/add",
					params: { code: params.code, url: normalizeBaseUrl(baseUrl) },
				});
				return;
			}
			router.replace("/");
		} catch (caught) {
			setError(authErrorMessage(caught));
		} finally {
			setBusy(false);
		}
	};

	// methods 是异步到的：用户可能已经在它返回前切到了注册模式。
	useEffect(() => {
		if (methods?.registration_mode === "closed" && mode === "register") setMode("login");
	}, [methods, mode]);

	const registrationClosed = methods?.registration_mode === "closed";
	const canSubmit = baseUrl.trim() !== "" && email.trim() !== "" && password !== "" && !busy;

	return (
		<SettingsPage>
			<SettingsSection
				title="账号"
				hint="用控制面账号登录；节点与项目都归属于这个账号。"
			>
				<Text style={styles.fieldLabel}>服务器地址</Text>
				<TextInput
					style={styles.input}
					value={baseUrl}
					onChangeText={setBaseUrl}
					autoCapitalize="none"
					autoCorrect={false}
					keyboardType="url"
					placeholder="http://192.168.1.10:8787"
					placeholderTextColor={theme.colors.placeholder}
				/>
				<Text style={styles.fieldLabel}>邮箱</Text>
				<TextInput
					style={styles.input}
					value={email}
					onChangeText={setEmail}
					autoCapitalize="none"
					autoCorrect={false}
					keyboardType="email-address"
					placeholder="you@example.com"
					placeholderTextColor={theme.colors.placeholder}
				/>
				<Text style={styles.fieldLabel}>密码</Text>
				<TextInput
					style={styles.input}
					value={password}
					onChangeText={setPassword}
					secureTextEntry
					placeholder="至少 8 位"
					placeholderTextColor={theme.colors.placeholder}
				/>

				<Pressable
					disabled={!canSubmit}
					onPress={() => void submit()}
					style={[styles.button, !canSubmit && styles.buttonDisabled]}
				>
					{busy ? (
						<ActivityIndicator color={theme.colors.actionText} />
					) : (
						<Text style={styles.buttonText}>{mode === "login" ? "登录" : "注册并登录"}</Text>
					)}
				</Pressable>

				{canRegister(methods) ? (
					<View style={{ flexDirection: "row", gap: 12 }}>
						<Pressable onPress={() => setMode(mode === "login" ? "register" : "login")}>
							<Text style={styles.hint}>
								{mode === "login" ? "还没有账号？注册" : "已有账号？去登录"}
							</Text>
						</Pressable>
					</View>
				) : null}

				{registrationClosed ? (
					<Text style={styles.hint}>这台服务器未开放注册</Text>
				) : null}
				{methods && !methods.initialized ? (
					<Text style={styles.hint}>
						这台服务器还没有管理员：在电脑上启动 lework，用默认账号 admin / admin123
						登录后修改密码。
					</Text>
				) : null}
				{error ? <Text style={[styles.status, styles.statusFailed]}>{error}</Text> : null}
			</SettingsSection>
		</SettingsPage>
	);
}

// buildClient 用地址（可选令牌）构造客户端；登录页只需要无令牌的实例。
function buildClient(baseUrl: string, token?: string) {
	return createClient({
		baseUrl,
		email: "",
		sessionToken: token ?? "",
		notifications: false,
	});
}
