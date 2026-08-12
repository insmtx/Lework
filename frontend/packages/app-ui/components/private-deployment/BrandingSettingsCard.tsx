"use client";

import {
	getNativeFileInputAccept,
	projectFileApi,
	readBrandLogo,
	readCustomBrandName,
	saveBrandLogo,
	saveBrandName,
} from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import {
	Card,
	CardContent,
	CardDescription,
	CardFooter,
	CardHeader,
	CardTitle,
} from "@leros/ui/components/ui/card";
import { Input } from "@leros/ui/components/ui/input";
import { Camera, LoaderCircle, Palette, Save } from "lucide-react";
import { type ChangeEvent, useRef, useState } from "react";
import { toast } from "sonner";
import { APP_LOGO_SRC } from "../../assets";
import { ProtectedImage } from "../avatar/ProtectedImage";

function isImageFile(file: File): boolean {
	if (file.type.startsWith("image/")) return true;
	return /\.(avif|bmp|gif|jpe?g|png|svg|webp)$/i.test(file.name);
}

function revokeObjectURLSafe(url?: string) {
	if (url?.startsWith("blob:")) {
		URL.revokeObjectURL(url);
	}
}

export function BrandingSettingsCard() {
	const fileInputRef = useRef<HTMLInputElement>(null);
	const logoPreviewRef = useRef<string | undefined>(undefined);
	const [logoPublicId, setLogoPublicId] = useState<string | null>(() => readBrandLogo());
	const [logoPreview, setLogoPreview] = useState<string | undefined>();
	const [uploadingLogo, setUploadingLogo] = useState(false);
	const [brandName, setBrandName] = useState(() => readCustomBrandName() ?? "");
	const [savingName, setSavingName] = useState(false);

	const clearLogoPreview = () => {
		revokeObjectURLSafe(logoPreviewRef.current);
		logoPreviewRef.current = undefined;
		setLogoPreview(undefined);
	};

	const handleLogoChange = async (event: ChangeEvent<HTMLInputElement>) => {
		const file = event.target.files?.[0];
		event.target.value = "";
		if (!file) return;
		if (!isImageFile(file)) {
			toast.error("请选择图片文件");
			return;
		}
		if (file.size > 5 * 1024 * 1024) {
			toast.error("图片大小不能超过 5M");
			return;
		}

		clearLogoPreview();
		setUploadingLogo(true);
		const previewURL = URL.createObjectURL(file);
		logoPreviewRef.current = previewURL;
		setLogoPreview(previewURL);
		try {
			const uploadResponse = await projectFileApi.uploadLoose({ file, purpose: "avatar" });
			const uploaded = uploadResponse.data;
			if (!uploaded?.public_id) {
				throw new Error("Logo 上传失败");
			}
			saveBrandLogo(uploaded.public_id);
			setLogoPublicId(uploaded.public_id);
			toast.success("Logo 已更新");
		} catch (error) {
			clearLogoPreview();
			const message = error instanceof Error ? error.message : "Logo 上传失败";
			toast.error(message);
		} finally {
			setUploadingLogo(false);
		}
	};

	const handleSaveBrandName = () => {
		setSavingName(true);
		try {
			const trimmed = brandName.trim();
			saveBrandName(brandName);
			setBrandName(trimmed);
			toast.success("品牌名已保存");
		} catch (error) {
			const message = error instanceof Error ? error.message : "品牌名保存失败";
			toast.error(message);
		} finally {
			setSavingName(false);
		}
	};

	const defaultLogoFallback = (
		<img src={APP_LOGO_SRC} alt="" className="h-full w-full object-contain" />
	);

	return (
		<Card className="border-indigo-100 bg-white/90 shadow-sm backdrop-blur">
			<CardHeader>
				<div className="flex items-center gap-3">
					<span className="flex size-10 items-center justify-center rounded-xl bg-indigo-100 text-indigo-600">
						<Palette className="size-5" />
					</span>
					<div>
						<CardTitle>品牌定制</CardTitle>
						<CardDescription className="mt-1">
							自定义系统 Logo 与品牌名。未设置时继续使用系统默认展示。
						</CardDescription>
					</div>
				</div>
			</CardHeader>
			<CardContent className="space-y-6">
				<section className="space-y-3">
					<span className="text-sm font-medium text-slate-800">系统 Logo</span>
					<div className="flex items-start gap-5">
						<button
							type="button"
							className="group relative size-20 shrink-0 overflow-hidden rounded-xl bg-slate-50 ring-4 ring-slate-100"
							onClick={() => fileInputRef.current?.click()}
							disabled={uploadingLogo}
							aria-label="上传系统 Logo"
						>
							{logoPreview || logoPublicId ? (
								<ProtectedImage
									src={logoPublicId ?? undefined}
									localSrc={logoPreview}
									alt="系统 Logo"
									className="h-full w-full object-contain"
									fallback={defaultLogoFallback}
									onProtectedSrcLoaded={clearLogoPreview}
								/>
							) : (
								defaultLogoFallback
							)}
							<span className="absolute inset-0 flex items-center justify-center bg-black/35 opacity-0 transition-opacity group-hover:opacity-100">
								{uploadingLogo ? (
									<LoaderCircle className="size-5 animate-spin text-white" />
								) : (
									<Camera className="size-5 text-white" />
								)}
							</span>
						</button>
						<div className="min-w-0 pt-1 text-xs leading-5 text-slate-500">
							<p>支持图片格式：jpg/jpeg/png/webp</p>
							<p>图片大小不超过 5M</p>
							<p>上传成功后立即生效，并替换侧边栏左上角 Logo。</p>
						</div>
						<input
							ref={fileInputRef}
							type="file"
							accept={getNativeFileInputAccept("image/jpeg,image/jpg,image/png,image/webp")}
							className="hidden"
							onChange={(event) => {
								void handleLogoChange(event);
							}}
						/>
					</div>
				</section>

				<label className="block space-y-2" htmlFor="branding-settings-name">
					<span className="text-sm font-medium text-slate-800">品牌名</span>
					<Input
						id="branding-settings-name"
						value={brandName}
						onChange={(event) => setBrandName(event.target.value)}
						placeholder="Lework"
						disabled={savingName}
					/>
					<p className="text-xs leading-5 text-slate-500">
						用于侧边栏、工作台描述、默认回复名称与技能市场文案。留空则恢复默认 Lework。
					</p>
				</label>
			</CardContent>
			<CardFooter className="flex justify-end">
				<Button type="button" onClick={handleSaveBrandName} disabled={savingName || uploadingLogo}>
					{savingName ? <LoaderCircle className="animate-spin" /> : <Save />}
					保存品牌名
				</Button>
			</CardFooter>
		</Card>
	);
}
