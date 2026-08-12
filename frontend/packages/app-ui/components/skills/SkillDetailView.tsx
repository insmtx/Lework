"use client";

import {
	officialPluginMarketplaceApi,
	type PluginInstallationStatus,
	type PluginRevisionFile,
	pluginApi,
} from "@leros/store";
import {
	AlertDialog,
	AlertDialogAction,
	AlertDialogCancel,
	AlertDialogContent,
	AlertDialogDescription,
	AlertDialogFooter,
	AlertDialogHeader,
	AlertDialogTitle,
} from "@leros/ui/components/ui/alert-dialog";
import { Button } from "@leros/ui/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuTrigger,
} from "@leros/ui/components/ui/dropdown-menu";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@leros/ui/components/ui/tabs";
import {
	ArrowLeft,
	Calendar,
	Ellipsis,
	FileText,
	FolderOpen,
	Loader2,
	RefreshCw,
	Trash2,
} from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { MarkdownRenderer } from "../common/MarkdownRenderer";
import { SkillFileTree } from "./SkillFileTree";
import { canUpdateOrganizationSkill } from "./skillInstallationState";

interface SkillDetailData {
	skill_id: string;
	source: string;
	name: string;
	display_name?: string;
	description: string;
	skill_md: string;
	version: string;
	author: string;
	category: string;
	tags: string[];
	icon: string;
	installed: boolean;
	marketplace_available: boolean;
	latest_version?: string;
	update_available: boolean;
	organization_override: boolean;
	files: PluginRevisionFile[];
	has_content_snapshot: boolean;
}

interface SkillDetailViewProps {
	skillId: string;
	/** Selects either the official marketplace or the organization repository. */
	source?: "official" | "organization";
	/** Called when the user wants to navigate back to the marketplace */
	onBack?: () => void;
	/** Called when user clicks "去使用" for an organization skill */
	onUse?: (skillId: string, displayLabel?: string) => void;
	/** Called after an official marketplace item is updated in the organization. */
	onOfficialInstalled?: () => void;
}

