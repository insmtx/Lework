"use client";

import type { DigitalAssistantItem } from "@leros/store";
import {
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
import { ImagePlus } from "lucide-react";
import { type ChangeEvent, useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { blobToDataURL, cacheProtectedImageDataURL } from "../avatar/ProtectedImage";
import { AssistantAvatar } from "./AssistantAvatar";
import { ASSISTANT_FORM_LIMITS } from "./assistantFormLimits";

export type AssistantEditDialogProps = {
	assistant: DigitalAssistantItem;
	open: boolean;
	onOpenChange: (open: boolean) => void;
};

export function AssistantEditDialog({ assistant, open, onOpenChange }: AssistantEditDialogProps) {
	const { assistants, updateAssistant } = useDAStore((s) => s);
	const isTemplateAssistant = assistant.source === "template";
	const [name, setName] = useState(assistant.name);
	const [roleName, setRoleName] = useState(assistant.roleName || assistant.name);
	const [avatar, setAvatar] = useState(assistant.avatar);
	const [description, setDescription] = useState(assistant.description);
	const [systemPrompt, setSystemPrompt] = useState(assistant.systemPrompt);
	const [uploadingAvatar, setUploadingAvatar] = useState(false);
	const [submitting, setSubmitting] = useState(false);
	const [previewAvatar, setPreviewAvatar] = useState<string | undefined>();
	const [nameError, setNameError] = useState("");
	const [checkingName, setCheckingName] = useState(false);
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
			assistants.some(
				(item) =>
					item.id !== assistant.id && item.name.trim().toLocaleLowerCase() === normalizedName,
			)
		) {
			// 中文注释：编辑时排除当前队友，本地命中其他队友后不再重复请求服务端。
			setNameError("该 AI 队友名称已存在");
			setCheckingName(false);
			return false;
		}
		setCheckingName(true);
		try {
			const response = await digitalAssistantApi.checkName({
				name: candidate,
				exclude_id: assistant.id,
			});
			if (sequence !== nameCheckSequenceRef.current) return false;
			const available = response.data.data?.available ?? false;
			setNameError(available ? "" : "该 AI 队友名称已存在");
			return available;
		} catch (error) {
			// 中文注释：预检异常不阻止保存，保存接口会再次执行最终校验。
			console.error("check ai teammate name error:", error);
			return true;
		} finally {
			if (sequence === nameCheckSequenceRef.current) setCheckingName(false);
		}
	}, [assistant.id, assistants, name]);

	useEffect(() => {
		if (!open) return;
		setName(assistant.name);
		setRoleName(assistant.roleName || assistant.name);
		setAvatar(assistant.avatar);
		setDescription(assistant.description);
		setSystemPrompt(assistant.systemPrompt);
		setSubmitting(false);
		setPreviewAvatar(undefined);
		setNameError("");
		setCheckingName(false);
	}, [assistant, open]);

	const handleSubmit = async () => {
		if (!name.trim() || !roleName.trim() || !description.trim() || submitting || uploadingAvatar)
			return;
		if (!(await validateName())) return;
		setSubmitting(true);
		const updated = await updateAssistant({
			id: assistant.id,
			name: name.trim(),
			avatar: avatar.trim(),
			description: description.trim(),
			// 中文注释：模板实例只允许编辑名称、简介与头像，角色名称和角色设定始终沿用模板配置。
			...(isTemplateAssistant
				? {}
				: {
						role_name: roleName.trim(),
						system_prompt: systemPrompt.trim(),
					}),
		});
		setSubmitting(false);
		if (!updated) {
			// 中文注释：保存阶段发生并发重名时复用字段提示，其余错误才使用通用提示。
			if (await validateName()) toast.error("AI 队友保存失败，请稍后重试");
			return;
		}
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
			const publicID = response.data.public_id;
			if (!publicID) throw new Error("头像上传失败");
			// 中文注释：先写入本地缓存再移除 blob 预览，避免受保护图片异步加载时短暂回退为默认头像。
			const dataURL = await blobToDataURL(file);
			cacheProtectedImageDataURL(publicID, dataURL);
			setAvatar(publicID);
			setPreviewAvatar(undefined);
			toast.success("头像已上传");
		} catch (err) {
			const message = err instanceof Error ? err.message : "头像上传失败";
			toast.error(message);
			setPreviewAvatar(undefined);
		} finally {
			URL.revokeObjectURL(previewURL);
			setUploadingAvatar(false);
		}
	};

	return (
		<Dialog
			open={open}
			onOpenChange={(nextOpen) => {
				if (!nextOpen && !submitting && !uploadingAvatar) onOpenChange(false);
			}}
		>
			<DialogContent className="flex max-h-[min(88dvh,640px)] max-w-[min(92vw,560px)] flex-col gap-0 overflow-hidden p-0 sm:rounded-2xl">
				<DialogHeader className="shrink-0 border-b border-slate-200 px-6 py-5 sm:px-8">
					<DialogTitle>编辑AI队友</DialogTitle>
					<span className="mt-1 w-fit rounded-md bg-[var(--leros-surface-soft)] px-2.5 py-1 text-xs font-medium text-[var(--leros-text-muted)]">
						{isTemplateAssistant ? "模板创建" : "自定义创建"}
					</span>
				</DialogHeader>
				<DialogDescription className="sr-only">编辑 AI 队友基础信息和能力简介</DialogDescription>

				<div className="min-h-0 flex-1 overflow-y-auto p-4 sm:px-8">
					<div className="space-y-5">
						<div className="space-y-1.5">
							<span className="text-xs font-medium text-slate-700">头像</span>
							<div className="flex items-center gap-3">
								{/* 中文注释：未上传时展示固定默认头像；编辑名称不影响默认头像外观。 */}
								<AssistantAvatar name={assistant.name} src={previewAvatar || avatar} />
								<label className="inline-flex h-9 cursor-pointer items-center justify-center rounded-md border border-slate-200 bg-white px-3 text-sm font-medium text-slate-700 transition-colors hover:bg-slate-50">
									<ImagePlus className="mr-2 size-4" />
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
						</div>
						<div className="space-y-1.5">
							<span className="text-xs font-medium text-slate-700">
								自定义名称 <span className="text-red-500">*</span>
							</span>
							<div className="relative">
								<input
									type="text"
									value={name}
									onChange={(e) => {
										setName(e.target.value);
										setNameError("");
										nameCheckSequenceRef.current += 1;
										setCheckingName(false);
									}}
									onBlur={() => void validateName()}
									placeholder="例如：小智、阿乐"
									maxLength={ASSISTANT_FORM_LIMITS.name}
									aria-invalid={Boolean(nameError)}
									className={`w-full rounded-md border bg-white px-3 py-2 pr-14 text-sm text-slate-800 placeholder:text-slate-400 transition-colors focus:border-[#4f46e5] focus:outline-none ${
										nameError ? "border-red-500" : "border-slate-200"
									}`}
								/>
								<span className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-xs text-slate-400">
									{name.length}/{ASSISTANT_FORM_LIMITS.name}
								</span>
							</div>
							{(nameError || checkingName) && (
								<p className={`text-xs ${nameError ? "text-red-500" : "text-slate-400"}`}>
									{nameError || "正在检查名称..."}
								</p>
							)}
						</div>
						<div className="space-y-1.5">
							<span className="text-xs font-medium text-slate-700">
								角色名称 <span className="text-red-500">*</span>
							</span>
							<div className="relative">
								<input
									type="text"
									value={roleName}
									onChange={(e) => setRoleName(e.target.value)}
									placeholder="例如：投标经理"
									maxLength={ASSISTANT_FORM_LIMITS.roleName}
									readOnly={isTemplateAssistant}
									className="w-full rounded-md border border-slate-200 bg-white px-3 py-2 pr-14 text-sm text-slate-800 placeholder:text-slate-400 transition-colors read-only:bg-slate-50 focus:border-[#4f46e5] focus:outline-none"
								/>
								<span className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-xs text-slate-400">
									{roleName.length}/{ASSISTANT_FORM_LIMITS.roleName}
								</span>
							</div>
						</div>
						<div className="space-y-1.5">
							<span className="text-xs font-medium text-slate-700">
								简介 <span className="text-red-500">*</span>
							</span>
							<div className="relative">
								<input
									type="text"
									value={description}
									onChange={(e) => setDescription(e.target.value)}
									placeholder="简要说明这个队友能做什么"
									maxLength={ASSISTANT_FORM_LIMITS.description}
									className="w-full rounded-md border border-slate-200 bg-white px-3 py-2 pr-16 text-sm text-slate-800 placeholder:text-slate-400 transition-colors focus:border-[#4f46e5] focus:outline-none"
								/>
								<span className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-xs text-slate-400">
									{description.length}/{ASSISTANT_FORM_LIMITS.description}
								</span>
							</div>
						</div>
						<div className="space-y-1.5">
							<span className="text-xs font-medium text-slate-700">角色设定</span>
							<div className="relative">
								<textarea
									value={systemPrompt}
									onChange={(e) => setSystemPrompt(e.target.value)}
									placeholder="能力边界、执行方式和输出要求"
									maxLength={ASSISTANT_FORM_LIMITS.systemPrompt}
									readOnly={isTemplateAssistant}
									rows={5}
									className="w-full resize-none rounded-md border border-slate-200 bg-white px-3 py-2 pb-6 text-sm text-slate-800 placeholder:text-slate-400 transition-colors read-only:bg-slate-50 focus:border-[#4f46e5] focus:outline-none"
								/>
								<span className="pointer-events-none absolute bottom-2 right-3 text-xs text-slate-400">
									{systemPrompt.length}/{ASSISTANT_FORM_LIMITS.systemPrompt}
								</span>
							</div>
						</div>
					</div>
				</div>

				<DialogFooter className="shrink-0 border-t border-[var(--leros-control-border)] bg-white px-6 py-4 sm:px-8">
					<Button
						variant="outline"
						onClick={() => onOpenChange(false)}
						disabled={submitting || uploadingAvatar}
					>
						取消
					</Button>
					<Button
						type="button"
						onClick={handleSubmit}
						disabled={
							!name.trim() ||
							!roleName.trim() ||
							!description.trim() ||
							submitting ||
							uploadingAvatar
						}
					>
						{submitting ? "保存中…" : "保存"}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

function isImageFile(file: File): boolean {
	if (file.type.startsWith("image/")) return true;
	return /\.(avif|bmp|gif|jpe?g|png|svg|webp)$/i.test(file.name);
}
