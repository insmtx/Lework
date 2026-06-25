"use client";

import type { AuthUser, NavItem, Project, ViewMode } from "@leros/store";
import {
	authenticatedFetch,
	getFileDownloadUrl,
	projectFileApi,
	useAuthStore,
	useChatStore,
	useLayoutStore,
	userApi,
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
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@leros/ui/components/ui/dropdown-menu";
import { Input } from "@leros/ui/components/ui/input";
import { ScrollArea } from "@leros/ui/components/ui/scroll-area";
import { cn } from "@leros/ui/lib/utils";
import {
	Camera,
	Check,
	ChevronsLeft,
	ChevronsRight,
	ClipboardList,
	Database,
	Hash,
	Loader2,
	LogOut,
	MoreHorizontal,
	Network,
	Pencil,
	RefreshCcw,
	Trash2,
	UserRound,
	X,
	Zap,
} from "lucide-react";
import type { ChangeEvent, CSSProperties, PointerEvent as ReactPointerEvent } from "react";
import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { APP_LOGO_SRC } from "../../assets";
import { useAuth } from "../auth";
import { DiceBearAvatar } from "../avatar/DiceBearAvatar";
import { getVisibleLeftRailItems } from "./left-rail-list-utils";

const LEFT_RAIL_WIDTH_STORAGE_KEY = "leros-left-rail-width";
const LEFT_RAIL_COLLAPSED_STORAGE_KEY = "leros-left-rail-collapsed";
const AVATAR_CACHE_PREFIX = "leros-avatar-cache:";
const LEFT_RAIL_COLLAPSED_WIDTH = 72;

type PublicEnv = {
	readonly VITE_LEROS_APP_VERSION?: string;
};

export type AppNavigation = {
	currentPath: string;
	goToRoute: (route: ViewMode) => void;
	goToProject: (projectId: string) => void;
	goToTaskDetail: (projectId: string, taskId: string, sessionId?: string | null) => void;
};

const iconMap: Record<string, React.ReactNode> = {
	IconTask: <ClipboardList className="size-5" />,
	IconSkill: <Zap className="size-5" />,
	IconKnowledge: <Database className="size-5" />,
	IconProject: <Hash className="size-4" />,
};

const navIdToView: Record<string, ViewMode> = {
	workbench: "workbench",
	knowledge: "knowledge",
	skills: "skills",
	"ai-1": "digitalAssistant",
	"ai-2": "digitalAssistant",
	"ai-3": "digitalAssistant",
};

const protectedNavIds = new Set(["skills", "knowledge"]);
const appVersion = getAppVersion();
const brandVersionLabel = appVersion.startsWith("v") ? appVersion : `v${appVersion}`;

export function LeftRail({
	logoSrc = APP_LOGO_SRC,
	navigation,
}: {
	logoSrc?: string;
	navigation?: AppNavigation;
}) {
	const {
		navGroups,
		projects,
		currentView,
		activeProjectId,
		leftRailCollapsed,
		leftRailWidth,
		fetchProjects,
		deleteProject,
		setLeftRailCollapsed,
		setLeftRailWidth,
		switchView,
		switchProject,
		updateProject,
	} = useLayoutStore((s) => s);
	const clearComposerInput = useChatStore((s) => s.clearComposerInput);
	const setAuthUser = useAuthStore((s) => s.setAuthUser);
	const { isHydrated, isAuthenticated, openAuthDialog, requireAuth, logout, user } = useAuth();
	const hasLoadedPreferenceRef = useRef(false);
	const [renameProject, setRenameProject] = useState<Project | null>(null);
	const [renameValue, setRenameValue] = useState("");
	const [deleteTarget, setDeleteTarget] = useState<Project | null>(null);
	const [accountDialogOpen, setAccountDialogOpen] = useState(false);
	const [projectsExpanded, setProjectsExpanded] = useState(false);
	const [aiTeammatesExpanded, setAiTeammatesExpanded] = useState(false);

	/* ── Desktop update notifier ── */
	const [promptOpen, setPromptOpen] = useState(false);
	const [downloadedVersion, setDownloadedVersion] = useState<string | undefined>(undefined);
	const [installing, setInstalling] = useState(false);
	const [installError, setInstallError] = useState<string | null>(null);
	const previousPhaseRef = useRef<DesktopUpdateState["phase"] | null>(null);
	const previousVersionRef = useRef<string | undefined>(undefined);
	const snoozeTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
	const updatePromptSnoozeMs = 5 * 60 * 1000;
	const versionUpdateImageSrc = new URL(
		"../../../../apps/desktop/resources/octopus_version_update.png",
		import.meta.url,
	).href;

	const clearSnoozeTimer = () => {
		if (snoozeTimerRef.current) {
			clearTimeout(snoozeTimerRef.current);
			snoozeTimerRef.current = null;
		}
	};

	const openUpdatePrompt = (version?: string) => {
		clearSnoozeTimer();
		setDownloadedVersion(version);
		setInstallError(null);
		setPromptOpen(true);
	};

	const snoozeUpdatePrompt = () => {
		setPromptOpen(false);
		if (!downloadedVersion || installing) return;
		clearSnoozeTimer();
		snoozeTimerRef.current = setTimeout(() => {
			setPromptOpen(true);
			snoozeTimerRef.current = null;
		}, updatePromptSnoozeMs);
	};

	useEffect(() => {
		const api = getDesktopUpdateApi();
		if (!api) return;

		let mounted = true;

		void api.getState().then((state) => {
			if (!mounted) return;
			previousPhaseRef.current = state.phase;
			previousVersionRef.current = state.availableVersion ?? state.downloadedVersion;
			if (state.phase === "downloaded") {
				openUpdatePrompt(state.downloadedVersion ?? state.availableVersion);
			}
		});

		const unsubscribe = api.subscribe((state) => {
			const previousPhase = previousPhaseRef.current;
			const previousVersion = previousVersionRef.current;
			const nextVersion = state.availableVersion ?? state.downloadedVersion;

			if (
				state.phase === "downloaded" &&
				(previousPhase !== "downloaded" || previousVersion !== nextVersion)
			) {
				openUpdatePrompt(nextVersion);
			}

			previousPhaseRef.current = state.phase;
			previousVersionRef.current = nextVersion;
		});

		return () => {
			mounted = false;
			clearSnoozeTimer();
			unsubscribe();
		};
	}, []);

	const handleInstallNow = async () => {
		setInstalling(true);
		setInstallError(null);
		try {
			const desktopUpdateApi = getDesktopUpdateApi();
			if (!desktopUpdateApi) {
				setInstalling(false);
				setInstallError("当前环境暂不支持自动安装更新");
				return;
			}

			const accepted = await desktopUpdateApi.quitAndInstall();
			if (!accepted) {
				setInstalling(false);
				setInstallError("当前还没有可安装的更新");
			}
		} catch (error) {
			setInstalling(false);
			setInstallError(error instanceof Error ? error.message : "启动安装失败，请稍后重试");
		}
	};
	/* ── end Desktop update notifier ── */

	useEffect(() => {
		fetchProjects();
	}, [fetchProjects]);

	useEffect(() => {
		if (typeof window === "undefined" || hasLoadedPreferenceRef.current) return;
		hasLoadedPreferenceRef.current = true;

		const savedWidth = window.localStorage.getItem(LEFT_RAIL_WIDTH_STORAGE_KEY);
		const savedCollapsed = window.localStorage.getItem(LEFT_RAIL_COLLAPSED_STORAGE_KEY);

		if (savedWidth) {
			const parsedWidth = Number(savedWidth);
			if (Number.isFinite(parsedWidth)) {
				setLeftRailWidth(parsedWidth);
			}
		}

		if (savedCollapsed) {
			setLeftRailCollapsed(savedCollapsed === "true");
		}
	}, [setLeftRailCollapsed, setLeftRailWidth]);

	useEffect(() => {
		if (typeof window === "undefined" || !hasLoadedPreferenceRef.current) return;
		window.localStorage.setItem(LEFT_RAIL_WIDTH_STORAGE_KEY, String(leftRailWidth));
	}, [leftRailWidth]);

	useEffect(() => {
		if (typeof window === "undefined" || !hasLoadedPreferenceRef.current) return;
		window.localStorage.setItem(LEFT_RAIL_COLLAPSED_STORAGE_KEY, String(leftRailCollapsed));
	}, [leftRailCollapsed]);

	const handleNavClick = (item: NavItem) => {
		const view = navIdToView[item.id] ?? "chat";
		const navigate = () => {
			if (navigation) {
				navigation.goToRoute(view);
				return;
			}
			switchView(view);
		};
		if (protectedNavIds.has(item.id)) {
			requireAuth(navigate);
			return;
		}
		navigate();
	};

	const handleProjectClick = (projectId: string) => {
		requireAuth(() => {
			if (projectId !== activeProjectId) {
				clearComposerInput();
			}
			if (navigation) {
				navigation.goToProject(projectId);
				return;
			}
			switchProject(projectId);
		});
	};

	const handleOpenRename = (project: Project) => {
		setRenameProject(project);
		setRenameValue(project.name);
	};

	const handleConfirmRename = async () => {
		const name = renameValue.trim();
		if (!renameProject || !name) return;

		const updatedProject = await updateProject({ public_id: renameProject.id, name });
		if (updatedProject) {
			setRenameProject(null);
			setRenameValue("");
		}
	};

	const handleConfirmDelete = async () => {
		if (!deleteTarget) return;

		const deletingActiveProject =
			activeProjectId === deleteTarget.id ||
			navigation?.currentPath === `/projects/${deleteTarget.id}` ||
			navigation?.currentPath.startsWith(`/projects/${deleteTarget.id}/`);

		const deleted = await deleteProject(deleteTarget.id);
		if (!deleted) return;

		setDeleteTarget(null);

		if (deletingActiveProject) {
			if (navigation) {
				navigation.goToRoute("workbench");
				return;
			}
			switchView("workbench");
		}
	};

	const handleProfileClick = () => {
		if (!isAuthenticated) {
			openAuthDialog("login");
		}
	};

	const handleLogout = () => {
		logout();
		if (navigation) {
			navigation.goToRoute("workbench");
			return;
		}
		switchView("workbench");
	};

	const isItemActive = (item: NavItem) => {
		const view = navIdToView[item.id] ?? "chat";
		if (navigation) {
			return getRouteActive(navigation.currentPath, view);
		}
		return currentView === view;
	};

	const handleResizePointerDown = (event: ReactPointerEvent<HTMLHRElement>) => {
		if (leftRailCollapsed) return;

		const startX = event.clientX;
		const startWidth = leftRailWidth;
		const pointerId = event.pointerId;
		const target = event.currentTarget;

		target.setPointerCapture(pointerId);

		const handlePointerMove = (moveEvent: PointerEvent) => {
			setLeftRailWidth(startWidth + (moveEvent.clientX - startX));
		};

		const handlePointerUp = () => {
			if (target.hasPointerCapture(pointerId)) {
				target.releasePointerCapture(pointerId);
			}
			target.removeEventListener("pointermove", handlePointerMove);
			target.removeEventListener("pointerup", handlePointerUp);
			target.removeEventListener("pointercancel", handlePointerUp);
		};

		target.addEventListener("pointermove", handlePointerMove);
		target.addEventListener("pointerup", handlePointerUp);
		target.addEventListener("pointercancel", handlePointerUp);
	};

	const sidebarWidth = leftRailCollapsed ? LEFT_RAIL_COLLAPSED_WIDTH : leftRailWidth;
	const profileTriggerWidth = Math.max(0, sidebarWidth - 16);

	return (
		<aside
			className="leros-sidebar relative"
			data-collapsed={leftRailCollapsed}
			style={{ width: `${sidebarWidth}px` }}
		>
			<div className="leros-brand">
				<div className="leros-brand-main">
					<div className="leros-logo-placeholder" aria-hidden="true">
						<img
							src={logoSrc}
							alt=""
							className="leros-logo-image"
							onError={(event) => {
								event.currentTarget.hidden = true;
							}}
						/>
						<Network className="size-5" />
					</div>
					<div className="leros-sidebar-expandable min-w-0">
						<div className="leros-brand-title">Lework</div>
						<div className="leros-brand-version">{brandVersionLabel}</div>
					</div>
				</div>
				<button
					type="button"
					className="leros-sidebar-toggle"
					aria-label={leftRailCollapsed ? "展开侧边栏" : "收起侧边栏"}
					onClick={() => setLeftRailCollapsed(!leftRailCollapsed)}
				>
					{leftRailCollapsed ? (
						<ChevronsRight className="size-[18px]" />
					) : (
						<ChevronsLeft className="size-[18px]" />
					)}
				</button>
			</div>

			<ScrollArea hideScrollbar className="min-h-0 flex-1 overflow-hidden">
				<nav className="leros-nav" aria-label="主导航">
					{navGroups.map((group) => {
						return (
							<div key={group.id} className="leros-nav-section">
								{group.label ? <div className="leros-nav-section-label">{group.label}</div> : null}
								{group.id === "projects" ? (
									<ProjectList
										projects={projects}
										activeProjectId={activeProjectId}
										currentView={currentView}
										currentPath={navigation?.currentPath}
										onProjectClick={handleProjectClick}
										onRenameProject={handleOpenRename}
										onDeleteProject={setDeleteTarget}
										collapsed={leftRailCollapsed}
										expanded={projectsExpanded}
										onExpand={() => setProjectsExpanded(true)}
									/>
								) : group.id === "ai-teammates" ? (
									<NavItemList
										items={group.items}
										collapsed={leftRailCollapsed}
										expanded={aiTeammatesExpanded}
										onExpand={() => setAiTeammatesExpanded(true)}
										isItemActive={isItemActive}
										onItemClick={handleNavClick}
									/>
								) : (
									<div className="space-y-1">
										{group.items.map((item: NavItem) => (
											<NavItemButton
												key={item.id}
												item={item}
												active={isItemActive(item)}
												collapsed={leftRailCollapsed}
												onClick={() => handleNavClick(item)}
											/>
										))}
									</div>
								)}
							</div>
						);
					})}
				</nav>
			</ScrollArea>

			<div className="leros-sidebar-footer shrink-0">
				{!isHydrated ? (
					<div className="leros-profile-trigger" aria-hidden="true">
						<span className="leros-avatar animate-pulse bg-slate-200" />
						<div className="leros-sidebar-expandable flex-1 space-y-1.5 overflow-hidden">
							<span className="block h-3.5 w-24 rounded bg-slate-200" />
							<span className="block h-2.5 w-16 rounded bg-slate-100" />
						</div>
					</div>
				) : isAuthenticated ? (
					<DropdownMenu>
						<DropdownMenuTrigger
							render={
								<button
									type="button"
									className="leros-profile-trigger"
									title={user?.name ?? "个人中心"}
								>
									<ProfileAvatar user={user} />
									<div className="leros-sidebar-expandable flex-1 overflow-hidden text-left">
										<p className="truncate text-[14px] font-bold text-[var(--leros-text-strong)]">
											{user?.name ?? "Lework 用户"}
										</p>
										<p className="truncate text-[11px] text-[var(--leros-text-subtle)]">
											{getDisplayPhone(user) ?? "已登录"}
										</p>
									</div>
								</button>
							}
						/>
						<DropdownMenuContent
							align="start"
							side="top"
							sideOffset={10}
							className="leros-profile-menu"
							style={
								{
									"--leros-sidebar-menu-width": `${profileTriggerWidth}px`,
								} as CSSProperties
							}
						>
							{/* 暂时仅保留退出登录入口，其他菜单项先注释隐藏；恢复时记得同步恢复对应 import。 */}
							{/*
							<DropdownMenuItem>
								<UserRound className="size-4" />
								<span>个人信息</span>
							</DropdownMenuItem>
							<DropdownMenuItem>
								<Settings className="size-4" />
								<span>系统设置</span>
							</DropdownMenuItem>
							<DropdownMenuItem>
								<CircleHelp className="size-4" />
								<span>使用帮助</span>
							</DropdownMenuItem>
							*/}
							<DropdownMenuItem onClick={() => setAccountDialogOpen(true)}>
								<UserRound className="size-4" />
								<span>账户管理</span>
							</DropdownMenuItem>
							<DropdownMenuSeparator />
							<DesktopUpdateMenuSection />
							<DropdownMenuSeparator />
							<DropdownMenuItem variant="destructive" onClick={handleLogout}>
								<LogOut className="size-4" />
								<span>退出登录</span>
							</DropdownMenuItem>
						</DropdownMenuContent>
					</DropdownMenu>
				) : (
					<button
						type="button"
						className="leros-profile-trigger"
						onClick={handleProfileClick}
						title="登录 / 注册"
					>
						<ProfileAvatar user={null} />
						<div className="leros-sidebar-expandable flex-1 overflow-hidden text-left">
							<p className="truncate text-[14px] font-bold text-[var(--leros-text-strong)]">
								登录 / 注册
							</p>
							<p className="text-[10px] font-bold uppercase tracking-tight text-[var(--leros-primary)]">
								LEROS
							</p>
						</div>
						<UserRound className="leros-sidebar-expandable size-4 shrink-0 text-[var(--leros-text-subtle)]" />
					</button>
				)}
			</div>

			<hr
				className="leros-sidebar-resize-handle"
				tabIndex={0}
				aria-orientation="vertical"
				aria-label="调整侧边栏宽度"
				aria-valuemin={220}
				aria-valuemax={320}
				aria-valuenow={leftRailWidth}
				onPointerDown={handleResizePointerDown}
				onKeyDown={(event) => {
					if (event.key === "ArrowLeft") setLeftRailWidth(leftRailWidth - 8);
					if (event.key === "ArrowRight") setLeftRailWidth(leftRailWidth + 8);
				}}
			/>
			<AccountManagementDialog
				open={accountDialogOpen}
				user={user}
				onOpenChange={setAccountDialogOpen}
				onUserChange={setAuthUser}
			/>
			<Dialog
				open={renameProject !== null}
				onOpenChange={(open) => !open && setRenameProject(null)}
			>
				<DialogContent className="sm:max-w-md" showCloseButton={false}>
					<DialogHeader>
						<DialogTitle>重命名项目</DialogTitle>
						<DialogDescription>请输入新的项目名称</DialogDescription>
					</DialogHeader>
					<div className="mt-4">
						<input
							type="text"
							value={renameValue}
							onChange={(event) => setRenameValue(event.target.value)}
							onKeyDown={(event) => {
								if (event.key === "Enter") {
									handleConfirmRename();
								}
							}}
							placeholder="项目名称"
							autoFocus
							className="w-full rounded-md border border-slate-200 bg-white px-3 py-2 text-sm text-slate-800 placeholder:text-slate-400 transition-colors focus:border-blue-300 focus:outline-none"
						/>
					</div>
					<DialogFooter className="mt-4">
						<Button variant="outline" onClick={() => setRenameProject(null)}>
							取消
						</Button>
						<Button onClick={handleConfirmRename} disabled={!renameValue.trim()}>
							确认
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>

			<Dialog open={deleteTarget !== null} onOpenChange={(open) => !open && setDeleteTarget(null)}>
				<DialogContent className="sm:max-w-md" showCloseButton={false}>
					<DialogHeader>
						<DialogTitle>删除项目</DialogTitle>
						<DialogDescription>
							确定要删除 <strong>{deleteTarget?.name}</strong> 吗？此操作不可撤销。
						</DialogDescription>
					</DialogHeader>
					<DialogFooter className="mt-4">
						<Button variant="outline" onClick={() => setDeleteTarget(null)}>
							取消
						</Button>
						<Button variant="destructive" onClick={handleConfirmDelete}>
							删除
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>

			{promptOpen ? (
				<div className="absolute inset-x-2 bottom-2 z-50 overflow-hidden rounded-2xl border border-[#4F46E5]/20 bg-[#EEEDFF]/95 text-slate-950 shadow-[0_12px_30px_rgba(79,70,229,0.2)] backdrop-blur">
					<div className="flex">
						<img
							src={versionUpdateImageSrc}
							alt=""
							className="h-[68px] w-[68px] shrink-0 self-end object-contain object-bottom-left"
							aria-hidden="true"
						/>
						<div className="min-w-0 flex-1 px-2.5 py-2">
							<Button
								type="button"
								variant="ghost"
								size="icon-xs"
								className="absolute top-1.5 right-2.5 rounded-full text-slate-950/65 hover:bg-white/50 hover:text-slate-950"
								onClick={snoozeUpdatePrompt}
								disabled={installing}
								aria-label="稍后安装"
							>
								<X className="size-3.5" />
							</Button>
							<div className="pr-7">
								<div className="truncate text-[14px] font-semibold leading-5">新版本已就绪</div>
								<div className="truncate text-[13px] leading-4 text-slate-900/60">
									{downloadedVersion ? `V${downloadedVersion.replace(/^v/i, "")}` : "V"}
								</div>
							</div>
							<div className="mt-1 flex items-center justify-end">
								<Button
									type="button"
									size="sm"
									className="h-7 min-w-20 rounded-lg bg-slate-950 px-3.5 text-sm text-white hover:bg-slate-800"
									onClick={handleInstallNow}
									disabled={installing}
								>
									<RefreshCcw className={installing ? "size-3.5 animate-spin" : "size-3.5"} />
									{installing ? "更新中" : "更新"}
								</Button>
							</div>
							{installError ? (
								<div className="mt-2 truncate text-xs text-red-500">{installError}</div>
							) : null}
						</div>
					</div>
				</div>
			) : null}
		</aside>
	);
}

function AccountManagementDialog({
	open,
	user,
	onOpenChange,
	onUserChange,
}: {
	open: boolean;
	user: AuthUser | null;
	onOpenChange: (open: boolean) => void;
	onUserChange: (user: AuthUser | null) => void;
}) {
	const fileInputRef = useRef<HTMLInputElement>(null);
	const [nameValue, setNameValue] = useState(user?.name ?? "");
	const [editingName, setEditingName] = useState(false);
	const [savingName, setSavingName] = useState(false);
	const [uploadingAvatar, setUploadingAvatar] = useState(false);
	const [previewAvatarUrl, setPreviewAvatarUrl] = useState<string | undefined>();
	const displayPhone = getDisplayPhone(user);

	useEffect(() => {
		if (!open) {
			setEditingName(false);
			setPreviewAvatarUrl(undefined);
			return;
		}
		setNameValue(user?.name ?? "");
	}, [open, user?.name]);

	const updateLocalUser = (patch: Partial<AuthUser>) => {
		if (!user) return;
		onUserChange({ ...user, ...patch });
	};

	const requirePublicId = () => {
		if (user?.publicId) return user.publicId;
		toast.error("当前登录信息缺少用户 ID，请重新登录后再试");
		return null;
	};

	const handleSaveName = async () => {
		const publicId = requirePublicId();
		const nextName = nameValue.trim();
		if (!publicId || !nextName || nextName === user?.name) {
			setEditingName(false);
			return;
		}

		setSavingName(true);
		try {
			const response = await userApi.update({ public_id: publicId, name: nextName });
			const updatedUser = response.data.data;
			if (updatedUser?.name) {
				updateLocalUser({
					publicId: updatedUser.public_id || publicId,
					name: updatedUser.name,
					email: updatedUser.email || user?.email || "",
					phone: updatedUser.phone || user?.phone,
					avatarUrl: updatedUser.avatar_url || user?.avatarUrl,
				});
			} else {
				updateLocalUser({ name: nextName });
			}
			setEditingName(false);
			toast.success("用户名已更新");
		} catch (err) {
			const message = err instanceof Error ? err.message : "用户名更新失败";
			toast.error(message);
		} finally {
			setSavingName(false);
		}
	};

	const handleAvatarChange = async (event: ChangeEvent<HTMLInputElement>) => {
		const file = event.target.files?.[0];
		event.target.value = "";
		if (!file) return;
		if (!isImageFile(file)) {
			toast.error("请选择图片文件");
			return;
		}

		setUploadingAvatar(true);
		const previewURL = URL.createObjectURL(file);
		setPreviewAvatarUrl(previewURL);
		try {
			const uploadResponse = await projectFileApi.uploadLoose({ file, purpose: "avatar" });
			const uploaded = uploadResponse.data;
			if (!uploaded?.public_id) {
				throw new Error("头像上传失败");
			}

			const publicId = requirePublicId();
			if (!publicId) return;

			const avatarUrl = getFileDownloadUrl(uploaded.public_id);
			const response = await userApi.update({ public_id: publicId, avatar_url: avatarUrl });
			const updatedUser = response.data.data;
			cacheAvatarDataURL(avatarUrl, await blobToDataURL(file));
			updateLocalUser({
				publicId: updatedUser?.public_id || publicId,
				name: updatedUser?.name || user?.name || "",
				email: updatedUser?.email || user?.email || "",
				phone: updatedUser?.phone || user?.phone,
				avatarUrl: updatedUser?.avatar_url || avatarUrl,
			});
			toast.success("头像已更新");
		} catch (err) {
			const message = err instanceof Error ? err.message : "头像更新失败";
			toast.error(message);
		} finally {
			setPreviewAvatarUrl(undefined);
			URL.revokeObjectURL(previewURL);
			setUploadingAvatar(false);
		}
	};

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="sm:max-w-md" showCloseButton>
				<DialogHeader>
					<DialogTitle>账户管理</DialogTitle>
					<DialogDescription>查看并更新你的个人信息</DialogDescription>
				</DialogHeader>

				<div className="mt-4 flex flex-col items-center gap-5">
					<div className="relative">
						<button
							type="button"
							className="group relative size-24 overflow-hidden rounded-full bg-[var(--leros-primary)] text-white ring-4 ring-slate-100"
							onClick={() => fileInputRef.current?.click()}
							disabled={uploadingAvatar}
							aria-label="上传头像"
						>
							<ImageWithFallback
								src={previewAvatarUrl || user?.avatarUrl}
								alt={user?.name ?? "Avatar"}
								className="h-full w-full object-cover"
								fallback={
									user ? (
										<DiceBearAvatar
											seed={`user:${displayPhone || user.name}`}
											alt={user.name ?? "Avatar"}
											className="h-full w-full"
											size={128}
										/>
									) : (
										<span className="text-xl font-bold">{getAvatarInitial("Lework")}</span>
									)
								}
							/>
							<span className="absolute inset-0 flex items-center justify-center bg-slate-950/45 text-white opacity-0 transition-opacity group-hover:opacity-100">
								{uploadingAvatar ? (
									<Loader2 className="size-5 animate-spin" />
								) : (
									<Camera className="size-5" />
								)}
							</span>
						</button>
						<input
							ref={fileInputRef}
							type="file"
							accept="image/*"
							className="hidden"
							onChange={handleAvatarChange}
						/>
					</div>

					<div className="w-full space-y-4">
						<div>
							<div className="mb-1.5 text-xs font-medium text-slate-500">用户名</div>
							{editingName ? (
								<div className="flex items-center gap-2">
									<Input
										value={nameValue}
										onChange={(event) => setNameValue(event.target.value)}
										onKeyDown={(event) => {
											if (event.key === "Enter") void handleSaveName();
											if (event.key === "Escape") {
												setNameValue(user?.name ?? "");
												setEditingName(false);
											}
										}}
										autoFocus
										className="h-9"
									/>
									<Button
										size="icon-sm"
										onClick={handleSaveName}
										disabled={savingName || !nameValue.trim()}
										aria-label="保存用户名"
									>
										{savingName ? (
											<Loader2 className="size-4 animate-spin" />
										) : (
											<Check className="size-4" />
										)}
									</Button>
								</div>
							) : (
								<div className="flex items-center justify-between rounded-lg border border-slate-200 bg-slate-50 px-3 py-2">
									<span className="truncate text-sm font-medium text-slate-900">
										{user?.name ?? "Lework 用户"}
									</span>
									<Button
										variant="ghost"
										size="icon-sm"
										onClick={() => setEditingName(true)}
										aria-label="更改用户名"
									>
										<Pencil className="size-3.5" />
									</Button>
								</div>
							)}
						</div>

						<div>
							<div className="mb-1.5 text-xs font-medium text-slate-500">手机号</div>
							<div className="rounded-lg border border-slate-200 bg-slate-50 px-3 py-2 text-sm text-slate-500">
								{displayPhone ?? "未绑定手机号"}
							</div>
						</div>
					</div>
				</div>
			</DialogContent>
		</Dialog>
	);
}