export function SkillDetailView({
	skillId,
	source = "official",
	onBack,
	onUse,
	onOfficialInstalled,
}: SkillDetailViewProps) {
	const [skill, setSkill] = useState<SkillDetailData | null>(null);
	const [loading, setLoading] = useState(true);
	const [error, setError] = useState<string | null>(null);
	const [installing, setInstalling] = useState(false);
	const [installationStatus, setInstallationStatus] = useState<PluginInstallationStatus | null>(
		null,
	);
	const [activeTab, setActiveTab] = useState("overview");
	const [mounted, setMounted] = useState(false);
	const [stickyHeaderActive, setStickyHeaderActive] = useState(false);
	const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
	const [deleting, setDeleting] = useState(false);

	// Gate fetch on mounted to avoid StrictMode double-fire
	useEffect(() => {
		setMounted(true);
	}, []);

	const fetchSkill = useCallback(async () => {
		setLoading(true);
		setError(null);
		setInstallationStatus(null);
		setStickyHeaderActive(false);
		const cancelled = false;
		const fetchInstallationStatus = async (kind: string, code: string) => {
			try {
				const response = await pluginApi.getInstallationStatus({ kind, code });
				if (!cancelled) setInstallationStatus(response.data.data);
			} catch (statusError) {
				if (!cancelled) {
					console.error("Failed to fetch skill installation status:", statusError);
				}
			}
		};
		try {
			if (source === "official") {
				const resp = await officialPluginMarketplaceApi.get(skillId);
				if (cancelled) return;
				const item = resp.data.data;
				const content = item.content;
				setSkill({
					skill_id: item.public_id,
					source: "official",
					name: item.code,
					display_name: item.display_name || item.name,
					description: item.description ?? "",
					skill_md: content?.skill_md ?? "",
					version: item.version,
					author: item.author,
					category: item.category,
					tags: item.tags,
					icon: item.icon ?? "",
					installed: item.installed,
					marketplace_available: item.marketplace_available,
					latest_version: item.latest_version,
					update_available: item.update_available,
					organization_override: item.organization_override,
					files: content?.files ?? [],
					has_content_snapshot: content != null,
				});
				return;
			}
			if (source === "organization") {
				const resp = await pluginApi.get(skillId);
				if (cancelled) return;
				const plugin = resp.data.data.plugin;
				const content = resp.data.data.content;
				setSkill({
					skill_id: plugin.public_id,
					source: "organization",
					name: plugin.code,
					display_name: plugin.display_name || plugin.name,
					description: plugin.description ?? "",
					skill_md: content?.skill_md ?? "",
					version: String(content?.version ?? plugin.current_revision),
					author: "组织插件",
					category: plugin.kind,
					tags: [plugin.kind],
					icon: "",
					installed: true,
					marketplace_available: false,
					update_available: false,
					organization_override: false,
					files: content?.files ?? [],
					has_content_snapshot: content !== null,
				});
				await fetchInstallationStatus(plugin.kind, plugin.code);
				return;
			}
			throw new Error(`不支持的技能来源：${source}`);
		} catch (err: any) {
			if (cancelled) return;
			const msg = err?.response?.data?.message ?? err?.message ?? "加载失败";
			setError(msg);
		} finally {
			if (!cancelled) {
				setLoading(false);
			}
		}
	}, [skillId, source]);

	useEffect(() => {
		if (!mounted) return;
		fetchSkill();
	}, [mounted, fetchSkill]);

	const handleInstall = useCallback(async () => {
		if (!skill) return;
		const marketplaceItemID =
			skill.source === "official" ? skill.skill_id : installationStatus?.marketplace_item_id;
		if (!marketplaceItemID) return;
		setInstalling(true);
		try {
			const response = await officialPluginMarketplaceApi.install(marketplaceItemID);
			const operation = response.data.data.operation;
			await fetchSkill();
			onOfficialInstalled?.();
			if (operation === "updated") {
				toast.success("技能更新成功");
			} else if (operation === "already_current") {
				toast.success("技能已是最新版本");
			} else {
				toast.success("技能安装成功");
			}
		} catch (err: any) {
			const msg = err?.response?.data?.message ?? err?.message ?? "未知错误";
			toast.error(`安装或更新失败：${msg}`);
		} finally {
			setInstalling(false);
		}
	}, [fetchSkill, installationStatus?.marketplace_item_id, onOfficialInstalled, skill]);

	const handleDeleteOrganizationPlugin = useCallback(async () => {
		if (!skill) return;
		setDeleting(true);
		try {
			await pluginApi.delete(skill.skill_id);
			toast.success("技能已删除");
			setDeleteDialogOpen(false);
			onBack?.();
		} catch (err: any) {
			const msg = err?.response?.data?.message ?? err?.message ?? "未知错误";
			toast.error(`删除失败：${msg}`);
		} finally {
			setDeleting(false);
		}
	}, [onBack, skill]);

	const handleUse = useCallback(() => {
		if (!skill) return;
		onUse?.(skill.name, skill.display_name || skill.name);
	}, [onUse, skill]);

	const canUpdateOrganization = canUpdateOrganizationSkill(installationStatus);
	const canUpdateMarketplace =
		skill?.source === "official" &&
		skill.installed &&
		skill.marketplace_available &&
		skill.update_available;

	// Loading state
	if (loading) {
		return (
			<div className="flex min-h-0 flex-1 items-center justify-center bg-[var(--leros-app-bg)]">
				<div className="flex flex-col items-center gap-3 text-[var(--leros-text-subtle)]">
					<Loader2 className="size-6 animate-spin" />
					<span className="text-sm">加载技能详情...</span>
				</div>
			</div>
		);
	}

	// Error state
	if (error || !skill) {
		return (
			<div className="flex min-h-0 flex-1 items-center justify-center bg-[var(--leros-app-bg)]">
				<div className="flex flex-col items-center gap-4 text-[var(--leros-text-subtle)]">
					<p className="text-sm">{error ?? "技能不存在"}</p>
					<div className="flex gap-2">
						{onBack && (
							<button
								type="button"
								onClick={onBack}
								className="inline-flex items-center gap-1.5 rounded-md border border-[var(--leros-control-border)] px-3 py-1.5 text-xs text-[var(--leros-text-muted)] hover:bg-[var(--leros-surface-soft)] transition-colors"
							>
								<ArrowLeft className="size-3.5" />
								返回市场
							</button>
						)}
						<button
							type="button"
							onClick={fetchSkill}
							className="rounded-md border border-[var(--leros-control-border)] px-3 py-1.5 text-xs text-[var(--leros-primary)] hover:bg-[var(--leros-primary-soft)] transition-colors"
						>
							重试
						</button>
					</div>
				</div>
			</div>
		);
	}

	const displayName = skill.display_name || skill.name;

	return (
		<div
			data-slot="skill-detail-scroll"
			onScroll={(event) => setStickyHeaderActive(event.currentTarget.scrollTop > 0)}
			className="flex min-h-0 min-w-0 flex-1 flex-col overflow-y-auto bg-[var(--leros-app-bg)] [scrollbar-gutter:stable]"
		>
			{/* Keep the primary navigation available while the detail content scrolls. */}
			{onBack && (
				<div
					data-slot="skill-detail-back-bar"
					data-stuck={stickyHeaderActive}
					className={`sticky top-0 z-40 flex h-12 shrink-0 items-center justify-between gap-4 border-b bg-[var(--leros-app-bg)] px-6 lg:px-12 xl:px-16 ${
						stickyHeaderActive
							? "border-[var(--leros-control-border)] shadow-sm"
							: "border-transparent"
					}`}
				>
					<div className="flex min-w-0 items-center gap-3">
						<button
							type="button"
							onClick={onBack}
							className="inline-flex shrink-0 items-center gap-1 text-xs text-[var(--leros-text-muted)] transition-colors hover:text-[var(--leros-text-strong)]"
						>
							<ArrowLeft className="size-3.5" />
							返回
						</button>
						{stickyHeaderActive && (
							<>
								<span className="h-3.5 w-px shrink-0 bg-[var(--leros-control-border)]" />
								<span className="truncate text-sm font-semibold text-[var(--leros-text-strong)]">
									{displayName}
								</span>
							</>
						)}
					</div>
					{stickyHeaderActive && (
						<Button
							size="sm"
							onClick={handleUse}
							className="h-7 shrink-0 rounded-lg px-3 text-xs font-medium bg-[var(--leros-primary)] text-white hover:bg-[var(--leros-primary)]/90"
						>
							去使用
						</Button>
					)}
				</div>
			)}

			{/* Top section: skill identity and actions (full width) */}
			<div className={`min-w-0 px-6 lg:px-12 xl:px-16 ${onBack ? "" : "pt-4"}`}>
				{/* Skill Header */}
				<div className="mb-5 flex min-w-0 flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
					<div className="flex min-w-0 gap-4">
						{/* Icon */}
						{skill.icon ? (
							<img
								src={skill.icon}
								alt={displayName}
								className="h-14 w-14 shrink-0 rounded-xl object-cover shadow-sm"
							/>
						) : (
							<div className="flex h-14 w-14 shrink-0 items-center justify-center rounded-xl bg-[var(--leros-primary-soft)] text-[var(--leros-primary)] shadow-sm">
								<span className="text-[28px] font-semibold">
									{displayName.charAt(0).toUpperCase()}
								</span>
							</div>
						)}
						<div className="min-w-0">
							<h1 className="mb-1 break-words text-xl font-semibold leading-tight text-[var(--leros-text-strong)]">
								{displayName}
							</h1>
							<div className="flex flex-wrap items-center gap-1.5">
								{skill.category && (
									<span className="inline-flex px-2 py-0.5 rounded bg-[var(--leros-surface-soft)] text-[var(--leros-text-muted)] text-[11px] font-medium border border-[var(--leros-control-border)]">
										{skill.category}
									</span>
								)}
								{skill.source === "official" && skill.installed && !skill.marketplace_available ? (
									<span className="inline-flex rounded border border-slate-200 bg-slate-100 px-2 py-0.5 text-[11px] font-medium text-slate-600">
										已下架
									</span>
								) : skill.source === "official" && skill.update_available ? (
									<span className="inline-flex rounded border border-amber-200 bg-amber-50 px-2 py-0.5 text-[11px] font-medium text-amber-700">
										有更新
									</span>
								) : skill.source === "official" && skill.installed ? (
									<span className="inline-flex rounded border border-green-200 bg-green-50 px-2 py-0.5 text-[11px] font-medium text-green-700">
										已安装
									</span>
								) : skill.source === "official" && skill.organization_override ? (
									<span className="inline-flex rounded border border-blue-200 bg-blue-50 px-2 py-0.5 text-[11px] font-medium text-blue-700">
										组织同名版本
									</span>
								) : installationStatus?.update_available && skill.source === "organization" ? (
									<span className="inline-flex rounded border border-amber-200 bg-amber-50 px-2 py-0.5 text-[11px] font-medium text-amber-700">
										有更新
									</span>
								) : null}
							</div>
							{skill.description && (
								<p className="mt-2 max-w-2xl [overflow-wrap:anywhere] text-xs leading-relaxed text-[var(--leros-text-muted)]">
									{skill.description}
								</p>
							)}
						</div>
					</div>

					{/* Action buttons — direct use for marketplace, use+menu for managed skills */}
					{skill.source === "organization" ? (
						<div className="flex shrink-0 items-center gap-1.5">
							<Button
								size="sm"
								onClick={handleUse}
								className="rounded-lg px-4 py-2 text-xs font-medium shadow-sm bg-[var(--leros-primary)] text-white hover:bg-[var(--leros-primary)]/90 hover:shadow-md transition-all"
							>
								去使用
							</Button>
							<DropdownMenu>
								<DropdownMenuTrigger
									render={(props) => (
										<Button
											size="sm"
											variant="ghost"
											{...props}
											aria-label="更多操作"
											className="rounded-lg px-2 py-2 hover:bg-[var(--leros-surface-soft)]"
										>
											<Ellipsis className="size-4" />
										</Button>
									)}
								/>
								<DropdownMenuContent align="end" className="w-32">
									{canUpdateOrganization && (
										<DropdownMenuItem onClick={handleInstall} disabled={installing}>
											{installing ? (
												<Loader2 className="size-3.5 mr-2 animate-spin" />
											) : (
												<RefreshCw className="size-3.5 mr-2" />
											)}
											{installing ? "更新中..." : "更新"}
										</DropdownMenuItem>
									)}
									<DropdownMenuItem
										onClick={() => setDeleteDialogOpen(true)}
										disabled={installing}
										className="text-xs text-red-600 focus:text-red-600"
									>
										<Trash2 className="size-3.5 mr-2" />
										删除
									</DropdownMenuItem>
								</DropdownMenuContent>
							</DropdownMenu>
						</div>
					) : (
						<div className="flex shrink-0 items-center gap-2">
							<Button
								size="sm"
								onClick={handleUse}
								className="rounded-lg px-4 py-2 text-xs font-medium shadow-sm bg-[var(--leros-primary)] text-white hover:bg-[var(--leros-primary)]/90 hover:shadow-md transition-all"
							>
								去使用
							</Button>
							{canUpdateMarketplace && (
								<Button
									size="sm"
									variant="outline"
									onClick={handleInstall}
									disabled={installing}
									className="rounded-lg px-4 py-2 text-xs font-medium"
								>
									{installing ? (
										<Loader2 className="mr-1.5 size-3.5 animate-spin" />
									) : (
										<RefreshCw className="mr-1.5 size-3.5" />
									)}
									{installing ? "更新中..." : "更新"}
								</Button>
							)}
						</div>
					)}
				</div>

				{skill.source === "official" && skill.organization_override && (
					<div className="mb-4 rounded-lg border border-blue-200 bg-blue-50 px-3 py-2 text-xs text-blue-700">
						组织中存在同 code 的自建 Skill；“去使用”将执行组织版本，市场版本不会覆盖它。
					</div>
				)}
			</div>

			{/* Bottom section: tabs + sidebar side by side */}
			<div
				data-slot="skill-detail-content"
				className="grid min-w-0 flex-1 grid-cols-1 gap-6 px-6 pb-12 lg:px-12 lg:pb-16 xl:grid-cols-[minmax(0,1fr)_16rem] xl:px-16"
			>
				{/* Left: tabbed content */}
				<div className="min-w-0">
					<Tabs value={activeTab} onValueChange={setActiveTab} className="min-w-0 w-full">
						<div className="border-b border-[var(--leros-control-border)] mb-5">
							<TabsList variant="line" className="gap-6">
								<TabsTrigger
									value="overview"
									className="pb-3 text-xs font-medium data-active:text-[var(--leros-primary)]"
								>
									概述
								</TabsTrigger>
								<TabsTrigger
									value="files"
									className="pb-3 text-xs font-medium data-active:text-[var(--leros-primary)]"
								>
									文件
								</TabsTrigger>
								<TabsTrigger
									value="versions"
									className="pb-3 text-xs font-medium data-active:text-[var(--leros-primary)]"
								>
									历史版本
								</TabsTrigger>
							</TabsList>
						</div>

						{/* Overview Tab — markdown-rendered SKILL.md body */}
						<TabsContent value="overview" className="min-w-0 outline-none">
							{skill.skill_md ? (
								<MarkdownRenderer
									content={skill.skill_md}
									className="prose prose-slate prose-sm min-w-0 max-w-none [overflow-wrap:anywhere] prose-headings:text-[var(--leros-text-strong)] prose-p:text-xs prose-p:leading-relaxed prose-p:text-[var(--leros-text-muted)] prose-p:my-1 prose-pre:overflow-x-auto prose-pre:rounded-xl prose-pre:border prose-pre:border-slate-800 prose-pre:bg-slate-950 prose-pre:p-4 prose-pre:text-slate-100 prose-pre:shadow-sm [&>*]:min-w-0 [&_:not(pre)>code]:break-words [&_:not(pre)>code]:rounded [&_:not(pre)>code]:bg-slate-100 [&_:not(pre)>code]:px-1.5 [&_:not(pre)>code]:py-0.5 [&_:not(pre)>code]:text-[11px] [&_:not(pre)>code]:font-medium [&_:not(pre)>code]:text-slate-800 [&_pre]:max-w-full [&_pre_code]:bg-transparent [&_pre_code]:p-0 [&_pre_code]:text-[13px] [&_pre_code]:leading-6 [&_pre_code]:text-slate-100"
								/>
							) : (
								<div className="flex flex-col items-center justify-center py-10 text-[var(--leros-text-subtle)]">
									<FileText className="size-6 mb-2 opacity-40" />
									<p className="text-xs">
										{!skill.has_content_snapshot ? "暂无内容快照" : "暂无概述内容"}
									</p>
								</div>
							)}
						</TabsContent>

						{/* Files Tab */}
						<TabsContent value="files" className="min-w-0 outline-none">
							{skill.files && skill.files.length > 0 ? (
								<SkillFileTree files={skill.files} />
							) : (
								<div className="flex flex-col items-center justify-center py-10 text-[var(--leros-text-subtle)]">
									<FolderOpen className="size-6 mb-2 opacity-40" />
									<p className="text-xs">
										{!skill.has_content_snapshot ? "暂无内容快照" : "暂无文件"}
									</p>
								</div>
							)}
						</TabsContent>

						{/* Versions Tab */}
						<TabsContent value="versions" className="min-w-0 outline-none">
							<div className="flex flex-col items-center justify-center py-10 text-[var(--leros-text-subtle)]">
								<Calendar className="size-6 mb-2 opacity-40" />
								<p className="text-xs">版本历史</p>
								{skill.version && (
									<span className="mt-2 inline-flex items-center rounded-md border border-[var(--leros-control-border)] px-2.5 py-0.5 text-[11px] font-medium text-[var(--leros-text-muted)]">
										当前版本: v{skill.version}
									</span>
								)}
							</div>
						</TabsContent>
					</Tabs>
				</div>

				{/* Right Sidebar — top aligns with tab bar, no left border */}
				<aside className="flex min-w-0 flex-col gap-4 py-3 xl:w-64">
					<section
						data-slot="skill-detail-metadata"
						className="bg-[var(--leros-surface-soft)]/50 p-4 rounded-xl border border-dashed border-[var(--leros-control-border)]"
					>
						<h5 className="text-[11px] font-semibold uppercase tracking-wider text-[var(--leros-text-subtle)] mb-3">
							元数据
						</h5>
						<div className="space-y-2 text-[11px]">
							<div className="flex justify-between">
								<span className="text-[var(--leros-text-subtle)]">版本</span>
								<span className="text-[var(--leros-text-strong)] font-medium">
									{skill.version ? `v${skill.version}` : "—"}
								</span>
							</div>
							<div className="flex justify-between">
								<span className="text-[var(--leros-text-subtle)]">作者</span>
								<span className="text-[var(--leros-text-strong)] font-medium">
									{skill.author || "—"}
								</span>
							</div>
						</div>
					</section>
				</aside>
			</div>

			<AlertDialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
				<AlertDialogContent>
					<AlertDialogHeader>
						<AlertDialogTitle>删除技能？</AlertDialogTitle>
						<AlertDialogDescription>
							删除后，{displayName} 将从组织插件库中移除，不能再被新项目使用。
						</AlertDialogDescription>
					</AlertDialogHeader>
					<AlertDialogFooter>
						<AlertDialogCancel disabled={deleting}>取消</AlertDialogCancel>
						<AlertDialogAction
							variant="destructive"
							onClick={handleDeleteOrganizationPlugin}
							disabled={deleting}
						>
							{deleting ? "删除中..." : "删除"}
						</AlertDialogAction>
					</AlertDialogFooter>
				</AlertDialogContent>
			</AlertDialog>
		</div>
	);
}
