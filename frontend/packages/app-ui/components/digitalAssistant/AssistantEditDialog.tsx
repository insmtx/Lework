"use client";

import type { DigitalAssistantItem } from "@leros/store";
import {
	getFileDownloadUrl,
	getFilePublicUrlFromStorageUri,
	projectFileApi,
	useDAStore,
} from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogTitle,
} from "@leros/ui/components/ui/dialog";
import { ImagePlus } from "lucide-react";
import { type ChangeEvent, useEffect, useState } from "react";
import { toast } from "sonner";
import { AssistantAvatar } from "./AssistantAvatar";
import { getAssistantDisplayStatus } from "./assistantStatus";

export type AssistantEditDialogProps = {
	assistant: DigitalAssistantItem;
	open: boolean;
	onOpenChange: (open: boolean) => void;
};

export function AssistantEditDialog({ assistant, open, onOpenChange }: AssistantEditDialogProps) {
	const { updateAssistant } = useDAStore((s) => s);
	const [name, setName] = useState(assistant.name);
	const [avatar, setAvatar] = useState(assistant.avatar);
	const [description, setDescription] = useState(assistant.description);
	const [systemPrompt, setSystemPrompt] = useState(assistant.systemPrompt);
	const [uploadingAvatar, setUploadingAvatar] = useState(false);
	const [previewAvatar, setPreviewAvatar] = useState<string | undefined>();

	useEffect(() => {
		if (!open) return;
		setName(assistant.name);
		setAvatar(assistant.avatar);
		setDescription(assistant.description);
		setSystemPrompt(assistant.systemPrompt);
		setPreviewAvatar(undefined);
	}, [assistant, open]);

	const statusInfo = getAssistantDisplayStatus(assistant);

	const handleSubmit = async () => {
		if (assistant.source === "template") {
			toast.error("模板创建的队友不允许修改");
			return;
		}
		if (!name.trim()) return;
		await updateAssistant({
			id: assistant.id,
			name: name.trim(),
			avatar: avatar.trim(),
			description: description.trim(),
			system_prompt: systemPrompt.trim(),
		});
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
			setAvatar(
				getFilePublicUrlFromStorageUri(response.data.storage_uri) ?? getFileDownloadUrl(publicID),
			);
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
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="max-h-[min(88dvh,640px)] max-w-[min(92vw,520px)] gap-0 overflow-y-auto p-0 sm:rounded-2xl">
				<DialogTitle className="sr-only">编辑 {assistant.name}</DialogTitle>
				<DialogDescription className="sr-only">编辑 AI 队友基础信息和能力简介</DialogDescription>

				<div className="px-6 pb-6 pt-7 sm:px-8">
					<div className="flex items-start pr-8">
						<div className="min-w-0 flex-1">
							<h2 className="truncate text-xl font-semibold text-[var(--leros-text-strong)] sm:text-2xl">
								{name || assistant.name}
							</h2>
							<div className="mt-3 flex flex-wrap items-center gap-2">
								<span className="rounded-md bg-[var(--leros-surface-soft)] px-2.5 py-1 text-xs font-medium text-[var(--leros-text-muted)]">
									{statusInfo.label}
								</span>
								{assistant.source && (
									<span className="rounded-md bg-[var(--leros-surface-soft)] px-2.5 py-1 text-xs font-medium text-[var(--leros-text-muted)]">
										{assistant.source === "template" ? "模板创建" : "自定义"}
									</span>
								)}
							</div>
						</div>
					</div>

					<div className="mt-8 space-y-5">
						<div className="space-y-1.5">
							<span className="text-xs font-medium text-slate-700">头像</span>
							<div className="flex items-center gap-3">
								<AssistantAvatar name={name || assistant.name} src={previewAvatar || avatar} />
								<label className="inline-flex h-9 cursor-pointer items-center justify-center rounded-md border border-slate-200 bg-white px-3 text-sm font-medium text-slate-700 transition-colors hover:bg-slate-50">
									<ImagePlus className="mr-2 size-4" />
									{uploadingAvatar ? "上传中" : "上传头像"}
									<input
										type="file"
										accept="image/*"
										className="sr-only"
										onChange={handleAvatarChange}
										disabled={uploadingAvatar}
									/>
								</label>
							</div>
						</div>
						<div className="space-y-1.5">
							<span className="text-xs font-medium text-slate-700">名称 *</span>
							<input
								type="text"
								value={name}
								onChange={(e) => setName(e.target.value)}
								placeholder="队友名称"
								className="w-full rounded-md border border-slate-200 bg-white px-3 py-2 text-sm text-slate-800 placeholder:text-slate-400 transition-colors focus:border-blue-300 focus:outline-none"
							/>
						</div>
						<div className="space-y-1.5">
							<span className="text-xs font-medium text-slate-700">描述</span>
							<input
								type="text"
								value={description}
								onChange={(e) => setDescription(e.target.value)}
								placeholder="简短描述"
								className="w-full rounded-md border border-slate-200 bg-white px-3 py-2 text-sm text-slate-800 placeholder:text-slate-400 transition-colors focus:border-blue-300 focus:outline-none"
							/>
						</div>
						<div className="space-y-1.5">
							<span className="text-xs font-medium text-slate-700">简介</span>
							<textarea
								value={systemPrompt}
								onChange={(e) => setSystemPrompt(e.target.value)}
								placeholder="能力边界、执行方式和输出要求"
								rows={5}
								className="w-full resize-none rounded-md border border-slate-200 bg-white px-3 py-2 text-sm text-slate-800 placeholder:text-slate-400 transition-colors focus:border-blue-300 focus:outline-none"
							/>
						</div>
					</div>
				</div>

				<DialogFooter className="border-t border-[var(--leros-control-border)] bg-white px-6 py-4 sm:px-8">
					<Button
						variant="outline"
						className="h-11 rounded-lg px-6"
						onClick={() => onOpenChange(false)}
					>
						取消
					</Button>
					<Button
						type="button"
						onClick={handleSubmit}
						disabled={!name.trim()}
						className="h-11 rounded-lg bg-[var(--leros-text-strong)] px-8 text-sm font-semibold text-white hover:bg-[var(--leros-text)]"
					>
						保存
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
