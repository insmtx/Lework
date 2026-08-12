"use client";

import {
	getNativeFileInputAccept,
	type OrgInfo,
	orgAdminApi,
	projectFileApi,
	useAuthStore,
} from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import { Input } from "@leros/ui/components/ui/input";
import { Label } from "@leros/ui/components/ui/label";
import { Building2, Camera, Loader2 } from "lucide-react";
import { type ChangeEvent, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { ORGANIZATION_DEFAULT_AVATAR_SRC } from "../../assets";
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

export function OrgProfilePanel({ active = true }: { active?: boolean }) {
	const user = useAuthStore((s) => s.authUser);
	const refreshAuthSession = useAuthStore((s) => s.refreshAuthSession);
	const syncOrganizationProfile = useAuthStore((s) => s.syncOrganizationProfile);
	const fileInputRef = useRef<HTMLInputElement>(null);
	const logoPreviewRef = useRef<string | undefined>(undefined);

	const [loading, setLoading] = useState(true);
	const [saving, setSaving] = useState(false);
	const [uploadingLogo, setUploadingLogo] = useState(false);
	const [org, setOrg] = useState<Pick<OrgInfo, "name" | "logo"> | null>(null);
	const [nameDraft, setNameDraft] = useState("");
	const [initialName, setInitialName] = useState("");
	const [initialLogo, setInitialLogo] = useState<string | undefined>();
	const [logoPreview, setLogoPreview] = useState<string | undefined>();
	const [pendingLogoUrl, setPendingLogoUrl] = useState<string | undefined>();

	const currentOrg = user?.currentOrg;
	const currentOrgId = currentOrg?.id;
	const currentOrgName = currentOrg?.name;
	const currentOrgLogo = currentOrg?.logo;
	const orgPublicId = currentOrg?.publicId;

	const clearLogoPreview = useCallback(() => {
		revokeObjectURLSafe(logoPreviewRef.current);
		logoPreviewRef.current = undefined;
		setLogoPreview(undefined);
	}, []);

	useEffect(() => {
		return () => {
			revokeObjectURLSafe(logoPreviewRef.current);
		};
	}, []);

	const loadData = useCallback(async () => {
		if (!currentOrgId || !currentOrgName) {
			setLoading(false);
			return;
		}

		setLoading(true);
		clearLogoPreview();
		try {
			// 中文注释：组织信息管理只编辑名称与图标，优先使用 AuthSession，避免依赖为空的 public_id。
			const data = {
				name: currentOrgName,
				logo: currentOrgLogo,
			};
			setOrg(data);
			setNameDraft(data.name);
			setInitialName(data.name);
			setInitialLogo(data.logo);
			setPendingLogoUrl(data.logo);
		} catch (err) {
			const message = err instanceof Error ? err.message : "组织信息加载失败";
			toast.error(message);
		} finally {
			setLoading(false);
		}
	}, [clearLogoPreview, currentOrgId, currentOrgLogo, currentOrgName]);

	useEffect(() => {
		if (!active) return;
		// 中文注释：进入页面时主动刷新 AuthSession，页面数据随后从最新 currentOrg 读取。
		void refreshAuthSession();
	}, [active, refreshAuthSession]);

	useEffect(() => {
		if (!active || !currentOrgId || !currentOrgName) return;
		void loadData();
	}, [active, currentOrgId, currentOrgName, loadData]);

	const dirty = useMemo(() => {
		return (
			nameDraft.trim() !== initialName.trim() || (pendingLogoUrl ?? "") !== (initialLogo ?? "")
		);
	}, [initialLogo, initialName, nameDraft, pendingLogoUrl]);

	const handleProtectedLogoLoaded = useCallback(() => {
		clearLogoPreview();
	}, [clearLogoPreview]);

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
				throw new Error("组织图标上传失败");
			}
			setPendingLogoUrl(uploaded.public_id);
		} catch (err) {
			clearLogoPreview();
			const message = err instanceof Error ? err.message : "组织图标上传失败";
			toast.error(message);
		} finally {
			setUploadingLogo(false);
		}
	};

	const handleSave = async () => {
		if (!user) return;
		const trimmedName = nameDraft.trim();
		if (!trimmedName) {
			toast.error("组织名称不能为空");
			return;
		}

		setSaving(true);
		try {
			const resp = await orgAdminApi.updateOrg({
				public_id: orgPublicId ?? "",
				name: trimmedName,
				logo: pendingLogoUrl,
			});
			const updated = resp.data.data;
			setOrg(updated);
			setInitialName(updated.name);
			setInitialLogo(updated.logo);
			setNameDraft(updated.name);
			setPendingLogoUrl(updated.logo);
			clearLogoPreview();
			if (user.currentOrg?.id) {
				syncOrganizationProfile(user.currentOrg.id, { name: updated.name, logo: updated.logo });
			}
			toast.success("组织信息已保存");
		} catch (err) {
			const message = err instanceof Error ? err.message : "保存失败";
			toast.error(message);
		} finally {
			setSaving(false);
		}
	};

	const handleCancel = () => {
		setNameDraft(initialName);
		clearLogoPreview();
		setPendingLogoUrl(initialLogo ?? org?.logo);
	};

	// 中文注释：仅在组织尚未设置图标或原图不可用时显示固定默认头像，不影响用户上传并保存自定义图标。
	const orgLogoFallback = (
		<img
			src={ORGANIZATION_DEFAULT_AVATAR_SRC}
			alt={user?.currentOrg?.name ?? "默认组织头像"}
			className="h-full w-full object-cover"
		/>
	);

	if (!user?.currentOrg) {
		return (
			<div className="py-8 text-sm text-[var(--leros-text-subtle)]">
				请先登录并选择组织后再管理组织信息。
			</div>
		);
	}

	if (loading) {
		return (
			<div className="flex h-full items-center justify-center py-24 text-sm text-[var(--leros-text-subtle)]">
				<Loader2 className="mr-2 size-4 animate-spin" />
				加载中...
			</div>
		);
	}

	const actions = (
		<div className="flex gap-3">
			<Button type="button" variant="outline" onClick={handleCancel} disabled={saving || !dirty}>
				取消
			</Button>
			<Button
				type="button"
				onClick={() => void handleSave()}
				disabled={saving || uploadingLogo || !dirty}
			>
				{saving ? <Loader2 className="size-4 animate-spin" /> : null}
				保存
			</Button>
		</div>
	);

	return (
		<div className="flex w-full flex-col gap-5">
			<section className="rounded-2xl border border-[var(--leros-control-border)] bg-[var(--leros-surface)] p-5 sm:p-6">
				<div className="mb-5 border-b border-[var(--leros-control-border)] pb-4">
					<h2 className="text-sm font-semibold text-[var(--leros-text-strong)]">组织标识</h2>
					<p className="mt-1 text-xs leading-5 text-[var(--leros-text-subtle)]">
						图标与名称会展示在侧栏、组织切换与协作场景中。
					</p>
				</div>

				<div className="flex flex-col gap-6">
					<div className="flex items-start gap-4">
						<button
							type="button"
							className="group relative size-24 shrink-0 overflow-hidden rounded-2xl bg-[var(--leros-primary-softer)] ring-4 ring-slate-100"
							onClick={() => fileInputRef.current?.click()}
							disabled={uploadingLogo}
							aria-label="上传组织图标"
						>
							<ProtectedImage
								src={pendingLogoUrl}
								localSrc={logoPreview}
								alt={user.currentOrg.name}
								className="h-full w-full object-cover"
								fallback={orgLogoFallback}
								onProtectedSrcLoaded={handleProtectedLogoLoaded}
								onProtectedSrcNotFound={() => setPendingLogoUrl(undefined)}
							/>
							<span className="absolute inset-0 flex items-center justify-center bg-black/35 opacity-0 transition-opacity group-hover:opacity-100">
								{uploadingLogo ? (
									<Loader2 className="size-5 animate-spin text-white" />
								) : (
									<Camera className="size-5 text-white" />
								)}
							</span>
						</button>
						<div className="min-w-0 space-y-1.5 pt-1 text-xs leading-5 text-[var(--leros-text-subtle)]">
							<p className="flex items-center gap-1.5 font-medium text-[var(--leros-text-muted)]">
								<Building2 className="size-3.5" />
								组织图标
							</p>
							<p>支持 jpg / jpeg / png / webp</p>
							<p>大小不超过 5M，建议 1:1 比例</p>
							<p>点击图标即可重新上传</p>
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

					<div className="space-y-2">
						<Label htmlFor="org-profile-name" className="text-xs text-[var(--leros-text-muted)]">
							组织名称 <span className="text-red-500">*</span>
						</Label>
						<div className="flex flex-col gap-3 sm:flex-row sm:items-center">
							<Input
								id="org-profile-name"
								value={nameDraft}
								onChange={(event) => setNameDraft(event.target.value)}
								placeholder="请输入组织名称"
								className="h-11 max-w-md"
								maxLength={64}
							/>
							{actions}
						</div>
						<p className="text-xs text-[var(--leros-text-subtle)]">
							用于成员识别当前组织，建议使用公司或团队正式名称。
						</p>
					</div>
				</div>
			</section>
		</div>
	);
}
