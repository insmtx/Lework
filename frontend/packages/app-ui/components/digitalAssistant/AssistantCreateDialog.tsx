"use client";

import {
	type BackendAITeammateTemplate,
	type DigitalAssistantItem,
	digitalAssistantApi,
	getNativeFileInputAccept,
	projectFileApi,
	useDAStore,
} from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@leros/ui/components/ui/dialog";
import {
	Sheet,
	SheetContent,
	SheetFooter,
	SheetHeader,
	SheetTitle,
} from "@leros/ui/components/ui/sheet";
import { cn } from "@leros/ui/lib/utils";
import {
	ChartNoAxesCombined,
	FileSearch,
	ImagePlus,
	Lightbulb,
	Loader2,
	PenLine,
	WandSparkles,
} from "lucide-react";
import { type ChangeEvent, useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { blobToDataURL, cacheProtectedImageDataURL } from "../avatar/ProtectedImage";
import { AssistantAvatar } from "./AssistantAvatar";
import { ASSISTANT_FORM_LIMITS } from "./assistantFormLimits";

export type AssistantCreateDialogProps = {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	onCreated?: (assistant: DigitalAssistantItem) => void;
};

const PRESET_TEMPLATE_CODES = [
	"bid-strategist",
	"contract-review-expert",
	"data-analysis-expert",
	"document-generation-expert",
	"ai-ppt-expert",
	"recruiting-expert",
	"stock-investment-expert",
] as const;

const TEMPLATE_EXPERTISE_DESCRIPTIONS: Record<string, string[]> = {
	"bid-strategist": [
		"快速梳理招标范围、资格条件、交付要求和关键时间节点。",
		"定位显性与隐性评分标准，形成逐项响应和得分提升建议。",
		"结合竞争环境与自身优势，制定差异化投标路径和重点。",
		"按规范组织章节与内容，辅助产出清晰、完整且可落地的标书。",
	],
	"contract-review-expert": [
		"系统核对权责、履约、付款、违约和终止等关键条款。",
		"识别责任失衡、表述歧义与潜在争议，提示风险优先级。",
		"针对问题条款提供清晰、可协商的修改方向和参考表述。",
		"结合业务场景检查合规边界，并给出后续专业咨询建议。",
	],
	"data-analysis-expert": [
		"把业务目标拆成可衡量指标，明确口径、维度和分析路径。",
		"识别趋势、波动和异常变化，解释背后的可能原因。",
		"通过分组与对比定位经营问题，提炼可执行的业务结论。",
		"建议合适的图表和信息层级，让分析结果更易理解。",
	],
	"document-generation-expert": [
		"将零散材料整理成结构完整、重点清晰的正式报告。",
		"围绕目标搭建方案框架，补齐步骤、资源、风险和交付物。",
		"提炼会议讨论、决议与待办，形成便于跟进的纪要。",
		"按管理场景编写规范、清晰且方便执行的制度文档。",
	],
	"ai-ppt-expert": [
		"围绕汇报目标设计开场、展开与结论，建立清晰演示逻辑。",
		"把复杂材料拆成逐页大纲，明确每页核心信息与表达方式。",
		"优化标题和正文文案，让重点更突出、表达更有说服力。",
		"串联页面叙事与转场节奏，帮助汇报自然推进。",
	],
	"recruiting-expert": [
		"提炼岗位目标、职责与胜任特征，形成清晰的人才画像。",
		"优化职位描述的结构和表达，提升信息准确性与吸引力。",
		"按岗位要求提取简历证据，辅助完成候选人初步筛选。",
		"设计结构化问题与评价维度，支持更一致的面试判断。",
	],
	"stock-investment-expert": [
		"整理商业模式、财务表现与竞争优势，建立公司研究基础。",
		"梳理产业链、竞争格局和周期变化，判断行业发展脉络。",
		"识别经营、财务与市场风险，提示关键假设和不确定性。",
		"组织研究问题、证据与结论，形成可持续更新的分析框架。",
	],
};

export function AssistantCreateDialog({
	open,
	onOpenChange,
	onCreated,
}: AssistantCreateDialogProps) {
	const { assistants, createAssistant, createAssistantFromTemplate, updateAssistantStatus } =
		useDAStore((s) => s);
	const [templates, setTemplates] = useState<BackendAITeammateTemplate[]>([]);
	const [templatesLoading, setTemplatesLoading] = useState(false);
	const [selectedTemplate, setSelectedTemplate] = useState<BackendAITeammateTemplate | null>(null);
	const [detailTemplate, setDetailTemplate] = useState<BackendAITeammateTemplate | null>(null);
	const [customMode, setCustomMode] = useState(false);
	const [name, setName] = useState("");
	const [roleName, setRoleName] = useState("");
	const [introduction, setIntroduction] = useState("");
	const [avatar, setAvatar] = useState("");
	const [uploadingAvatar, setUploadingAvatar] = useState(false);
	const [previewAvatar, setPreviewAvatar] = useState<string | undefined>();
	const [submitting, setSubmitting] = useState(false);
	const [nameError, setNameError] = useState("");
	const [checkingName, setCheckingName] = useState(false);
	const selectionTouchedRef = useRef(false);
	const nameCheckSequenceRef = useRef(0);

	const validateName = useCallback(async () => {
		const sequence = ++nameCheckSequenceRef.current;
		const candidate = name.trim();
		if (!candidate) {
			setCheckingName(false);
			return false;
		}
		const normalizedName = candidate.toLocaleLowerCase();
		if (
			assistants.some((assistant) => assistant.name.trim().toLocaleLowerCase() === normalizedName)
		) {
			// 中文注释：本地已命中重名时不再发请求，避免同一个错误提示出现两次。
			setNameError("该 AI 队友名称已存在");
			setCheckingName(false);
			return false;
		}
		setCheckingName(true);
		try {
			const response = await digitalAssistantApi.checkName({ name: candidate });
			if (sequence !== nameCheckSequenceRef.current) return false;
			const available = response.data.data?.available ?? false;
			setNameError(available ? "" : "该 AI 队友名称已存在");
			return available;
		} catch (error) {
			// 中文注释：预检失败不阻断保存，保存接口仍会以服务端数据做最终校验。
			console.error("check ai teammate name error:", error);
			return true;
		} finally {
			if (sequence === nameCheckSequenceRef.current) setCheckingName(false);
		}
	}, [assistants, name]);

	useEffect(() => {
		// 中文注释：预设角色从后端模板读取，保证组织后续调整模板时创建入口无需重新发版。
		if (!open) return;
		let cancelled = false;
		selectionTouchedRef.current = false;
		setTemplatesLoading(true);
		void digitalAssistantApi
			.listTemplates({ status: "active", list_all: true, limit: 100 })
			.then((response) => {
				if (cancelled) return;
				const items = response.data.data?.items ?? [];
				const templatesByCode = new Map(items.map((item) => [item.code, item]));
				// 中文注释：创建入口只展示产品约定的七个内置角色，并保持固定顺序，不混入历史市场模板。
				const presetTemplates = PRESET_TEMPLATE_CODES.flatMap((code) => {
					const template = templatesByCode.get(code);
					return template ? [template] : [];
				});
				setTemplates(presetTemplates);
				// 中文注释：模板异步返回时，不覆盖用户已经切换的自定义模式或手动选择。
				if (presetTemplates[0] && !selectionTouchedRef.current) {
					selectTemplate(presetTemplates[0], false);
				}
			})
			.catch((error) => {
				if (cancelled) return;
				console.error("fetch ai teammate templates error:", error);
				toast.error("AI 队友模板加载失败");
			})
			.finally(() => {
				if (!cancelled) setTemplatesLoading(false);
			});
		return () => {
			cancelled = true;
		};
	}, [open]);

	const selectTemplate = (template: BackendAITeammateTemplate, markTouched = true) => {
		if (markTouched) selectionTouchedRef.current = true;
		setCustomMode(false);
		setSelectedTemplate(template);
		setRoleName(template.name);
		setIntroduction(template.description ?? "");
		setAvatar(template.avatar ?? "");
		setPreviewAvatar(undefined);
	};

	const switchToCustom = () => {
		selectionTouchedRef.current = true;
		setCustomMode(true);
		setSelectedTemplate(null);
		setName("");
		setRoleName("");
		setIntroduction("");
		setAvatar("");
		setPreviewAvatar(undefined);
		setDetailTemplate(null);
	};

	const formValid = Boolean(
		name.trim() && roleName.trim() && introduction.trim() && (customMode || selectedTemplate),
	);

	const handleSubmit = async () => {
		if (!formValid || submitting) {
			toast.error("请填写自定义名称、角色名称和简介");
			return;
		}
		if (!(await validateName())) return;
		setSubmitting(true);
		try {
			let assistant: DigitalAssistantItem | null;
			if (selectedTemplate) {
				assistant = await createAssistantFromTemplate({
					template_id: selectedTemplate.id,
					name: name.trim(),
					role_name: roleName.trim(),
					description: introduction.trim(),
					// 中文注释：模板实例允许覆盖头像，与后续编辑入口保持一致，其他模板配置仍由服务端继承。
					avatar: avatar.trim() || undefined,
				});
			} else {
				assistant = await createAssistant({
					name: name.trim(),
					role_name: roleName.trim(),
					avatar: avatar.trim() || undefined,
					// 中文注释：当前自定义创建只保留一个“简介”输入，同时用于卡片简介和角色设定。
					description: introduction.trim(),
					system_prompt: `你是${roleName.trim()}，名称是${name.trim()}。${introduction.trim()}`,
				});
				if (assistant) {
					const activated = await updateAssistantStatus(assistant.id, "active");
					if (!activated) {
						toast.error("AI 队友已创建，但启用失败；可稍后在列表中重试");
						handleClose();
						return;
					}
					assistant = { ...assistant, status: "active", deploymentStatus: "pending" };
				}
			}
			if (!assistant) {
				// 中文注释：保存时若被其他成员抢先使用名称，重新预检并将服务端结果落到同一字段提示。
				if (await validateName()) toast.error("创建队友失败");
				return;
			}
			toast.success("AI 队友创建中，请等待部署完成后再使用");
			onCreated?.(assistant);
			handleClose();
		} finally {
			setSubmitting(false);
		}
	};

	const handleClose = () => {
		setTemplates([]);
		setSelectedTemplate(null);
		setDetailTemplate(null);
		setCustomMode(false);
		setName("");
		setRoleName("");
		setIntroduction("");
		setAvatar("");
		setPreviewAvatar(undefined);
		setNameError("");
		setCheckingName(false);
		onOpenChange(false);
	};

	const handleAvatarChange = async (event: ChangeEvent<HTMLInputElement>) => {
		const file = event.target.files?.[0];
		event.target.value = "";
		if (!file) return;
		if (!isImageFile(file)) {
			toast.error("请选择图片文件");
			return;
		}
		const previewURL = URL.createObjectURL(file);
		setPreviewAvatar(previewURL);
		setUploadingAvatar(true);
		try {
			const response = await projectFileApi.uploadLoose({ file, purpose: "avatar" });
			const publicId = response.data?.public_id;
			if (!publicId) throw new Error("头像上传失败");
			// 中文注释：先写入本地缓存再移除 blob 预览，避免受保护图片异步加载时短暂回退为默认头像。
			const dataURL = await blobToDataURL(file);
			cacheProtectedImageDataURL(publicId, dataURL);
			setAvatar(publicId);
			setPreviewAvatar(undefined);
			toast.success("头像已上传");
		} catch (error) {
			console.error("upload ai teammate avatar error:", error);
			toast.error(error instanceof Error ? error.message : "头像上传失败");
			setPreviewAvatar(undefined);
		} finally {
			URL.revokeObjectURL(previewURL);
			setUploadingAvatar(false);
		}
	};

	return (
		<>
			<Dialog
				open={open}
				onOpenChange={(nextOpen) => {
					if (!nextOpen && !submitting && !uploadingAvatar) handleClose();
				}}
			>
				<DialogContent className="flex max-h-[min(92dvh,820px)] max-w-[min(94vw,960px)] flex-col overflow-hidden p-0 sm:rounded-2xl">
					<DialogHeader className="border-b border-slate-200 px-6 py-5">
						<DialogTitle>创建 AI 队友</DialogTitle>
						<DialogDescription>选择一个预设角色，或创建自定义 AI 队友</DialogDescription>
					</DialogHeader>
					<div className="min-h-0 flex-1 overflow-y-auto px-6 py-5">
						<div className="flex items-center justify-between">
							<h3 className="text-sm font-semibold text-slate-900">选择角色</h3>
							<Button type="button" size="sm" onClick={switchToCustom}>
								自定义 AI 队友
							</Button>
						</div>
						{templatesLoading ? (
							<div className="flex h-40 items-center justify-center text-sm text-slate-500">
								<Loader2 className="mr-2 size-4 animate-spin" />
								加载角色模板…
							</div>
						) : templates.length > 0 ? (
							<div className="mt-3 grid gap-3 md:grid-cols-2">
								{templates.map((template) => (
									<div
										key={template.id}
										className={cn(
											"flex items-center gap-3 rounded-xl border p-3 transition-colors",
											selectedTemplate?.id === template.id && !customMode
												? "border-[#4f46e5] bg-slate-50"
												: "border-slate-200 hover:border-[#4f46e5]",
										)}
									>
										<button
											type="button"
											className="flex min-w-0 flex-1 items-center gap-3 text-left"
											onClick={() => selectTemplate(template)}
										>
											<AssistantAvatar name={template.name} src={template.avatar} />
											<span className="min-w-0 flex-1">
												<span className="block text-sm font-medium text-slate-900">
													{template.name}
												</span>
												<span className="mt-1 block truncate text-xs text-slate-500">
													{template.description}
												</span>
											</span>
										</button>
										<Button
											type="button"
											variant="ghost"
											size="sm"
											onClick={() => setDetailTemplate(template)}
										>
											查看详情
										</Button>
									</div>
								))}
							</div>
						) : (
							<div className="flex h-40 items-center justify-center text-sm text-slate-500">
								暂无可用的预设角色，可先创建自定义 AI 队友
							</div>
						)}

						<div className="mt-6 border-t border-slate-200 pt-5">
							<h3 className="text-sm font-semibold text-slate-900">
								{customMode ? "自定义 AI 队友" : "完善队友信息"}
							</h3>
							<div className="mt-4 grid gap-4 md:grid-cols-[auto_1fr_1fr]">
								<div className="flex items-center gap-3 md:row-span-2 md:flex-col md:items-start">
									<AssistantAvatar
										// 中文注释：未上传时展示固定默认头像；有模板头像或用户上传时优先展示对应 src。
										name={selectedTemplate?.name || name || "自定义 AI 队友"}
										src={previewAvatar || avatar}
										size="lg"
									/>
									{/* 中文注释：预设和自定义创建共用头像上传能力，避免创建后的编辑能力与创建阶段不一致。 */}
									<label className="inline-flex h-8 cursor-pointer items-center rounded-md border border-slate-200 px-2 text-xs font-medium text-slate-700 hover:bg-slate-50">
										<ImagePlus className="mr-1.5 size-3.5" />
										{uploadingAvatar ? "上传中" : "上传头像"}
										<input
											type="file"
											accept={getNativeFileInputAccept("image/*")}
											className="sr-only"
											onChange={handleAvatarChange}
											disabled={uploadingAvatar}
										/>
									</label>
								</div>
								<Field
									label="自定义名称"
									value={name}
									onChange={(nextName) => {
										setName(nextName);
										setNameError("");
										nameCheckSequenceRef.current += 1;
										setCheckingName(false);
									}}
									onBlur={() => void validateName()}
									error={nameError}
									checking={checkingName}
									placeholder="例如：小智、阿乐"
									maxLength={ASSISTANT_FORM_LIMITS.name}
								/>
								<Field
									label="角色名称"
									value={roleName}
									onChange={setRoleName}
									placeholder="例如：投标经理"
									maxLength={ASSISTANT_FORM_LIMITS.roleName}
									readOnly={!customMode}
								/>
								<label className="space-y-1.5 md:col-span-2">
									<span className="text-xs font-medium text-slate-700">
										简介 <span className="text-red-500">*</span>
									</span>
									<span className="relative block">
										<textarea
											value={introduction}
											onChange={(event) => setIntroduction(event.target.value)}
											placeholder="简要说明这位 AI 队友擅长什么、可以提供哪些帮助"
											maxLength={ASSISTANT_FORM_LIMITS.description}
											rows={3}
											className="w-full resize-none rounded-md border border-slate-200 bg-white px-3 py-2 pb-6 text-sm text-slate-800 focus:border-[#4f46e5] focus:outline-none"
										/>
										<span className="pointer-events-none absolute bottom-2 right-3 text-xs text-slate-400">
											{introduction.length}/{ASSISTANT_FORM_LIMITS.description}
										</span>
									</span>
								</label>
							</div>
						</div>
					</div>
					<DialogFooter className="border-t border-slate-200 px-6 py-4">
						<Button
							variant="outline"
							onClick={handleClose}
							disabled={submitting || uploadingAvatar}
						>
							取消
						</Button>
						<Button onClick={handleSubmit} disabled={!formValid || uploadingAvatar || submitting}>
							{submitting ? "创建中…" : "创建并启用"}
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>

			<TemplateDetailSheet
				template={detailTemplate}
				onOpenChange={(nextOpen) => !nextOpen && setDetailTemplate(null)}
				onSelect={(template) => {
					selectTemplate(template);
					setDetailTemplate(null);
				}}
			/>
		</>
	);
}

function Field({
	label,
	value,
	placeholder,
	readOnly,
	maxLength,
	onChange,
	onBlur,
	error,
	checking,
}: {
	label: string;
	value: string;
	placeholder: string;
	readOnly?: boolean;
	maxLength: number;
	onChange: (value: string) => void;
	onBlur?: () => void;
	error?: string;
	checking?: boolean;
}) {
	return (
		<label className="space-y-1.5">
			<span className="text-xs font-medium text-slate-700">
				{label} <span className="text-red-500">*</span>
			</span>
			<span className="relative block">
				<input
					type="text"
					value={value}
					onChange={(event) => onChange(event.target.value)}
					onBlur={onBlur}
					placeholder={placeholder}
					readOnly={readOnly}
					maxLength={maxLength}
					aria-invalid={Boolean(error)}
					className={cn(
						"w-full rounded-md border bg-white px-3 py-2 pr-14 text-sm text-slate-800 read-only:bg-slate-50 focus:border-[#4f46e5] focus:outline-none",
						error ? "border-red-500" : "border-slate-200",
					)}
				/>
				<span className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-xs text-slate-400">
					{value.length}/{maxLength}
				</span>
			</span>
			{(error || checking) && (
				<span className={cn("block pt-1 text-xs", error ? "text-red-500" : "text-slate-400")}>
					{error || "正在检查名称..."}
				</span>
			)}
		</label>
	);
}

function TemplateDetailSheet({
	template,
	onOpenChange,
	onSelect,
}: {
	template: BackendAITeammateTemplate | null;
	onOpenChange: (open: boolean) => void;
	onSelect: (template: BackendAITeammateTemplate) => void;
}) {
	return (
		<Sheet open={!!template} onOpenChange={onOpenChange}>
			<SheetContent className="gap-0 sm:max-w-[680px]">
				{template ? (
					<>
						<SheetHeader className="border-b border-slate-200 px-6 py-5 pr-14">
							<SheetTitle className="text-base font-semibold text-slate-900">员工详情</SheetTitle>
						</SheetHeader>
						<div className="border-b border-slate-100 px-6 py-6">
							<div className="flex items-start gap-4">
								<AssistantAvatar name={template.name} src={template.avatar} size="lg" />
								<div className="min-w-0">
									<h2 className="text-xl font-semibold text-slate-900">{template.name}</h2>
									<p className="mt-2 text-sm leading-6 text-slate-500">
										{template.description || "暂无角色介绍"}
									</p>
								</div>
							</div>
						</div>
						<div className="min-h-0 flex-1 overflow-y-auto p-6">
							<h3 className="text-base font-semibold text-slate-900">核心能力</h3>
							<div className="mt-4 space-y-3">
								{(template.expertise ?? []).map((item, index) => {
									const ExpertiseIcon =
										EXPERTISE_ICONS[index % EXPERTISE_ICONS.length] ?? FileSearch;
									return (
										<div
											key={item}
											className="flex items-start gap-3 rounded-xl border border-slate-200 bg-white px-4 py-4"
										>
											<ExpertiseIcon
												className="mt-0.5 size-5 shrink-0 text-blue-500"
												aria-hidden="true"
											/>
											<span className="min-w-0">
												<span className="block text-sm font-medium text-slate-800">{item}</span>
												<span className="mt-1 block text-sm leading-5 text-slate-500">
													{TEMPLATE_EXPERTISE_DESCRIPTIONS[template.code]?.[index] ??
														`围绕${item}提供清晰、可执行的分析与建议。`}
												</span>
											</span>
										</div>
									);
								})}
							</div>
						</div>
						<SheetFooter className="flex-row justify-start border-t border-slate-200 p-4">
							<Button className="min-w-28" onClick={() => onSelect(template)}>
								就他了！
							</Button>
						</SheetFooter>
					</>
				) : null}
			</SheetContent>
		</Sheet>
	);
}

// 中文注释：擅长领域目前只提供文本，按顺序轮换图标以增强能力卡片的可扫描性。
const EXPERTISE_ICONS = [FileSearch, ChartNoAxesCombined, PenLine, WandSparkles, Lightbulb];

function isImageFile(file: File): boolean {
	if (file.type.startsWith("image/")) return true;
	return /\.(avif|bmp|gif|jpe?g|png|svg|webp)$/i.test(file.name);
}