function ProfileAvatar({ user }: { user: AuthUser | null }) {
	const displayPhone = getDisplayPhone(user);
	const fallbackLabel = getAvatarInitial(user?.name ?? displayPhone ?? "Lework");
	const setAuthUser = useAuthStore((s) => s.setAuthUser);

	const handleProtectedAvatarNotFound = () => {
		if (!user?.avatarUrl) return;
		// 中文注释：头像文件如果已经失效，就把本地登录态里的旧下载地址清掉，避免每次刷新都重复命中 404。
		setAuthUser({
			...user,
			avatarUrl: undefined,
		});
	};

	return (
		<span
			className="leros-avatar overflow-hidden text-[11px] font-bold"
			style={{ background: "var(--leros-primary)", color: "#fff" }}
		>
			<ImageWithFallback
				src={user?.avatarUrl}
				alt={user?.name ?? "Avatar"}
				className="h-full w-full object-cover"
				onProtectedSrcNotFound={handleProtectedAvatarNotFound}
				fallback={
					user ? (
						<DiceBearAvatar
							seed={`user:${displayPhone || user.name}`}
							alt={user.name ?? "Avatar"}
							className="h-full w-full"
							size={96}
						/>
					) : (
						<span>{fallbackLabel}</span>
					)
				}
			/>
		</span>
	);
}

