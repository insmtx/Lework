"use client";

import {
	applyCustom,
	buildAnswer,
	FormValidationError,
	initialAnswers,
	validateForm,
	visibleFields,
} from "@lework/api";
import { Button } from "@lework/ui/components/ui/button";
import { Checkbox } from "@lework/ui/components/ui/checkbox";
import { Input } from "@lework/ui/components/ui/input";
import { Loader2 } from "lucide-react";
import { useState } from "react";
import type { FormAnswerValue, FormField, PendingForm } from "../types";

interface FormCardProps {
	form: PendingForm;
	/** 节点离线时禁用控件，避免把必然失败的请求发出去。 */
	disabled?: boolean;
	onSubmit: (answer: Record<string, FormAnswerValue>) => Promise<void>;
	onCancel: () => Promise<void>;
}

function externalURL(url: string | undefined): string | null {
	if (!url) return null;
	// 只放行 http(s)：javascript: 与自定义 scheme 可能被远端表单用来执行代码。
	return /^https?:\/\//i.test(url) ? url : null;
}

/**
 * FormCard 渲染一张 OpenCode question 表单。
 *
 * 会话页与 global 表单共用：校验、显隐与答案构建全部来自 `@lework/api`，
 * 组件只收集输入，保证 Web 与移动端对同一张表单得出相同结论。
 */
export function FormCard({ form, disabled, onSubmit, onCancel }: FormCardProps) {
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
	const locked = disabled || busy;

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
				setError(caught instanceof Error ? caught.message : "Unable to submit");
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
			setError(caught instanceof Error ? caught.message : "Unable to skip");
		} finally {
			setBusy(false);
		}
	};

	const renderField = (field: FormField) => {
		const value = answers[field.key];
		const link = field.type === "external" ? externalURL(field.url) : null;

		return (
			<div key={field.key} className="space-y-1.5">
				<div className="text-sm font-medium">{field.title || field.key}</div>
				{field.description ? (
					<p className="text-xs text-muted-foreground">{field.description}</p>
				) : null}

				{field.type === "string" && field.options?.length ? (
					<div className="flex flex-wrap gap-1.5">
						{field.options.map((option) => (
							<Button
								key={option.value}
								type="button"
								size="sm"
								variant={value === option.value ? "default" : "outline"}
								disabled={locked}
								onClick={() => setAnswer(field.key, option.value)}
							>
								{option.label}
							</Button>
						))}
					</div>
				) : null}

				{field.type === "string" && !field.options?.length ? (
					<Input
						value={typeof value === "string" ? value : ""}
						placeholder={field.placeholder}
						disabled={locked}
						onChange={(event) => setAnswer(field.key, event.target.value)}
					/>
				) : null}

				{field.type === "string" && field.options?.length && field.custom ? (
					<div className="flex gap-1.5">
						<Input
							value={custom[field.key] ?? ""}
							placeholder={field.placeholder || "Custom answer"}
							disabled={locked}
							onChange={(event) =>
								setCustom((previous) => ({ ...previous, [field.key]: event.target.value }))
							}
						/>
						<Button
							type="button"
							size="sm"
							variant="outline"
							disabled={locked}
							onClick={() =>
								setAnswer(field.key, applyCustom(field, value, custom[field.key] ?? ""))
							}
						>
							Add
						</Button>
					</div>
				) : null}

				{field.type === "multiselect" ? (
					<div className="space-y-1.5">
						<div className="flex flex-wrap gap-1.5">
							{(field.options ?? []).map((option) => {
								const selected = Array.isArray(value) && value.includes(option.value);
								return (
									<Button
										key={option.value}
										type="button"
										size="sm"
										variant={selected ? "default" : "outline"}
										disabled={locked}
										onClick={() => {
											const current = Array.isArray(value) ? value : [];
											setAnswer(
												field.key,
												selected
													? current.filter((item) => item !== option.value)
													: [...current, option.value],
											);
										}}
									>
										{option.label}
									</Button>
								);
							})}
						</div>
						{field.custom ? (
							<div className="flex gap-1.5">
								<Input
									value={custom[field.key] ?? ""}
									placeholder={field.placeholder || "Custom option"}
									disabled={locked}
									onChange={(event) =>
										setCustom((previous) => ({ ...previous, [field.key]: event.target.value }))
									}
								/>
								<Button
									type="button"
									size="sm"
									variant="outline"
									disabled={locked}
									onClick={() => {
										setAnswer(field.key, applyCustom(field, value, custom[field.key] ?? "") ?? []);
										setCustom((previous) => ({ ...previous, [field.key]: "" }));
									}}
								>
									Add
								</Button>
							</div>
						) : null}
					</div>
				) : null}

				{field.type === "boolean" ? (
					<label
						htmlFor={`form-${form.id}-${field.key}`}
						className="flex items-center gap-2 text-sm"
					>
						<Checkbox
							id={`form-${form.id}-${field.key}`}
							checked={value === true}
							disabled={locked}
							onCheckedChange={(checked) => setAnswer(field.key, checked)}
						/>
						Yes
					</label>
				) : null}

				{field.type === "number" || field.type === "integer" ? (
					<Input
						inputMode="decimal"
						value={typeof value === "number" || typeof value === "string" ? String(value) : ""}
						placeholder={field.placeholder}
						disabled={locked}
						onChange={(event) => {
							const trimmed = event.target.value.trim();
							if (trimmed === "") {
								setAnswer(field.key, undefined);
								return;
							}
							const parsed = Number(trimmed);
							// 非法输入保留原文，交给 validateForm 给出统一提示。
							setAnswer(field.key, Number.isNaN(parsed) ? trimmed : parsed);
						}}
					/>
				) : null}

				{field.type === "external" ? (
					<div className="space-y-1.5">
						{link ? (
							<a
								href={link}
								target="_blank"
								rel="noreferrer"
								className="text-xs font-medium text-primary underline underline-offset-2"
							>
								Open link
							</a>
						) : null}
						<label
							htmlFor={`form-${form.id}-${field.key}`}
							className="flex items-center gap-2 text-sm"
						>
							<Checkbox
								id={`form-${form.id}-${field.key}`}
								checked={value === true}
								disabled={locked}
								onCheckedChange={(checked) => setAnswer(field.key, checked)}
							/>
							I opened and confirmed
						</label>
					</div>
				) : null}

				{errors[field.key] ? <p className="text-xs text-destructive">{errors[field.key]}</p> : null}
			</div>
		);
	};

	return (
		<div className="space-y-3 rounded-xl border border-amber-300/70 bg-amber-50 p-3 text-sm shadow-sm dark:border-amber-900/60 dark:bg-amber-950/30">
			<div>
				<div className="font-medium text-amber-950 dark:text-amber-100">Answer required</div>
				{form.title && form.title !== "Questions" ? (
					<p className="mt-0.5 text-xs text-amber-900/80 dark:text-amber-200/80">{form.title}</p>
				) : null}
			</div>

			{fields.map(renderField)}

			{error ? <p className="text-xs text-destructive">{error}</p> : null}

			<div className="flex flex-wrap gap-2 pt-1">
				<Button size="sm" disabled={locked} onClick={() => void submit()}>
					{busy ? <Loader2 className="size-3.5 animate-spin" /> : null}
					Submit
				</Button>
				<Button
					size="sm"
					variant="outline"
					className="text-destructive"
					disabled={locked}
					onClick={() => void cancel()}
				>
					Skip
				</Button>
			</div>
		</div>
	);
}
