"use client";

import { projectFileApi } from "@leros/store";
import { cn } from "@leros/ui/lib/utils";
import { ImagePlus, Loader2, Upload, X } from "lucide-react";
import { forwardRef, useCallback, useId, useImperativeHandle, useRef, useState } from "react";
import { toast } from "sonner";
import {
	FEEDBACK_IMAGE_ACCEPT,
	FEEDBACK_MAX_IMAGES,
	getImageFilesFromClipboard,
	isAcceptedFeedbackImage,
} from "./feedbackImageUtils";

type UploadedImage = {
	id: string;
	publicId: string;
	previewUrl: string;
};

export type FeedbackImageUploadHandle = {
	uploadFiles: (files: File[]) => Promise<void>;
};

type FeedbackImageUploadProps = {
	attachmentIds: string[];
	onAttachmentIdsChange: (ids: string[]) => void;
};

export const FeedbackImageUpload = forwardRef<FeedbackImageUploadHandle, FeedbackImageUploadProps>(
	function FeedbackImageUpload({ attachmentIds, onAttachmentIdsChange }, ref) {
		const inputId = useId();
		const inputRef = useRef<HTMLInputElement>(null);
		const [images, setImages] = useState<UploadedImage[]>([]);
		const [uploadingCount, setUploadingCount] = useState(0);
		const [dragActive, setDragActive] = useState(false);
		const imagesRef = useRef<UploadedImage[]>([]);
		const attachmentIdsRef = useRef(attachmentIds);

		imagesRef.current = images;
		attachmentIdsRef.current = attachmentIds;

		const uploadFiles = useCallback(
			async (files: File[]) => {
				const imageFiles = files.filter(isAcceptedFeedbackImage);
				if (!imageFiles.length) {
					if (files.length > 0) {
						toast.error("仅支持 JPG、PNG、WebP 格式图片");
					}
					return;
				}

				const remaining = FEEDBACK_MAX_IMAGES - imagesRef.current.length;
				if (remaining <= 0) {
					toast.error(`最多上传 ${FEEDBACK_MAX_IMAGES} 张图片`);
					return;
				}

				const selected = imageFiles.slice(0, remaining);
				if (imageFiles.length > remaining) {
					toast.error(`最多上传 ${FEEDBACK_MAX_IMAGES} 张图片`);
				}

				setUploadingCount((count) => count + selected.length);
				const nextImages = [...imagesRef.current];
				const nextIds = [...attachmentIdsRef.current];

				for (const file of selected) {
					const previewUrl = URL.createObjectURL(file);
					try {
						const response = await projectFileApi.uploadLoose({
							file,
							purpose: "attachment",
						});
						const publicId = response.data?.public_id;
						if (!publicId) {
							URL.revokeObjectURL(previewUrl);
							toast.error("图片上传失败");
							continue;
						}
						nextImages.push({
							id: `${publicId}-${Date.now()}`,
							publicId,
							previewUrl,
						});
						nextIds.push(publicId);
					} catch {
						URL.revokeObjectURL(previewUrl);
						toast.error("图片上传失败");
					}
				}

				setImages(nextImages);
				onAttachmentIdsChange(nextIds);
				setUploadingCount((count) => Math.max(0, count - selected.length));
			},
			[onAttachmentIdsChange],
		);

		useImperativeHandle(ref, () => ({ uploadFiles }), [uploadFiles]);

		const handleSelect = async (event: React.ChangeEvent<HTMLInputElement>) => {
			const files = Array.from(event.target.files ?? []);
			event.target.value = "";
			if (!files.length) return;
			await uploadFiles(files);
		};

		const handleRemove = (image: UploadedImage) => {
			URL.revokeObjectURL(image.previewUrl);
			setImages((current) => current.filter((item) => item.id !== image.id));
			onAttachmentIdsChange(attachmentIds.filter((id) => id !== image.publicId));
		};

		const handlePaste = (event: React.ClipboardEvent<HTMLElement>) => {
			const files = getImageFilesFromClipboard(event.clipboardData);
			if (!files.length) return;
			event.preventDefault();
			void uploadFiles(files);
		};

		const handleDragOver = (event: React.DragEvent<HTMLElement>) => {
			event.preventDefault();
			setDragActive(true);
		};

		const handleDragLeave = (event: React.DragEvent<HTMLElement>) => {
			if (event.currentTarget.contains(event.relatedTarget as Node)) return;
			setDragActive(false);
		};

		const handleDrop = (event: React.DragEvent<HTMLElement>) => {
			event.preventDefault();
			setDragActive(false);
			const files = Array.from(event.dataTransfer.files);
			if (!files.length) return;
			void uploadFiles(files);
		};

		const openFilePicker = () => {
			if (canUploadMore && !isUploading) {
				inputRef.current?.click();
			}
		};

		const canUploadMore = images.length < FEEDBACK_MAX_IMAGES;
		const isUploading = uploadingCount > 0;
		const showEmptyHint = images.length === 0 && !isUploading;

		const dropZoneClassName = cn(
			"rounded-lg border border-dashed px-3 py-3 transition-colors",
			canUploadMore && !isUploading
				? "border-[var(--leros-control-border)]"
				: "border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)]/60",
			dragActive && "border-[var(--leros-primary)] bg-[var(--leros-primary-subtle)]",
		);

		return (
			<div className="space-y-2">
				<div className="flex items-center justify-between gap-2">
					<p className="text-xs font-medium text-slate-500">截图附件</p>
					<span className="text-xs text-[var(--leros-text-subtle)]">
						{images.length}/{FEEDBACK_MAX_IMAGES}
					</span>
				</div>

				<input
					ref={inputRef}
					id={inputId}
					type="file"
					accept={FEEDBACK_IMAGE_ACCEPT}
					multiple
					className="hidden"
					onChange={handleSelect}
				/>

				{showEmptyHint ? (
					<button
						type="button"
						onClick={openFilePicker}
						onPaste={handlePaste}
						onDragOver={handleDragOver}
						onDragLeave={handleDragLeave}
						onDrop={handleDrop}
						disabled={!canUploadMore || isUploading}
						className={cn(
							dropZoneClassName,
							"flex w-full flex-col items-center gap-2 py-2 text-center disabled:cursor-default",
							canUploadMore &&
								!isUploading &&
								"hover:border-[var(--leros-text-muted)] hover:bg-[var(--leros-surface-soft)]",
						)}
					>
						<div className="flex size-10 items-center justify-center rounded-full bg-[var(--leros-surface-soft)] text-[var(--leros-text-muted)]">
							<Upload className="size-5" />
						</div>
						<div>
							<p className="text-sm text-[var(--leros-text-strong)]">点击上传、拖拽或粘贴图片</p>
							<p className="mt-0.5 text-xs text-[var(--leros-text-muted)]">
								支持 JPG、PNG、WebP，最多 {FEEDBACK_MAX_IMAGES} 张
							</p>
						</div>
					</button>
				) : (
					// biome-ignore lint/a11y/noStaticElementInteractions: 已上传截图区域需要承接拖拽与粘贴，且内部包含删除/添加按钮，不能整体使用 button。
					<div
						onPaste={handlePaste}
						onDragOver={handleDragOver}
						onDragLeave={handleDragLeave}
						onDrop={handleDrop}
						className={dropZoneClassName}
					>
						<div className="space-y-3">
							<div className="flex flex-wrap gap-2">
								{images.map((image) => (
									<div
										key={image.id}
										className="group relative size-[72px] overflow-hidden rounded-lg border border-[var(--leros-control-border)] bg-white shadow-sm"
									>
										<img src={image.previewUrl} alt="反馈截图" className="size-full object-cover" />
										<button
											type="button"
											onClick={(event) => {
												event.stopPropagation();
												handleRemove(image);
											}}
											className="absolute right-1 top-1 rounded-full bg-slate-900/70 p-0.5 text-white opacity-0 transition-opacity group-hover:opacity-100"
											aria-label="删除图片"
										>
											<X className="size-3" />
										</button>
									</div>
								))}
								{isUploading ? (
									<div className="flex size-[72px] items-center justify-center rounded-lg border border-dashed border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)]">
										<Loader2 className="size-5 animate-spin text-[var(--leros-text-muted)]" />
									</div>
								) : null}
								{canUploadMore && !isUploading ? (
									<button
										type="button"
										onClick={openFilePicker}
										className="flex size-[72px] flex-col items-center justify-center gap-1 rounded-lg border border-dashed border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] text-[var(--leros-text-muted)] transition-colors hover:border-[var(--leros-text-muted)] hover:text-[var(--leros-text-strong)]"
										aria-label="添加图片"
									>
										<ImagePlus className="size-4" />
										<span className="text-[10px]">添加</span>
									</button>
								) : null}
							</div>
							{canUploadMore ? (
								<p className="text-center text-xs text-[var(--leros-text-muted)]">
									支持拖拽或点击添加
								</p>
							) : null}
						</div>
					</div>
				)}
			</div>
		);
	},
);