function getDisplayPhone(user: AuthUser | null): string | undefined {
	if (user?.phone) return user.phone;
	if (user?.name && /^1[3-9]\d{9}$/.test(user.name)) return user.name;
	return undefined;
}

function getAppVersion(): string {
	const version = (import.meta as ImportMeta & { readonly env?: PublicEnv }).env
		?.VITE_LEROS_APP_VERSION;
	return version?.trim() || "0.0.0";
}

function isImageFile(file: File): boolean {
	if (file.type.startsWith("image/")) return true;
	return /\.(avif|bmp|gif|jpe?g|png|svg|webp)$/i.test(file.name);
}

function ImageWithFallback({
	src,
	alt,
	className,
	fallback,
	onProtectedSrcNotFound,
}: {
	src?: string | null;
	alt: string;
	className: string;
	fallback: React.ReactNode;
	onProtectedSrcNotFound?: () => void;
}) {
	const [failed, setFailed] = useState(false);
	const [imageURL, setImageURL] = useState<string | null>(() => getCachedAvatarDataURL(src));

	useEffect(() => {
		setFailed(false);
		if (!src || !isProtectedFileURL(src)) {
			setImageURL(null);
			return;
		}

		const cachedAvatarURL = getCachedAvatarDataURL(src);
		if (cachedAvatarURL) {
			setImageURL(cachedAvatarURL);
		}

		let cancelled = false;
		authenticatedFetch(src)
			.then(async (response) => {
				if (!response.ok) throw new Error(`HTTP ${response.status}`);
				return response.blob();
			})
			.then(async (blob) => {
				if (cancelled) return;
				const dataURL = await blobToDataURL(blob);
				if (cancelled) return;
				cacheAvatarDataURL(src, dataURL);
				setImageURL(dataURL);
			})
			.catch((error) => {
				if (cancelled) return;
				const isNotFoundError =
					error instanceof Error && (error.message === "HTTP 404" || error.message.includes("404"));
				if (isNotFoundError) {
					onProtectedSrcNotFound?.();
				}
				if (!cachedAvatarURL) setFailed(true);
			});

		return () => {
			cancelled = true;
		};
	}, [src, onProtectedSrcNotFound]);

	if (!src || failed) return <>{fallback}</>;
	const imageSrc = imageURL || src;
	if (isProtectedFileURL(src) && !imageURL) return <>{fallback}</>;

	return (
		<img
			src={imageSrc}
			alt={alt}
			className={className}
			loading="lazy"
			decoding="async"
			referrerPolicy="no-referrer"
			onError={() => setFailed(true)}
		/>
	);
}

