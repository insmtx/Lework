import type { ClaimPreview, NodeView } from "@lework/api";
import { router, useLocalSearchParams } from "expo-router";
import { useCallback, useEffect, useMemo, useState } from "react";
import { ActivityIndicator, Alert, Pressable, Text, TextInput, View } from "react-native";

import { createClient } from "../../src/api/client";
import {
	claimExpiryLabel,
	describeClaimError,
	isCompleteCode,
	normalizeCodeInput,
} from "../../src/nodes/claim";
import type { PairPayload } from "../../src/pairing/link";
import { PairScanner } from "../../src/pairing/scanner";
import { createSettingsStyles, SettingsPage, SettingsSection } from "../../src/settings/components";
import { createSecureSettingsStore } from "../../src/settings/secure-store";
import { authErrorMessage, saveServerAddress, type SessionDeps } from "../../src/settings/session";
import { useTheme, useThemedStyles } from "../../src/theme/use-theme";
import { useAppShell, useScreenTitle } from "../../src/ui/app-shell-context";

type Mode = "choose" | "input" | "preview";

/**
 * 添加电脑只有两条路：扫电脑上打印的二维码，或手输 5 位码。
 *
 * 认领是「把已有的机器挂到我的账号下」，所以这里不收集机器信息，也不出码；
 * 预览与确认都直接问控制面，避免手机上复制一份节点状态。
 */
