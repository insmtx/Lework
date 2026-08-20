"use client";

import type { AutomationItem } from "@leros/store";
import {
	prepareOutgoingComposer,
	skillChipsToComposerState,
	useAutomationStore,
	useLayoutStore,
} from "@leros/store";
import type { ComposerToken } from "@leros/store/types/chat";
import { Button } from "@leros/ui/components/ui/button";
import {
	Command,
	CommandGroup,
	CommandInput,
	CommandItem,
	CommandList,
	CommandSeparator,
} from "@leros/ui/components/ui/command";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@leros/ui/components/ui/dialog";
import {
	DropdownMenu,
	DropdownMenuCheckboxItem,
	DropdownMenuContent,
	DropdownMenuTrigger,
} from "@leros/ui/components/ui/dropdown-menu";
import { Label } from "@leros/ui/components/ui/label";
import { Popover, PopoverContent, PopoverTrigger } from "@leros/ui/components/ui/popover";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@leros/ui/components/ui/select";
import { Switch } from "@leros/ui/components/ui/switch";
import { Check, ChevronsUpDown, Clock, FolderOpen } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { StructuredComposer, type StructuredComposerHandle } from "../input/StructuredComposer";
import { useComposerSkillOptions } from "../input/useComposerSkillOptions";
import { ProjectIcon } from "../layout/project-icon";
import {
	type AutomationScheduleFormState,
	buildFormSummary,
	buildScheduleFormState,
	buildScheduleRequest,
	computeNextRunPreview,
	DEFAULT_SCHEDULE_FORM,
	formatSelectionPreview,
	getBrowserTimezone,
	toggleCalendarArray,
	WEEKDAY_LABELS,
} from "./automationForm";
import { formatLocalDateTime } from "./automationTime";

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
	const projects = useLayoutStore((s) => s.projects);
	const fetchProjects = useLayoutStore((s) => s.fetchProjects);
	const [form, setForm] = useState<AutomationScheduleFormState>(DEFAULT_SCHEDULE_FORM);
	const [name, setName] = useState("");
	const [instruction, setInstruction] = useState("");
	const [instructionPrefill, setInstructionPrefill] = useState<{
		id: string;
		value: string;
		tokens: ComposerToken[];
	}>();
	const composerRef = useRef<StructuredComposerHandle>(null);
	const [enabled, setEnabled] = useState(true);
	const [timezone, setTimezone] = useState(getBrowserTimezone());
	const [submitting, setSubmitting] = useState(false);
	// 关联项目："" 表示"新项目"，否则为已选项目 public_id
	const [selectedProjectId, setSelectedProjectId] = useState("");
	const [projectSearch, setProjectSearch] = useState("");
	const [projectPickerOpen, setProjectPickerOpen] = useState(false);
	const [projectListLoading, setProjectListLoading] = useState(false);
	const [projectListReady, setProjectListReady] = useState(false);

	const isEdit = Boolean(editTarget);
	const { skillOptions, skillsLoading } = useComposerSkillOptions(selectedProjectId || null, open);
	const nextRunPreview = useMemo(() => computeNextRunPreview(form, timezone), [form, timezone]);

	// ListProjects 已按当前用户的项目绑定过滤；关联项目的最终可用性仍由保存接口权威校验。
	// 不在弹窗中二次权限过滤，避免首次打开时因批量权限请求未完成而隐藏完整项目列表。
	useEffect(() => {
		if (!open) {
			setProjectListLoading(false);
			setProjectListReady(false);
			return;
		}

		let active = true;
		setProjectListLoading(true);
		setProjectListReady(false);
		void fetchProjects()
			.then((succeeded) => {
				if (!active) return;
				setProjectListReady(succeeded);
				setProjectListLoading(false);
			})
			.catch(() => {
				if (!active) return;
				setProjectListReady(false);
				setProjectListLoading(false);
			});

		return () => {
			active = false;
		};
	}, [fetchProjects, open]);

	// 已有项目按搜索词过滤。列表接口已确保当前用户可见，服务端在保存时继续校验权限和 AI 队友绑定。
	const existingProjects = useMemo(() => {
		const q = projectSearch.trim().toLowerCase();
		return projects.filter((p) => !q || p.name.toLowerCase().includes(q));
	}, [projectSearch, projects]);

	// 当前选中项目的展示名（用于触发按钮）；未选中显示"新项目"
	const selectedProjectLabel = useMemo(() => {
		if (!selectedProjectId) return "";
		return projects.find((p) => p.id === selectedProjectId)?.name ?? editTarget?.projectName ?? "";
	}, [editTarget?.projectName, projects, selectedProjectId]);

	// 编辑时选中了项目但该项目已不在用户可访问的项目列表中 => 提示改选
	const selectedProjectIsUnavailable = useMemo(() => {
		if (!isEdit || !editTarget?.projectPublicId) return false;
		if (!projectListReady || projectListLoading) return false;
		const pid = editTarget.projectPublicId;
		return !projects.some((project) => project.id === pid);
	}, [editTarget?.projectPublicId, isEdit, projectListLoading, projectListReady, projects]);

	// 编辑且存在活动执行时禁止更换项目
	const projectDisabled = Boolean(editTarget?.hasActiveExecution);

	useEffect(() => {
		if (!open) return;
		const prefillId = `automation-instruction-${editTarget?.publicId ?? "new"}-${Date.now()}`;
		if (editTarget) {
			const restored = skillChipsToComposerState(editTarget.instruction ?? "");
			setName(editTarget.name);
			setInstruction(restored.value);
			setInstructionPrefill({
				id: prefillId,
				value: restored.value,
				tokens: restored.tokens,
			});
			setEnabled(editTarget.enabled);
			// 编辑时使用服务端已保存的时区，不覆盖已有配置
			setTimezone(editTarget.timezone || getBrowserTimezone());
			setForm(buildScheduleFormState(editTarget.formConfig));
			setSelectedProjectId(editTarget.projectPublicId ?? "");
		} else {
			// 创建时使用浏览器 IANA 时区
			setName("");
			setInstruction("");
			setInstructionPrefill({ id: prefillId, value: "", tokens: [] });
			setEnabled(true);
			setTimezone(getBrowserTimezone());
			setForm(DEFAULT_SCHEDULE_FORM);
			setSelectedProjectId("");
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
		const prepared = prepareOutgoingComposer(
			instruction,
			composerRef.current?.getComposerTokens() ?? [],
		);
		if (!prepared.content.trim()) {
			toast.error("请填写名称和任务指令");
			return;
		}
		setSubmitting(true);
		try {
			if (isEdit && editTarget) {
				// 仅当关联发生变化时提交 project_public_id；切回默认时显式提交空串
				const prevProject = editTarget.projectPublicId ?? "";
				const projectChanged = selectedProjectId !== prevProject;
				const params: Parameters<typeof updateAutomation>[1] = {
					name: name.trim(),
					instruction: prepared.content,
					enabled,
					schedule_mode: form.mode,
					schedule,
					timezone,
				};
				if (projectChanged) {
					params.project_public_id = selectedProjectId;
				}
				const res = await updateAutomation(editTarget.publicId, params);
				if (!res.ok) {
					showProjectSaveError(res.status, "保存失败，请稍后重试");
					return;
				}
				toast.success("自动化已更新");
			} else {
				// 仅当选择已有项目时提交 project_public_id；选"新项目"不发送
				const params: Parameters<typeof createAutomation>[0] = {
					name: name.trim(),
					instruction: prepared.content,
					enabled,
					schedule_mode: form.mode,
					schedule,
					timezone,
				};
				if (selectedProjectId) {
					params.project_public_id = selectedProjectId;
				}
				const res = await createAutomation(params);
				if (!res.ok) {
					showProjectSaveError(res.status, "创建失败，请稍后重试");
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

	/** 依据 HTTP 状态展示关联项目/保存失败的明确提示。 */
	const showProjectSaveError = (status: number | undefined, fallback: string) => {
		if (status === 409) {
			toast.error("存在进行中的执行，执行结束后可修改项目");
			return;
		}
		if (status === 403) {
			toast.error("无权使用该关联项目，请重新选择");
			return;
		}
		if (status === 400) {
			toast.error("关联项目当前不可用（AI 队友未在其中），请改选");
			return;
		}
		toast.error(fallback);
	};

	const setCalendar = <K extends keyof AutomationScheduleFormState["calendar"]>(
		key: K,
		value: AutomationScheduleFormState["calendar"][K],
	) => {
		setForm((prev) => ({
			...prev,
			calendar: { ...prev.calendar, [key]: value },
		}));
	};
	const setInterval = <K extends keyof AutomationScheduleFormState["interval"]>(
		key: K,
		value: AutomationScheduleFormState["interval"][K],
	) => {
		setForm((prev) => ({
			...prev,
			interval: { ...prev.interval, [key]: value },
		}));
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

	const weekdayPreview = formatSelectionPreview(
		form.calendar.daysOfWeek,
		(d) => WEEKDAY_LABELS[d] ?? "",
	);
	const monthDaysPreview = formatSelectionPreview(form.calendar.daysOfMonth, (d) => `${d}日`);

	const handleToggleDayOfWeek = useCallback((value: number) => {
		setForm((prev) => ({
			...prev,
			calendar: {
				...prev.calendar,
				daysOfWeek: toggleCalendarArray(prev.calendar.daysOfWeek, value, {
					order: "weekday",
				}),
			},
		}));
	}, []);
	const handleToggleDayOfMonth = useCallback((value: number) => {
		setForm((prev) => ({
			...prev,
			calendar: {
				...prev.calendar,
				daysOfMonth: toggleCalendarArray(prev.calendar.daysOfMonth, value, {
					order: "numeric",
				}),
			},
		}));
	}, []);

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
						配置 Agent 指令的执行规则，由 Lework 自动托管运行
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
							<div className="rounded-lg border border-slate-200 bg-white transition-colors focus-within:border-[#4f46e5] focus-within:outline-none">
								<StructuredComposer
									ref={composerRef}
									value={instruction}
									onChange={setInstruction}
									onSubmit={() => void handleSubmit()}
									onPasteFiles={(event) => event.preventDefault()}
									onFocus={() => undefined}
									onBlur={() => undefined}
									placeholder="每轮执行时发送给 Agent 的完整指令，输入 / 选择技能"
									isProjectVariant
									inputSize="compact"
									pickerPlacement="bottom"
									pickerSize="compact"
									skillOptions={skillOptions}
									skillsLoading={skillsLoading}
									prefill={instructionPrefill}
								/>
							</div>
						</div>

						{/* 关联项目（可选） */}
						<div className="space-y-1.5">
							<div className="flex items-center justify-between">
								<span className="text-xs font-normal text-slate-700">关联项目（可选）</span>
								{projectDisabled ? (
									<span className="text-xs text-amber-500">
										存在进行中的执行，执行结束后可修改项目
									</span>
								) : null}
							</div>
							<Popover open={projectPickerOpen} onOpenChange={setProjectPickerOpen}>
								<PopoverTrigger
									type="button"
									disabled={projectDisabled}
									className="flex h-9 w-full items-center justify-between gap-1.5 rounded-lg border border-slate-200 bg-white px-3 text-sm font-normal text-slate-800 transition-colors hover:border-slate-300 focus:border-[#4f46e5] focus:outline-none disabled:cursor-not-allowed disabled:bg-slate-50 disabled:text-slate-400"
								>
									<span className="flex min-w-0 items-center gap-2">
										<FolderOpen className="size-4 shrink-0 text-slate-400" />
										<span className="truncate">{selectedProjectLabel || "新项目"}</span>
									</span>
									<ChevronsUpDown className="size-4 shrink-0 text-slate-400" />
								</PopoverTrigger>
								{!projectDisabled && (
									<PopoverContent
										align="start"
										sideOffset={4}
										className="w-[min(360px,calc(100vw-2rem))] p-1.5"
									>
										<Command shouldFilter={false} className="rounded-xl! bg-transparent p-0">
											<div className="px-2 pb-1 pt-1 text-xs text-slate-400">
												首次执行时自动创建专属项目，或选择你所在的一个项目
											</div>
											<CommandInput
												value={projectSearch}
												onValueChange={setProjectSearch}
												placeholder="搜索项目"
											/>
											<CommandList className="max-h-60">
												<CommandGroup heading="新项目" className="p-0">
													<CommandItem
														value="__default__"
														onSelect={() => {
															setSelectedProjectId("");
															setProjectPickerOpen(false);
														}}
														className="flex items-center gap-2 rounded-lg px-2 py-2"
													>
														<ProjectIcon className="size-4 shrink-0 text-slate-400" />
														<span className="flex-1 truncate">新项目</span>
														{selectedProjectId === "" && (
															<Check className="size-4 text-[#4f46e5]" />
														)}
													</CommandItem>
												</CommandGroup>
												<CommandSeparator className="mx-1 my-1.5 bg-slate-200/80" />
												<CommandGroup heading="已有项目" className="p-0">
													{projectListLoading && (
														<p className="px-3 py-2 text-xs text-slate-400">正在加载已有项目...</p>
													)}
													{!projectListLoading && existingProjects.length === 0 && (
														<p className="px-3 py-4 text-center text-sm text-slate-400">
															没有已有项目
														</p>
													)}
													{existingProjects.map((p) => (
														<CommandItem
															key={p.id}
															value={p.id}
															onSelect={() => {
																setSelectedProjectId(p.id);
																setProjectPickerOpen(false);
															}}
															className="flex items-center gap-2 rounded-lg px-2 py-2"
														>
															<ProjectIcon className="size-4 shrink-0 text-slate-400" />
															<span className="flex-1 truncate">{p.name}</span>
															{selectedProjectId === p.id && (
																<Check className="size-4 text-[#4f46e5]" />
															)}
														</CommandItem>
													))}
												</CommandGroup>
											</CommandList>
										</Command>
									</PopoverContent>
								)}
							</Popover>
							{selectedProjectIsUnavailable && (
								<p className="text-xs text-amber-500">当前关联项目已不可访问，请选择新项目</p>
							)}
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
										<DropdownMenu>
											<DropdownMenuTrigger
												render={
													<button
														type="button"
														aria-label="选择星期"
														className="!h-9 flex min-w-0 flex-1 cursor-pointer items-center justify-between gap-1.5 rounded-lg border border-slate-200 bg-white px-3 text-sm font-normal text-slate-800 transition-colors hover:border-slate-300 focus:border-[#4f46e5] focus:outline-none"
													>
														<span className="truncate">{weekdayPreview || WEEKDAY_LABELS[1]}</span>
														<ChevronsUpDown className="size-4 shrink-0 text-slate-400" />
													</button>
												}
											/>
											<DropdownMenuContent
												align="start"
												sideOffset={4}
												className="max-h-64 w-[200px] overflow-y-auto"
											>
												{WEEKDAY_OPTIONS.map((w) => (
													<DropdownMenuCheckboxItem
														key={w.value}
														checked={form.calendar.daysOfWeek.includes(w.value)}
														onCheckedChange={() => handleToggleDayOfWeek(w.value)}
														disabled={
															form.calendar.daysOfWeek.length === 1 &&
															form.calendar.daysOfWeek.includes(w.value)
														}
														className="data-[disabled]:cursor-not-allowed data-[disabled]:opacity-40"
													>
														{w.label}
													</DropdownMenuCheckboxItem>
												))}
											</DropdownMenuContent>
										</DropdownMenu>
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
										<DropdownMenu>
											<DropdownMenuTrigger
												render={
													<button
														type="button"
														aria-label="选择日期"
														className="!h-9 flex min-w-0 flex-1 cursor-pointer items-center justify-between gap-1.5 rounded-lg border border-slate-200 bg-white px-3 text-sm font-normal text-slate-800 transition-colors hover:border-slate-300 focus:border-[#4f46e5] focus:outline-none"
													>
														<span className="truncate">{monthDaysPreview || "1日"}</span>
														<ChevronsUpDown className="size-4 shrink-0 text-slate-400" />
													</button>
												}
											/>
											<DropdownMenuContent
												align="start"
												sideOffset={4}
												className="max-h-64 w-[200px] overflow-y-auto"
											>
												{Array.from({ length: 31 }, (_, i) => i + 1).map((d) => (
													<DropdownMenuCheckboxItem
														key={d}
														checked={form.calendar.daysOfMonth.includes(d)}
														onCheckedChange={() => handleToggleDayOfMonth(d)}
														disabled={
															form.calendar.daysOfMonth.length === 1 &&
															form.calendar.daysOfMonth.includes(d)
														}
														className="data-[disabled]:cursor-not-allowed data-[disabled]:opacity-40"
													>
														{d}日
													</DropdownMenuCheckboxItem>
												))}
											</DropdownMenuContent>
										</DropdownMenu>
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
									{nextRunPreview ? formatLocalDateTime(nextRunPreview) : "—"}
								</span>
								<span className="text-slate-400"></span>
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