function isProtectedFileURL(src: string): boolean {
	return src.includes("/files/") && src.includes("/download");
}

function getAvatarCacheKey(src: string): string {
	return `${AVATAR_CACHE_PREFIX}${src}`;
}

function getCachedAvatarDataURL(src?: string | null): string | null {
	if (!src || typeof window === "undefined" || !isProtectedFileURL(src)) return null;
	try {
		return window.localStorage.getItem(getAvatarCacheKey(src));
	} catch {
		return null;
	}
}

function cacheAvatarDataURL(src: string, dataURL: string) {
	if (typeof window === "undefined" || !isProtectedFileURL(src)) return;
	try {
		window.localStorage.setItem(getAvatarCacheKey(src), dataURL);
	} catch {
		// Avatar cache is an optional UX optimization.
	}
}

function blobToDataURL(blob: Blob): Promise<string> {
	return new Promise((resolve, reject) => {
		const reader = new FileReader();
		reader.addEventListener("load", () => {
			if (typeof reader.result === "string") {
				resolve(reader.result);
				return;
			}
			reject(new Error("头像缓存失败"));
		});
		reader.addEventListener("error", () => reject(new Error("头像缓存失败")));
		reader.readAsDataURL(blob);
	});
}

function getAvatarInitial(label: string) {
	const trimmed = label.trim();
	return (trimmed[0] ?? "L").toUpperCase();
}

