"use client";

import type { AutomationItem } from "@leros/store";
import { useAutomationStore } from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@leros/ui/components/ui/dialog";
import { Label } from "@leros/ui/components/ui/label";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@leros/ui/components/ui/select";
import { Switch } from "@leros/ui/components/ui/switch";
import { Clock } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import {
	type AutomationScheduleFormState,
	buildFormSummary,
	buildScheduleFormState,
	buildScheduleRequest,
	computeNextRunPreview,
	DEFAULT_SCHEDULE_FORM,
	getBrowserTimezone,
} from "./automationForm";

const WEEKDAY_OPTIONS = [
	{ label: "周一", value: 1 },
	{ label: "周二", value: 2 },
	{ label: "周三", value: 3 },
	{ label: "周四", value: 4 },
	{ label: "周五", value: 5 },
	{ label: "周六", value: 6 },
	{ label: "周日", value: 0 },
];

type CombinedPreset = "daily" | "weekly" | "monthly" | "interval";

export type AutomationFormDialogProps = {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	/** 传入 editTarget 表示编辑，否则为创建 */
	editTarget?: AutomationItem | null;
	onClose?: () => void;
};

export function AutomationFormDialog({
	open,
	onOpenChange,
	editTarget,
	onClose,
}: AutomationFormDialogProps) {
	const { createAutomation, updateAutomation } = useAutomationStore((s) => s);
	const [form, setForm] = useState<AutomationScheduleFormState>(DEFAULT_SCHEDULE_FORM);
	const [name, setName] = useState("");
	const [instruction, setInstruction] = useState("");
	const [enabled, setEnabled] = useState(true);
	const [timezone, setTimezone] = useState(getBrowserTimezone());
	const [submitting, setSubmitting] = useState(false);

	const isEdit = Boolean(editTarget);
	const nextRunPreview = useMemo(() => computeNextRunPreview(form), [form]);

	useEffect(() => {
		if (!open) return;
		if (editTarget) {
			setName(editTarget.name);
			setInstruction(editTarget.instruction ?? "");
			setEnabled(editTarget.enabled);
			// 编辑时使用服务端已保存的时区，不覆盖已有配置
			setTimezone(editTarget.timezone || getBrowserTimezone());
			setForm(buildScheduleFormState(editTarget.formConfig));
		} else {
			// 创建时使用浏览器 IANA 时区
			setName("");
			setInstruction("");
			setEnabled(true);
			setTimezone(getBrowserTimezone());
			setForm(DEFAULT_SCHEDULE_FORM);
		}
	}, [open, editTarget]);

	const formValid = name.trim().length > 0 && instruction.trim().length > 0;

	const handleSubmit = async () => {
		if (!formValid || submitting) {
			toast.error("请填写名称和任务指令");
			return;
		}
		if (form.mode === "interval" && form.interval.intervalMinutes < 5) {
			toast.error("固定间隔最少 5 分钟");
			return;
		}
		const schedule = buildScheduleRequest(form, timezone);
		setSubmitting(true);
		try {
			if (isEdit && editTarget) {
				const ok = await updateAutomation(editTarget.publicId, {
					name: name.trim(),
					instruction: instruction.trim(),
					enabled,
					schedule_mode: form.mode,
					schedule,
					timezone,
				});
				if (!ok) {
					toast.error("保存失败，请稍后重试");
					return;
				}
				toast.success("自动化已更新");
			} else {
				const created = await createAutomation({
					name: name.trim(),
					instruction: instruction.trim(),
					enabled,
					schedule_mode: form.mode,
					schedule,
					timezone,
				});
				if (!created) {
					toast.error("创建失败，请稍后重试");
					return;
				}
				toast.success("自动化已创建");
			}
			handleClose();
		} finally {
			setSubmitting(false);
		}
	};

	const handleClose = () => {
		setSubmitting(false);
		onOpenChange(false);
		onClose?.();
	};

	const setCalendar = <K extends keyof AutomationScheduleFormState["calendar"]>(
		key: K,
		value: AutomationScheduleFormState["calendar"][K],
	) => {
		setForm((prev) => ({ ...prev, calendar: { ...prev.calendar, [key]: value } }));
	};
	const setInterval = <K extends keyof AutomationScheduleFormState["interval"]>(
		key: K,
		value: AutomationScheduleFormState["interval"][K],
	) => {
		setForm((prev) => ({ ...prev, interval: { ...prev.interval, [key]: value } }));
	};

	const combinedPreset: CombinedPreset =
		form.mode === "interval"
			? "interval"
			: form.calendar.preset === "weekly"
				? "weekly"
				: form.calendar.preset === "monthly"
					? "monthly"
					: "daily";

	const handlePresetChange = (nextPreset: CombinedPreset) => {
		if (nextPreset === "interval") {
			setForm((prev) => ({ ...prev, mode: "interval" }));
		} else {
			setForm((prev) => ({
				...prev,
				mode: "calendar",
				calendar: {
					...prev.calendar,
					preset: nextPreset,
				},
			}));
		}
	};

	const currentDayOfWeek = form.calendar.daysOfWeek[0] ?? 1;
	const currentDayOfMonth = form.calendar.daysOfMonth[0] ?? 1;

	const timeString = `${String(form.calendar.hour).padStart(2, "0")}:${String(form.calendar.minute).padStart(2, "0")}`;

	const handleTimeChange = (val: string) => {
		if (!val) return;
		const [hStr, mStr] = val.split(":");
		const h = Number(hStr);
		const m = Number(mStr);
		if (!Number.isNaN(h) && !Number.isNaN(m)) {
			setCalendar("hour", h);
			setCalendar("minute", m);
		}
	};

	return (
		<Dialog
			open={open}
			onOpenChange={(nextOpen) => {
				if (!nextOpen && !submitting) handleClose();
			}}
		>
			<DialogContent className="flex max-h-[min(90dvh,720px)] max-w-[min(92vw,540px)] flex-col overflow-hidden p-0 sm:rounded-2xl shadow-xl">
				{/* 标头 */}
				<DialogHeader className="border-b border-slate-200/80 px-6 py-4">
					<DialogTitle className="text-base font-semibold text-slate-900">
						{isEdit ? "编辑自动化" : "创建自动化"}
					</DialogTitle>
					<DialogDescription className="mt-0.5 text-xs text-slate-400">
						配置 Agent 指令的执行规则，由 SingerOS 自动托管运行
					</DialogDescription>
				</DialogHeader>

				{/* 表单内容区 */}
				<div className="min-h-0 flex-1 overflow-y-auto px-6 py-5">
					<div className="space-y-4">
						{/* 名称 */}
						<div className="space-y-1.5">
							<span className="text-xs font-normal text-slate-700">
								自动化名称 <span className="text-red-500">*</span>
							</span>
							<div className="relative">
								<input
									type="text"
									value={name}
									onChange={(e) => setName(e.target.value)}
									placeholder="例如：AI 热点日报"
									maxLength={50}
									className="h-9 w-full rounded-lg border border-slate-200 bg-white pl-3 pr-14 text-sm font-normal text-slate-800 placeholder:text-slate-400 transition-colors focus:border-[#4f46e5] focus:outline-none"
								/>
								<span className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-xs text-slate-400">
									{name.length}/50
								</span>
							</div>
						</div>

						{/* 指令 */}
						<div className="space-y-1.5">
							<span className="text-xs font-normal text-slate-700">
								任务指令 <span className="text-red-500">*</span>
							</span>
							<textarea
								value={instruction}
								onChange={(e) => setInstruction(e.target.value)}
								placeholder="每轮执行时发送给 Agent 的完整指令"
								rows={3}
								className="w-full resize-none rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm font-normal text-slate-800 placeholder:text-slate-400 transition-colors focus:border-[#4f46e5] focus:outline-none"
							/>
						</div>

						{/* 执行周期 */}
						<div className="space-y-2">
							<div className="flex items-center justify-between">
								<span className="text-xs font-normal text-slate-700">
									执行周期 <span className="text-red-500">*</span>
								</span>
								<div className="flex items-center gap-1.5 text-xs text-slate-400">
									<Clock className="size-3.5" />
									<span>时区：{timezone || "系统默认"}</span>
								</div>
							</div>

							<div className="flex w-full items-center gap-2">
								<Select
									value={combinedPreset}
									onValueChange={(v) => handlePresetChange(v as CombinedPreset)}
								>
									<SelectTrigger className="!h-9 w-[120px] shrink-0 rounded-lg border-slate-200 bg-white px-3 text-sm font-normal text-slate-800 focus:border-[#4f46e5]">
										<SelectValue>
											{combinedPreset === "daily"
												? "每天执行"
												: combinedPreset === "weekly"
													? "每周执行"
													: combinedPreset === "monthly"
														? "每月执行"
														: "按固定间隔"}
										</SelectValue>
									</SelectTrigger>
									<SelectContent>
										<SelectItem value="daily">每天执行</SelectItem>
										<SelectItem value="weekly">每周执行</SelectItem>
										<SelectItem value="monthly">每月执行</SelectItem>
										<SelectItem value="interval">按固定间隔</SelectItem>
									</SelectContent>
								</Select>

								{combinedPreset === "daily" && (
									<>
										<span className="shrink-0 text-sm font-normal text-slate-700">每天</span>
										<div className="relative flex min-w-0 flex-1 items-center">
											<input
												type="time"
												value={timeString}
												onChange={(e) => handleTimeChange(e.target.value)}
												className="h-9 w-full rounded-lg border border-slate-200 bg-white pl-3 pr-8 text-sm font-normal text-slate-800 shadow-none transition-colors hover:border-slate-300 focus:border-[#4f46e5] focus:outline-none cursor-pointer [&::-webkit-calendar-picker-indicator]:opacity-0"
											/>
											<Clock className="pointer-events-none absolute right-2.5 size-4 text-slate-400" />
										</div>
										<span className="shrink-0 text-sm font-normal text-slate-700">执行</span>
									</>
								)}

								{combinedPreset === "weekly" && (
									<>
										<span className="shrink-0 text-sm font-normal text-slate-700">每周</span>
										<Select
											value={String(currentDayOfWeek)}
											onValueChange={(v) => setCalendar("daysOfWeek", [Number(v)])}
										>
											<SelectTrigger className="!h-9 flex-1 min-w-0 rounded-lg border-slate-200 bg-white px-3 text-sm font-normal text-slate-800 focus:border-[#4f46e5]">
												<SelectValue>
													{WEEKDAY_OPTIONS.find((w) => w.value === currentDayOfWeek)?.label ??
														"周一"}
												</SelectValue>
											</SelectTrigger>
											<SelectContent>
												{WEEKDAY_OPTIONS.map((w) => (
													<SelectItem key={w.value} value={String(w.value)}>
														{w.label}
													</SelectItem>
												))}
											</SelectContent>
										</Select>
										<div className="relative flex min-w-0 flex-1 items-center">
											<input
												type="time"
												value={timeString}
												onChange={(e) => handleTimeChange(e.target.value)}
												className="h-9 w-full rounded-lg border border-slate-200 bg-white pl-3 pr-8 text-sm font-normal text-slate-800 shadow-none transition-colors hover:border-slate-300 focus:border-[#4f46e5] focus:outline-none cursor-pointer [&::-webkit-calendar-picker-indicator]:opacity-0"
											/>
											<Clock className="pointer-events-none absolute right-2.5 size-4 text-slate-400" />
										</div>
										<span className="shrink-0 text-sm font-normal text-slate-700">执行</span>
									</>
								)}

								{combinedPreset === "monthly" && (
									<>
										<span className="shrink-0 text-sm font-normal text-slate-700">每月</span>
										<Select
											value={String(currentDayOfMonth)}
											onValueChange={(v) => setCalendar("daysOfMonth", [Number(v)])}
										>
											<SelectTrigger className="!h-9 flex-1 min-w-0 rounded-lg border-slate-200 bg-white px-3 text-sm font-normal text-slate-800 focus:border-[#4f46e5]">
												<SelectValue>{`${currentDayOfMonth}日`}</SelectValue>
											</SelectTrigger>
											<SelectContent>
												{Array.from({ length: 31 }, (_, i) => i + 1).map((d) => (
													<SelectItem key={d} value={String(d)}>
														{d}日
													</SelectItem>
												))}
											</SelectContent>
										</Select>
										<div className="relative flex min-w-0 flex-1 items-center">
											<input
												type="time"
												value={timeString}
												onChange={(e) => handleTimeChange(e.target.value)}
												className="h-9 w-full rounded-lg border border-slate-200 bg-white pl-3 pr-8 text-sm font-normal text-slate-800 shadow-none transition-colors hover:border-slate-300 focus:border-[#4f46e5] focus:outline-none cursor-pointer [&::-webkit-calendar-picker-indicator]:opacity-0"
											/>
											<Clock className="pointer-events-none absolute right-2.5 size-4 text-slate-400" />
										</div>
										<span className="shrink-0 text-sm font-normal text-slate-700">执行</span>
									</>
								)}

								{combinedPreset === "interval" && (
									<>
										<span className="shrink-0 text-sm font-normal text-slate-700">每</span>
										<input
											type="number"
											min={5}
											value={form.interval.intervalMinutes}
											onChange={(e) => setInterval("intervalMinutes", Number(e.target.value) || 5)}
											className="h-9 flex-1 min-w-0 rounded-lg border border-slate-200 bg-white px-3 text-sm font-normal text-slate-800 focus:border-[#4f46e5] focus:outline-none"
										/>
										<span className="shrink-0 text-sm font-normal text-slate-700">分钟执行</span>
									</>
								)}
							</div>
						</div>

						{/* 周期摘要与预览卡片 */}
						<div className="space-y-1 rounded-xl border border-slate-200/60 bg-slate-50/70 p-3.5 text-xs text-slate-500">
							<p className="flex items-center gap-1.5">
								<span className="text-slate-400">周期摘要：</span>
								<span className="font-normal text-slate-800">{buildFormSummary(form) || "—"}</span>
							</p>
							<p className="flex items-center gap-1.5">
								<span className="text-slate-400">下一次执行：</span>
								<span className="font-normal text-slate-800">
									{nextRunPreview ? formatISO(nextRunPreview) : "—"}
								</span>
								<span className="text-slate-400">（以服务端为准）</span>
							</p>
						</div>
					</div>
				</div>

				{/* 底部按钮栏 */}
				<DialogFooter className="flex items-center border-t border-slate-200/80 px-6 py-3.5">
					<div className="mr-auto flex items-center gap-2">
						<Switch checked={enabled} onCheckedChange={setEnabled} id="auto-enabled" />
						<Label
							htmlFor="auto-enabled"
							className="cursor-pointer text-xs font-normal text-slate-700"
						>
							创建后启用
						</Label>
					</div>
					<Button
						variant="outline"
						size="sm"
						onClick={handleClose}
						disabled={submitting}
						className="rounded-xl px-4"
					>
						取消
					</Button>
					<Button
						size="sm"
						onClick={handleSubmit}
						disabled={!formValid || submitting}
						className="rounded-xl bg-slate-900 text-white hover:bg-slate-800 px-5"
					>
						{submitting ? "保存中…" : isEdit ? "保存" : "创建"}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

function formatISO(date: Date): string {
	const pad = (n: number) => String(n).padStart(2, "0");
	return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}
