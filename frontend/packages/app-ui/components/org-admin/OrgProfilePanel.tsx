"use client";

import { type OrgInfo, orgAdminApi, projectFileApi, useAuthStore } from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import { Input } from "@leros/ui/components/ui/input";
import { Camera, Loader2 } from "lucide-react";
import { type ChangeEvent, useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { ORGANIZATION_DEFAULT_AVATAR_SRC } from "../../assets";
import { ProtectedImage } from "../avatar/ProtectedImage";

function isImageFile(file: File): boolean {
	return file.type.startsWith("image/");
}

function revokeObjectURLSafe(url?: string) {
	if (url?.startsWith("blob:")) {
		URL.revokeObjectURL(url);
	}
}

export function OrgProfilePanel({
	compact = false,
	active = true,
}: {
	compact?: boolean;
	active?: boolean;
}) {
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
			// 中文注释：基本信息读取暂时直接使用 AuthSession 返回的当前组织，避免依赖为空的 public_id。
			const data = {
				name: currentOrgName,
				logo: currentOrgLogo,
			};

			// 中文注释：保留原 GetOrg 查询逻辑，后续确认接口契约后可恢复。
			// const resp = await orgAdminApi.getOrg({ public_id: orgPublicId });
			// const data = resp.data.data;
			setOrg(data);
			setNameDraft(data.name);
			setInitialName(data.name);
			// 中文注释：组织接口已可直接返回资源地址；保留原值也兼容尚未迁移的 file public_id。
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
		// 中文注释：暂时保留原 public_id 判空逻辑，允许保存接口自行返回当前契约错误。
		// if (!orgPublicId || !user) return;
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
			setPendingLogoUrl(updated.logo);
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
		setPendingLogoUrl(org?.logo);
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
			<div className="rounded-2xl border border-[var(--leros-control-border)] bg-white p-8 text-sm text-[var(--leros-text-subtle)]">
				请先登录并选择组织后再管理组织信息。
			</div>
		);
	}

	if (loading) {
		return (
			<div className="flex items-center justify-center py-24 text-sm text-[var(--leros-text-subtle)]">
				<Loader2 className="mr-2 size-4 animate-spin" />
				加载中...
			</div>
		);
	}

	return (
		<div className={compact ? undefined : "mx-auto max-w-3xl"}>
			{!compact && (
				<h1 className="mb-6 text-xl font-semibold text-[var(--leros-text-strong)]">基本信息</h1>
			)}
			<div
				className={
					compact
						? "space-y-6"
						: "rounded-2xl border border-[var(--leros-control-border)] bg-white p-6 md:p-8"
				}
			>
				<div className={compact ? "space-y-6" : "space-y-8"}>
					<section>
						<h2 className="mb-4 text-sm font-semibold text-[var(--leros-text-strong)]">组织图标</h2>
						<div className="flex items-start gap-5">
							<button
								type="button"
								className="group relative size-20 shrink-0 overflow-hidden rounded-full bg-[var(--leros-primary-softer)] ring-4 ring-slate-100"
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
							<div className="min-w-0 pt-1 text-xs leading-5 text-[var(--leros-text-subtle)]">
								<p>支持图片格式：jpg/jpeg/png</p>
								<p>图片大小不超过 5M</p>
								<p>为保证展示效果，请上传 1:1 比例的图片</p>
							</div>
							<input
								ref={fileInputRef}
								type="file"
								accept="image/jpeg,image/jpg,image/png,image/webp"
								className="hidden"
								onChange={(event) => {
									void handleLogoChange(event);
								}}
							/>
						</div>
					</section>

					<section>
						<h2 className="mb-3 text-sm font-semibold text-[var(--leros-text-strong)]">组织名称</h2>
						<Input
							value={nameDraft}
							onChange={(event) => setNameDraft(event.target.value)}
							placeholder="请输入组织名称"
							className="max-w-md"
						/>
					</section>
				</div>

				<div className={compact ? "mt-6 flex justify-end gap-3" : "mt-10 flex justify-end gap-3"}>
					<Button type="button" variant="outline" onClick={handleCancel} disabled={saving}>
						取消
					</Button>
					<Button
						type="button"
						onClick={() => void handleSave()}
						disabled={saving || uploadingLogo}
					>
						{saving ? <Loader2 className="size-4 animate-spin" /> : null}
						保存
					</Button>
				</div>
			</div>
		</div>
	);
}