type DesktopUpdatePhase =
	| "idle"
	| "checking"
	| "available"
	| "downloading"
	| "downloaded"
	| "up-to-date"
	| "error"
	| "unsupported";

type DesktopUpdateState = {
	currentVersion: string;
	phase: DesktopUpdatePhase;
	message: string;
	availableVersion?: string;
	downloadedVersion?: string;
	progressPercent?: number;
	releaseNotes?: string;
	canCheck: boolean;
	canRestart: boolean;
};

type DesktopUpdateApi = {
	getState: () => Promise<DesktopUpdateState>;
	checkForUpdates: () => Promise<DesktopUpdateState>;
	quitAndInstall: () => Promise<boolean>;
	subscribe: (listener: (state: DesktopUpdateState) => void) => () => void;
};

const initialDesktopUpdateState: DesktopUpdateState = {
	currentVersion: "0.0.0",
	phase: "idle",
	message: "正在读取更新状态",
	canCheck: false,
	canRestart: false,
};

function getDesktopUpdateApi(): DesktopUpdateApi | null {
	if (typeof window === "undefined") {
		return null;
	}

	return ((window as Window & { lerosDesktop?: DesktopUpdateApi }).lerosDesktop ??
		null) as DesktopUpdateApi | null;
}

function DesktopUpdateMenuSection() {
	const desktopApi = getDesktopUpdateApi();
	const [updateState, setUpdateState] = useState<DesktopUpdateState>(initialDesktopUpdateState);
	const [checking, setChecking] = useState(false);

	useEffect(() => {
		if (!desktopApi) {
			return;
		}

		let mounted = true;
		void desktopApi.getState().then((state) => {
			if (mounted) {
				setUpdateState(state);
			}
		});

		const unsubscribe = desktopApi.subscribe((state) => {
			setUpdateState(state);
		});

		return () => {
			mounted = false;
			unsubscribe();
		};
	}, [desktopApi]);

	if (!desktopApi) {
		return null;
	}

	const handleCheckForUpdates = async () => {
		setChecking(true);
		try {
			const nextState = await desktopApi.checkForUpdates();
			setUpdateState(nextState);
			if (nextState.phase === "up-to-date") {
				toast.success("当前已经是最新版本");
			}
			if (nextState.phase === "unsupported") {
				toast.message(nextState.message);
			}
		} finally {
			setChecking(false);
		}
	};

	return (
		<div className="space-y-1">
			{updateState.phase === "downloading" && typeof updateState.progressPercent === "number" ? (
				<div className="px-2 pb-1">
					<div className="h-1.5 overflow-hidden rounded-full bg-slate-200">
						<div
							className="h-full rounded-full bg-[#34c59a] transition-all"
							style={{ width: `${Math.max(0, Math.min(updateState.progressPercent, 100))}%` }}
						/>
					</div>
				</div>
			) : null}

			<button
				type="button"
				className="flex w-full items-center gap-2 rounded-sm px-2 py-2 text-left text-sm text-slate-700 outline-none transition-colors hover:bg-accent hover:text-accent-foreground disabled:opacity-50"
				onClick={handleCheckForUpdates}
				disabled={!updateState.canCheck || checking}
			>
				{checking || updateState.phase === "checking" ? (
					<Loader2 className="size-4 animate-spin" />
				) : (
					<RefreshCcw className="size-4" />
				)}
				<span>检查更新</span>
			</button>
		</div>
	);
}

