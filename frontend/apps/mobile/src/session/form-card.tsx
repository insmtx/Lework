import type { FormAnswerValue, FormField, PendingForm } from "@lework/api";
import {
	applyCustom,
	buildAnswer,
	FormValidationError,
	initialAnswers,
	validateForm,
	visibleFields,
} from "@lework/api";
import { useState } from "react";
import { Linking, Pressable, StyleSheet, Text, TextInput, View } from "react-native";

import type { Theme } from "../theme/theme";
import { useTheme, useThemedStyles } from "../theme/use-theme";

interface FormCardProps {
	form: PendingForm;
	onSubmit: (answer: Record<string, FormAnswerValue>) => Promise<void>;
	onCancel: () => Promise<void>;
}

// FormCard 渲染一张 OpenCode question 表单。会话页与 global 表单页共用，
// 校验与答案构建全部来自 @lework/api，界面只负责收集用户输入。
export function FormCard({ form, onSubmit, onCancel }: FormCardProps) {
	const theme = useTheme();
	const styles = useThemedStyles(createStyles);
	const [answers, setAnswers] = useState<Record<string, FormAnswerValue>>(() =>
		initialAnswers(form),
	);
	const [custom, setCustom] = useState<Record<string, string>>({});
	const [errors, setErrors] = useState<Record<string, string>>({});
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<string | null>(null);

	const setAnswer = (key: string, value: FormAnswerValue | undefined) => {
		setAnswers((previous) => {
			const next = { ...previous };
			if (value === undefined) delete next[key];
			else next[key] = value;
			return next;
		});
	};

	const fields = visibleFields(form, answers);

	const submit = async () => {
		setError(null);
		// 自定义输入在提交时并入答案，界面上的选项值保持原样。
		const merged = { ...answers };
		for (const field of form.fields) {
			const value = applyCustom(field, merged[field.key], custom[field.key] ?? "");
			if (value === undefined) delete merged[field.key];
			else merged[field.key] = value;
		}

		const issues = validateForm(form, merged);
		if (issues.length > 0) {
			setErrors(Object.fromEntries(issues.map((issue) => [issue.fieldKey, issue.message])));
			return;
		}
		setErrors({});
		setBusy(true);
		try {
			await onSubmit(buildAnswer(form, merged));
		} catch (caught) {
			if (caught instanceof FormValidationError) {
				setErrors({ [caught.fieldKey]: caught.message });
			} else {
				setError(caught instanceof Error ? caught.message : "提交失败");
			}
		} finally {
			setBusy(false);
		}
	};

	const cancel = async () => {
		setError(null);
		setBusy(true);
		try {
			await onCancel();
		} catch (caught) {
			setError(caught instanceof Error ? caught.message : "跳过失败");
		} finally {
			setBusy(false);
		}
	};

	const openExternal = async (url: string | undefined) => {
		// 只放行 http(s)：file:// 与自定义 scheme 可能被远端表单用来唤起本机应用。
		if (!url || !/^https?:\/\//i.test(url)) return;
		try {
			if (await Linking.canOpenURL(url)) await Linking.openURL(url);
		} catch {
			// 打不开链接不是表单错误；用户仍可自行复制或跳过。
		}
	};

	const renderField = (field: FormField) => {
		const value = answers[field.key];
		const disabled = busy;
		return (
			<View key={field.key} style={styles.field}>
				<Text style={styles.fieldTitle}>{field.title || field.key}</Text>
				{field.description ? <Text style={styles.fieldHint}>{field.description}</Text> : null}

				{field.type === "string" && field.options?.length ? (
					<View style={styles.options}>
						{field.options.map((option) => {
							const active = value === option.value;
							return (
								<Pressable
									key={option.value}
									accessibilityRole="button"
									disabled={disabled}
									style={[styles.option, active && styles.optionActive]}
									onPress={() => setAnswer(field.key, option.value)}
								>
									<Text style={[styles.optionLabel, active && styles.optionLabelActive]}>
										{option.label}
									</Text>
									{option.description ? (
										<Text style={styles.optionHint}>{option.description}</Text>
									) : null}
								</Pressable>
							);
						})}
						{field.custom ? (
							<TextInput
								style={styles.input}
								value={custom[field.key] ?? ""}
								onChangeText={(text) => setCustom((prev) => ({ ...prev, [field.key]: text }))}
								placeholder={field.placeholder || "自定义答案"}
								placeholderTextColor={theme.colors.placeholder}
								selectionColor={theme.colors.accent}
								editable={!disabled}
							/>
						) : null}
					</View>
				) : null}

				{field.type === "string" && !field.options?.length ? (
					<TextInput
						style={styles.input}
						value={typeof value === "string" ? value : ""}
						onChangeText={(text) => setAnswer(field.key, text)}
						placeholder={field.placeholder}
						placeholderTextColor={theme.colors.placeholder}
						selectionColor={theme.colors.accent}
						editable={!disabled}
					/>
				) : null}

				{field.type === "multiselect" ? (
					<View style={styles.options}>
						{(field.options ?? []).map((option) => {
							const selected = Array.isArray(value) && value.includes(option.value);
							return (
								<Pressable
									key={option.value}
									accessibilityRole="button"
									disabled={disabled}
									style={[styles.option, selected && styles.optionActive]}
									onPress={() => {
										const current = Array.isArray(value) ? value : [];
										setAnswer(
											field.key,
											selected
												? current.filter((item) => item !== option.value)
												: [...current, option.value],
										);
									}}
								>
									<Text style={[styles.optionLabel, selected && styles.optionLabelActive]}>
										{selected ? "✓ " : ""}
										{option.label}
									</Text>
								</Pressable>
							);
						})}
						{field.custom ? (
							<View style={styles.customRow}>
								<TextInput
									style={[styles.input, styles.customInput]}
									value={custom[field.key] ?? ""}
									onChangeText={(text) => setCustom((prev) => ({ ...prev, [field.key]: text }))}
									placeholder={field.placeholder || "自定义选项"}
									placeholderTextColor={theme.colors.placeholder}
									selectionColor={theme.colors.accent}
									editable={!disabled}
								/>
								<Pressable
									accessibilityRole="button"
									disabled={disabled}
									style={styles.secondaryButton}
									onPress={() => {
										setAnswer(field.key, applyCustom(field, value, custom[field.key] ?? "") ?? []);
										setCustom((prev) => ({ ...prev, [field.key]: "" }));
									}}
								>
									<Text style={styles.secondaryButtonText}>添加</Text>
								</Pressable>
							</View>
						) : null}
					</View>
				) : null}

				{field.type === "boolean" ? (
					<View style={styles.inlineButtons}>
						{[true, false].map((option) => {
							const active = value === option;
							return (
								<Pressable
									key={String(option)}
									accessibilityRole="button"
									disabled={disabled}
									style={[styles.option, active && styles.optionActive]}
									onPress={() => setAnswer(field.key, option)}
								>
									<Text style={[styles.optionLabel, active && styles.optionLabelActive]}>
										{option ? "是" : "否"}
									</Text>
								</Pressable>
							);
						})}
					</View>
				) : null}

				{field.type === "number" || field.type === "integer" ? (
					<TextInput
						style={styles.input}
						value={typeof value === "number" || typeof value === "string" ? String(value) : ""}
						onChangeText={(text) => {
							const trimmed = text.trim();
							if (trimmed === "") {
								setAnswer(field.key, undefined);
								return;
							}
							const parsed = Number(trimmed);
							// 非法输入保留原文，交给 validateForm 给出统一提示。
							setAnswer(field.key, Number.isNaN(parsed) ? trimmed : parsed);
						}}
						keyboardType="numeric"
						placeholder={field.placeholder}
						placeholderTextColor={theme.colors.placeholder}
						selectionColor={theme.colors.accent}
						editable={!disabled}
					/>
				) : null}

				{field.type === "external" ? (
					<View style={styles.options}>
						{field.url ? (
							<Pressable
								accessibilityRole="link"
								disabled={disabled}
								style={styles.secondaryButton}
								onPress={() => void openExternal(field.url)}
							>
								<Text style={styles.secondaryButtonText}>打开链接</Text>
							</Pressable>
						) : null}
						<Pressable
							accessibilityRole="button"
							disabled={disabled}
							style={[styles.option, value === true && styles.optionActive]}
							onPress={() => setAnswer(field.key, value !== true)}
						>
							<Text style={[styles.optionLabel, value === true && styles.optionLabelActive]}>
								{value === true ? "✓ " : ""}我已打开并确认
							</Text>
						</Pressable>
					</View>
				) : null}

				{errors[field.key] ? <Text style={styles.fieldError}>{errors[field.key]}</Text> : null}
			</View>
		);
	};

	return (
		<View style={styles.card}>
			<Text style={styles.title}>需要你的回答</Text>
			{form.title && form.title !== "Questions" ? (
				<Text style={styles.subtitle}>{form.title}</Text>
			) : null}

			{fields.map(renderField)}

			{error ? <Text style={styles.error}>{error}</Text> : null}

			<View style={styles.buttons}>
				<Pressable
					accessibilityRole="button"
					disabled={busy}
					style={[styles.primaryButton, busy && styles.disabled]}
					onPress={() => void submit()}
				>
					<Text style={styles.primaryButtonText}>提交</Text>
				</Pressable>
				<Pressable
					accessibilityRole="button"
					disabled={busy}
					style={[styles.skipButton, busy && styles.disabled]}
					onPress={() => void cancel()}
				>
					<Text style={styles.skipButtonText}>跳过</Text>
				</Pressable>
			</View>
		</View>
	);
}

function createStyles(theme: Theme) {
	const { colors } = theme;
	return StyleSheet.create({
		card: {
			borderWidth: 1,
			borderColor: colors.warningBorder,
			backgroundColor: colors.warningSurface,
			borderRadius: 12,
			padding: 12,
			gap: 10,
		},
		title: { fontSize: 14, fontWeight: "700", color: colors.warning },
		subtitle: { fontSize: 13, color: colors.textSecondary },
		field: { gap: 6 },
		fieldTitle: { fontSize: 14, fontWeight: "600", color: colors.text },
		fieldHint: { fontSize: 12, color: colors.textSecondary },
		fieldError: { fontSize: 12, color: colors.danger },
		options: { gap: 6 },
		inlineButtons: { flexDirection: "row", gap: 8 },
		option: {
			borderWidth: 1,
			borderColor: colors.inputBorder,
			borderRadius: 8,
			paddingHorizontal: 10,
			paddingVertical: 8,
			backgroundColor: colors.inputBackground,
		},
		optionActive: { borderColor: colors.accent, backgroundColor: colors.surfaceSubtle },
		optionLabel: { fontSize: 14, color: colors.text },
		optionLabelActive: { fontWeight: "600", color: colors.accent },
		optionHint: { fontSize: 12, color: colors.textSecondary, marginTop: 2 },
		input: {
			borderWidth: 1,
			borderColor: colors.inputBorder,
			borderRadius: 8,
			paddingHorizontal: 10,
			paddingVertical: 8,
			fontSize: 14,
			backgroundColor: colors.inputBackground,
			color: colors.text,
		},
		customRow: { flexDirection: "row", gap: 8, alignItems: "center" },
		customInput: { flex: 1 },
		buttons: { flexDirection: "row", gap: 8, marginTop: 2 },
		primaryButton: {
			backgroundColor: colors.action,
			borderRadius: 8,
			paddingHorizontal: 14,
			paddingVertical: 10,
			alignItems: "center",
			justifyContent: "center",
		},
		primaryButtonText: { color: colors.actionText, fontWeight: "600" },
		secondaryButton: {
			borderWidth: 1,
			borderColor: colors.borderStrong,
			borderRadius: 8,
			paddingHorizontal: 12,
			paddingVertical: 8,
			alignItems: "center",
			justifyContent: "center",
		},
		secondaryButtonText: { color: colors.text, fontWeight: "600" },
		skipButton: {
			borderWidth: 1,
			borderColor: colors.danger,
			borderRadius: 8,
			paddingHorizontal: 12,
			paddingVertical: 10,
			alignItems: "center",
			justifyContent: "center",
		},
		skipButtonText: { color: colors.danger, fontWeight: "600" },
		disabled: { opacity: 0.5 },
		error: { fontSize: 12, color: colors.danger },
	});
}