export default function AddNodeScreen() {
	useScreenTitle("添加电脑");
	const theme = useTheme();
	const styles = useThemedStyles(createSettingsStyles);
	const { settings, settingsLoading, projects, reloadSettings } = useAppShell();
	const params = useLocalSearchParams<{ code?: string; url?: string; mode?: string }>();
	const settingsStore = useMemo(() => createSecureSettingsStore(), []);

	const paramCode = typeof params.code === "string" ? normalizeCodeInput(params.code) : "";
	const [mode, setMode] = useState<Mode>(() => {
		if (isCompleteCode(paramCode)) return "preview";
		return params.mode === "input" ? "input" : "choose";
	});
	const [code, setCode] = useState(paramCode);
	const [preview, setPreview] = useState<ClaimPreview | null>(null);
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const [scannerOpen, setScannerOpen] = useState(false);
	// attempt 是「重试」的显式计数：同一个码重拉预览时依赖不变，靠它触发。
	const [attempt, setAttempt] = useState(0);

	// 只有持有会话时才构造客户端：未登录时任何请求都必然 401，先送去登录。
	const client = useMemo(() => (settings?.sessionToken ? createClient(settings) : null), [settings]);

	const goLogin = useCallback((nextCode: string, nextUrl: string) => {
		router.replace({
			pathname: "/login",
			params: nextCode ? { code: nextCode, url: nextUrl } : {},
		});
	}, []);

	// 未登录却已经带着码进来：先登录，登录页会带着码回到这里。
	useEffect(() => {
		if (settingsLoading || mode !== "preview") return;
		if (!settings?.sessionToken) {
			// 深链可能带着另一台控制面的地址；它比本地旧地址更可信。
			const nextUrl = typeof params.url === "string" && params.url !== "" ? params.url : (settings?.baseUrl ?? "");
			goLogin(code, nextUrl);
		}
	}, [settingsLoading, settings?.sessionToken, settings?.baseUrl, params.url, mode, code, goLogin]);

	// 预览只在「进入预览且码完整且已登录」时拉一次；重试由按钮重新触发。
	useEffect(() => {
		if (mode !== "preview" || settingsLoading) return;
		if (!client || !isCompleteCode(code)) return;
		let cancelled = false;
		setBusy(true);
		setError(null);
		setPreview(null);
		void client
			.claimPreview(code)
			.then((next) => {
				if (!cancelled) setPreview(next);
			})
			.catch((caught: unknown) => {
				if (!cancelled) setError(describeClaimError(caught));
			})
			.finally(() => {
				if (!cancelled) setBusy(false);
			});
		return () => {
			cancelled = true;
		};
	}, [mode, code, client, settingsLoading, attempt]);

	// 剩余时间要跟着走；预览没有过期时间就不需要计时器。
	const [now, setNow] = useState(() => Date.now());
	useEffect(() => {
		if (!preview) return;
		const timer = setInterval(() => setNow(Date.now()), 1000);
		return () => clearInterval(timer);
	}, [preview]);

	const switchToServer = useCallback(
		async (payload: PairPayload) => {
			setBusy(true);
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
				setBusy(false);
				return;
			}
			setBusy(false);
			goLogin(payload.code, payload.baseUrl);
		},
		[settingsStore, settings, reloadSettings, goLogin],
	);

	const handleScanned = (payload: PairPayload) => {
		setScannerOpen(false);
		if (!settings?.sessionToken) {
			// 当前没有登录态：切地址不需要先退出，直接带着 url/code 去登录。
			void switchToServer(payload);
			return;
		}
		if (payload.baseUrl !== settings.baseUrl) {
			Alert.alert(
				"这是另一个控制面的码",
				`${payload.baseUrl}\n\n切换控制面会退出当前登录。`,
				[
					{ text: "取消", style: "cancel" },
					{ text: "切换", style: "destructive", onPress: () => void switchToServer(payload) },
				],
			);
			return;
		}
		setError(null);
		setCode(payload.code);
		setMode("preview");
	};

	const confirm = async () => {
		if (!client) return;
		setBusy(true);
		setError(null);
		try {
			const node: NodeView = await client.claimConfirm(code);
			projects.refresh();
			Alert.alert(`已加入 ${node.name}`, "这台电脑已归属于当前账号。", [
				{ text: "好", onPress: () => router.replace("/nodes") },
			]);
		} catch (caught) {
			setError(describeClaimError(caught));
		} finally {
			setBusy(false);
		}
	};

	const startOver = () => {
		setPreview(null);
		setError(null);
		setCode("");
		setMode("choose");
	};

	return (
		<SettingsPage>
			{mode === "choose" ? (
				<>
					<SettingsSection
						title="添加电脑"
						hint="在这台电脑上运行 lework agent，终端会显示二维码与 5 位配对码。"
					>
						<Pressable style={styles.button} onPress={() => setScannerOpen(true)}>
							<Text style={styles.buttonText}>扫描二维码</Text>
						</Pressable>
						<Pressable style={styles.pairButton} onPress={() => setMode("input")}>
							<Text style={styles.pairButtonText}>输入配对码</Text>
						</Pressable>
					</SettingsSection>
					{!settingsLoading && !settings?.sessionToken ? (
						<SettingsSection title="账号" hint="电脑要挂到账号下，先登录。">
							<Pressable style={styles.button} onPress={() => router.push("/login")}>
								<Text style={styles.buttonText}>去登录 / 注册</Text>
							</Pressable>
						</SettingsSection>
					) : (
						<SettingsSection title="当前控制面" hint="扫码时会切换到二维码所属的控制面。">
							<View style={styles.statusRow}>
								<View style={[styles.statusDot, { backgroundColor: theme.colors.success }]} />
								<Text style={styles.statusText} numberOfLines={1}>
									{settings?.baseUrl}
								</Text>
							</View>
						</SettingsSection>
					)}
				</>
			) : null}

			{mode === "input" ? (
				<SettingsSection title="输入配对码" hint="在 lework agent 的输出里找 5 位码；不区分大小写。">
					<TextInput
						style={styles.input}
						value={code}
						onChangeText={(text) => {
							setCode(normalizeCodeInput(text));
							setError(null);
						}}
						autoCapitalize="characters"
						autoCorrect={false}
						autoFocus
						maxLength={8}
						placeholder="AB234"
						placeholderTextColor={theme.colors.placeholder}
						selectionColor={theme.colors.accent}
					/>
					<Pressable
						disabled={!isCompleteCode(code) || busy}
						onPress={() => setMode("preview")}
						style={[styles.button, (!isCompleteCode(code) || busy) && styles.buttonDisabled]}
					>
						<Text style={styles.buttonText}>下一步</Text>
					</Pressable>
					{error ? <Text style={[styles.status, styles.statusFailed]}>{error}</Text> : null}
					<Pressable style={styles.pairButton} onPress={startOver}>
						<Text style={styles.pairButtonText}>返回</Text>
					</Pressable>
				</SettingsSection>
			) : null}

			{mode === "preview" ? (
				<SettingsSection title="确认添加" hint="确认后这台电脑会出现在「我的电脑」里。">
					{busy ? (
						<View style={{ paddingVertical: 12 }}>
							<ActivityIndicator color={theme.colors.accent} />
						</View>
					) : null}

					{preview ? (
						<View style={styles.card}>
							<Text style={styles.label}>{preview.node_name || "未命名电脑"}</Text>
							<Text style={styles.hint}>
								{preview.os}/{preview.arch} · agent {preview.agent_version}
							</Text>
							<Text style={styles.hint}>{claimExpiryLabel(preview.expires_at, now)}</Text>
						</View>
					) : null}

					{error ? <Text style={[styles.status, styles.statusFailed]}>{error}</Text> : null}

					<Pressable
						disabled={!preview || busy}
						onPress={() => void confirm()}
						style={[styles.button, (!preview || busy) && styles.buttonDisabled]}
					>
						<Text style={styles.buttonText}>加入我的账号</Text>
					</Pressable>

					{error ? (
						<Pressable
							style={styles.pairButton}
							onPress={() => {
								setError(null);
								setAttempt((value) => value + 1);
							}}
						>
							<Text style={styles.pairButtonText}>重试</Text>
						</Pressable>
					) : null}

					<Pressable style={styles.pairButton} onPress={startOver}>
						<Text style={styles.pairButtonText}>换一个码</Text>
					</Pressable>
				</SettingsSection>
			) : null}

			<PairScanner
				visible={scannerOpen}
				onClose={() => setScannerOpen(false)}
				onScanned={handleScanned}
			/>
		</SettingsPage>
	);
}