function getRouteActive(path: string, view: ViewMode) {
	if (view === "workbench") return path === "/" || path.startsWith("/workbench");
	if (view === "chat") return path.startsWith("/chat");
	if (view === "digitalAssistant") return path.startsWith("/assistants");
	if (view === "skills") return path.startsWith("/skills");
	if (view === "knowledge") return path.startsWith("/knowledge");
	if (view === "tasks") return path.startsWith("/tasks");
	return false;
}

function ProjectList({
	projects,
	activeProjectId,
	currentView,
	currentPath,
	onProjectClick,
	onRenameProject,
	onDeleteProject,
	collapsed,
	expanded,
	onExpand,
}: {
	projects: Project[];
	activeProjectId: string | null;
	currentView: ViewMode;
	currentPath?: string;
	onProjectClick: (projectId: string) => void;
	onRenameProject: (project: Project) => void;
	onDeleteProject: (project: Project) => void;
	collapsed: boolean;
	expanded: boolean;
	onExpand: () => void;
}) {
	const { visibleItems, showExpandTrigger } = getVisibleLeftRailItems(projects, expanded);

	return (
		<div className="space-y-1">
			{visibleItems.map((project) => {
				const active = currentPath
					? currentPath === `/projects/${project.id}` ||
						currentPath.startsWith(`/projects/${project.id}/`)
					: currentView === "project" && activeProjectId === project.id;
				return (
					// biome-ignore lint/a11y/useSemanticElements: The row contains a nested menu button, so the row itself cannot be a button.
					<div
						key={project.id}
						role="button"
						tabIndex={0}
						onClick={() => onProjectClick(project.id)}
						onKeyDown={(event) => {
							if (event.key === "Enter" || event.key === " ") {
								event.preventDefault();
								onProjectClick(project.id);
							}
						}}
						data-active={active}
						className={cn(
							"leros-nav-item group relative cursor-pointer text-sm",
							collapsed && "justify-center",
						)}
						title={collapsed ? project.name : undefined}
					>
						<span className="font-mono text-[14px] text-[var(--leros-text-subtle)]">#</span>
						<span className={cn("min-w-0 flex-1 truncate", collapsed && "hidden")}>
							{project.name}
						</span>
						{!collapsed && (
							<DropdownMenu>
								<DropdownMenuTrigger
									render={
										<button
											type="button"
											aria-label={`管理项目 ${project.name}`}
											className="flex size-6 shrink-0 items-center justify-center rounded-md text-[var(--leros-text-subtle)] opacity-0 transition-[opacity,background-color,color] duration-150 hover:bg-black/5 hover:text-[var(--leros-text-strong)] group-hover:opacity-100 group-focus-within:opacity-100 aria-expanded:opacity-100"
											onClick={(event) => event.stopPropagation()}
										>
											<MoreHorizontal className="size-4" />
										</button>
									}
								/>
								<DropdownMenuContent align="end" sideOffset={4}>
									<DropdownMenuItem onClick={() => onRenameProject(project)}>
										<Pencil className="size-3.5" />
										<span>重命名</span>
									</DropdownMenuItem>
									<DropdownMenuItem variant="destructive" onClick={() => onDeleteProject(project)}>
										<Trash2 className="size-3.5" />
										<span>删除</span>
									</DropdownMenuItem>
								</DropdownMenuContent>
							</DropdownMenu>
						)}
					</div>
				);
			})}
			{showExpandTrigger ? <ExpandMoreButton collapsed={collapsed} onClick={onExpand} /> : null}
		</div>
	);
}

