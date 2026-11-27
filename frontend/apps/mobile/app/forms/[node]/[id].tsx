import type { FormAnswerValue, PendingForm } from "@lework/api";
import { router, useLocalSearchParams } from "expo-router";
import { useEffect, useState } from "react";
import {
	KeyboardAvoidingView,
	Platform,
	Pressable,
	ScrollView,
	StyleSheet,
	Text,
	View,
} from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";

import { createClient } from "../../../src/api/client";
import { FormCard } from "../../../src/session/form-card";
import type { Theme } from "../../../src/theme/theme";
import { useThemedStyles } from "../../../src/theme/use-theme";
import { useAppShell, useScreenTitle } from "../../../src/ui/app-shell-context";
import { LoadingScreen } from "../../../src/ui/loading";

function param(value: string | string[] | undefined): string {
	return Array.isArray(value) ? (value[0] ?? "") : (value ?? "");
}

// GlobalFormScreen 处理不挂在任何会话上的位置级表单。
//
// 目录不在 URL 里作为事实来源：进入时重新向节点要列表，以节点返回的
// directory 为准，避免深链里的旧目录把回答发到错误的工作区。
export default function GlobalFormScreen() {
	const params = useLocalSearchParams<{ node: string; id: string }>();
	const styles = useThemedStyles(createStyles);
	const insets = useSafeAreaInsets();
	const { settings, settingsLoading } = useAppShell();
	const nodeID = param(params.node);
	const formID = param(params.id);
	useScreenTitle("全局表单");

	const [form, setForm] = useState<PendingForm | null>(null);
	const [loading, setLoading] = useState(true);
	const [error, setError] = useState<string | null>(null);
	const [reload, setReload] = useState(0);

	// biome-ignore lint/correctness/useExhaustiveDependencies: reload 只用于回答/跳过失败后触发重拉，不参与请求本身
	useEffect(() => {
		if (!settings || !nodeID) return;
		let cancelled = false;
		setLoading(true);
		setError(null);
		createClient(settings)
			.listNodeForms(nodeID)
			.then((forms) => {
				if (cancelled) return;
				setForm(forms.find((item) => item.id === formID) ?? null);
			})
			.catch((caught: unknown) => {
				if (cancelled) return;
				setError(caught instanceof Error ? caught.message : "加载全局表单失败");
			})
			.finally(() => {
				if (!cancelled) setLoading(false);
			});
		return () => {
			cancelled = true;
		};
	}, [settings, nodeID, formID, reload]);

	const submit = async (answer: Record<string, FormAnswerValue>) => {
		if (!settings || !form) return;
		try {
			await createClient(settings).replyNodeForm(nodeID, form.id, form.directory ?? "", answer);
			router.back();
		} catch (caught) {
			setReload((value) => value + 1);
			throw caught;
		}
	};

	const cancel = async () => {
		if (!settings || !form) return;
		try {
			await createClient(settings).cancelNodeForm(nodeID, form.id, form.directory ?? "");
			router.back();
		} catch (caught) {
			setReload((value) => value + 1);
			throw caught;
		}
	};

	if (settingsLoading) {
		return <LoadingScreen text="正在加载表单…" />;
	}

	if (!settings) {
		return (
			<View style={styles.center}>
				<Text style={styles.title}>未连接</Text>
				<Text style={styles.hint}>先在设置里填写控制面地址与令牌。</Text>
				<Pressable style={styles.button} onPress={() => router.push("/settings")}>
					<Text style={styles.buttonText}>打开设置</Text>
				</Pressable>
			</View>
		);
	}

	// 已有表单时不要退回全屏加载：回答失败会触发 reload，整页闪一下会让人以为答案丢了。
	if (loading && !form) {
		return <LoadingScreen text="正在加载表单…" />;
	}

	return (
		<KeyboardAvoidingView
			style={styles.screen}
			behavior={Platform.OS === "ios" ? "padding" : undefined}
		>
			<ScrollView
				contentContainerStyle={[styles.content, { paddingBottom: insets.bottom + 16 }]}
				keyboardShouldPersistTaps="handled"
			>
				{error ? <Text style={styles.error}>{error}</Text> : null}
				{form ? (
					<FormCard form={form} onSubmit={submit} onCancel={cancel} />
				) : (
					<View style={styles.center}>
						<Text style={styles.title}>表单已处理或不存在</Text>
						<Text style={styles.hint}>它可能已经被回答、跳过，或所在节点已离线。</Text>
						<Pressable style={styles.button} onPress={() => router.back()}>
							<Text style={styles.buttonText}>返回</Text>
						</Pressable>
					</View>
				)}
			</ScrollView>
		</KeyboardAvoidingView>
	);
}

function createStyles(theme: Theme) {
	const { colors } = theme;
	return StyleSheet.create({
		screen: { flex: 1, backgroundColor: colors.background },
		// flexGrow 让空表单时的居中提示仍能占满可视区域。
		content: { flexGrow: 1, padding: 16 },
		center: { flex: 1, alignItems: "center", justifyContent: "center", padding: 24, gap: 8 },
		title: { fontSize: 18, fontWeight: "600", color: colors.text },
		hint: { color: colors.textSecondary, textAlign: "center" },
		error: { color: colors.danger, marginBottom: 8 },
		button: {
			marginTop: 8,
			backgroundColor: colors.action,
			borderRadius: 8,
			paddingHorizontal: 16,
			paddingVertical: 10,
		},
		buttonText: { color: colors.actionText, fontWeight: "600" },
	});
}