function NavItemList({
	items,
	collapsed,
	expanded,
	onExpand,
	isItemActive,
	onItemClick,
}: {
	items: NavItem[];
	collapsed: boolean;
	expanded: boolean;
	onExpand: () => void;
	isItemActive: (item: NavItem) => boolean;
	onItemClick: (item: NavItem) => void;
}) {
	const { visibleItems, showExpandTrigger } = getVisibleLeftRailItems(items, expanded);

	return (
		<div className="space-y-1">
			{visibleItems.map((item) => (
				<NavItemButton
					key={item.id}
					item={item}
					active={isItemActive(item)}
					collapsed={collapsed}
					onClick={() => onItemClick(item)}
				/>
			))}
			{showExpandTrigger ? <ExpandMoreButton collapsed={collapsed} onClick={onExpand} /> : null}
		</div>
	);
}

function ExpandMoreButton({ collapsed, onClick }: { collapsed: boolean; onClick: () => void }) {
	return (
		<button
			type="button"
			onClick={onClick}
			className={cn(
				"leros-nav-item text-[var(--leros-text-subtle)] transition-colors hover:text-[var(--leros-text-strong)]",
				collapsed && "justify-center",
			)}
			title={collapsed ? "展开更多" : undefined}
		>
			<span className="font-mono text-[16px] leading-none">...</span>
			<span className={cn("min-w-0 flex-1 truncate", collapsed && "hidden")}>展开更多</span>
		</button>
	);
}

function NavItemButton({
	item,
	active,
	collapsed,
	onClick,
}: {
	item: NavItem;
	active: boolean;
	collapsed: boolean;
	onClick: () => void;
}) {
	const icon =
		item.icon === "IconAITeammate" ? (
			<DiceBearAvatar
				seed={`ai-teammate:${item.label}`}
				alt=""
				className="h-full w-full"
				size={64}
			/>
		) : (
			iconMap[item.icon]
		);

	return (
		<button
			type="button"
			onClick={onClick}
			data-active={active}
			className={cn("leros-nav-item", collapsed && "justify-center")}
			title={collapsed ? item.label : undefined}
		>
			<span
				className={cn(
					"leros-nav-icon",
					item.icon === "IconProject" && "leros-nav-icon-text",
					item.icon === "IconAITeammate" && "leros-nav-icon-avatar",
				)}
			>
				{icon}
			</span>
			<span className={cn("flex-1 truncate font-medium", collapsed && "hidden")}>{item.label}</span>
			{item.badge ? (
				item.icon === "IconAITeammate" ? (
					<div
						className={cn(
							"h-1.5 w-1.5 shrink-0 rounded-full bg-[var(--leros-primary)]",
							collapsed ? "absolute right-2 top-2" : "mr-1",
						)}
					/>
				) : (
					<span
						className={cn(
							"rounded-full bg-destructive/10 px-1.5 py-0.5 text-xs text-destructive",
							collapsed ? "absolute right-1.5 top-1.5" : "ml-auto",
						)}
					>
						{item.badge}
					</span>
				)
			) : null}
		</button>
	);
}
