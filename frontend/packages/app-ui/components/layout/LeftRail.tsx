"use client";

import type { AuthUser, NavItem, Project, ViewMode } from "@leros/store";
import { useLayoutStore } from "@leros/store";
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
	DropdownMenuTrigger,
} from "@leros/ui/components/ui/dropdown-menu";
import { ScrollArea } from "@leros/ui/components/ui/scroll-area";
import { cn } from "@leros/ui/lib/utils";
import {
	ChevronsLeft,
	ChevronsRight,
	ClipboardList,
	Database,
	Hash,
	LayoutGrid,
	LogOut,
	MoreHorizontal,
	Network,
	Pencil,
	Trash2,
	UserRound,
	Zap,
} from "lucide-react";
import type { PointerEvent as ReactPointerEvent } from "react";
import { useEffect, useRef, useState } from "react";
import { APP_LOGO_SRC } from "../../assets";
import { useAuth } from "../auth";
import { DiceBearAvatar } from "../avatar/DiceBearAvatar";

const LEFT_RAIL_WIDTH_STORAGE_KEY = "leros-left-rail-width";
const LEFT_RAIL_COLLAPSED_STORAGE_KEY = "leros-left-rail-collapsed";
const LEFT_RAIL_COLLAPSED_WIDTH = 72;

export type AppNavigation = {
	currentPath: string;
	goToRoute: (route: ViewMode) => void;
	goToProject: (projectId: string) => void;
	goToTaskDetail: (projectId: string, taskId: string, sessionId?: string | null) => void;
};

const iconMap: Record<string, React.ReactNode> = {
	IconWorkbench: <LayoutGrid className="size-5" />,
	IconTask: <ClipboardList className="size-5" />,
	IconSkill: <Zap className="size-5" />,
	IconKnowledge: <Database className="size-5" />,
	IconProject: <Hash className="size-4" />,
};

const navIdToView: Record<string, ViewMode> = {
	workbench: "workbench",
	tasks: "tasks",
	knowledge: "knowledge",
	skills: "skills",
	"ai-1": "digitalAssistant",
	"ai-2": "digitalAssistant",
	"ai-3": "digitalAssistant",
};

const protectedNavIds = new Set(["tasks", "skills", "knowledge"]);

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
	const { isAuthenticated, openAuthDialog, requireAuth, logout, user } = useAuth();
	const hasLoadedPreferenceRef = useRef(false);
	const [renameProject, setRenameProject] = useState<Project | null>(null);
	const [renameValue, setRenameValue] = useState("");
	const [deleteTarget, setDeleteTarget] = useState<Project | null>(null);

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

	return (
		<aside
			className="leros-sidebar"
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
						<div className="leros-brand-title">Leros AI</div>
						<div className="leros-brand-version">v0.1</div>
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

			<ScrollArea className="min-h-0 flex-1 overflow-hidden">
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
				{isAuthenticated ? (
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
											{user?.name ?? "Leros 用户"}
										</p>
										<p className="truncate text-[11px] text-[var(--leros-text-subtle)]">
											{user?.email ?? "已登录"}
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
							<DropdownMenuSeparator />
							*/}
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
		</aside>
	);
}

function ProfileAvatar({ user }: { user: AuthUser | null }) {
	const fallbackLabel = getAvatarInitial(user?.name ?? user?.email ?? "Leros");

	return (
		<span
			className="leros-avatar overflow-hidden text-[11px] font-bold"
			style={{ background: "var(--leros-primary)", color: "#fff" }}
		>
			<ImageWithFallback
				src={user?.avatarUrl}
				alt={user?.name ?? "Avatar"}
				className="h-full w-full object-cover"
				fallback={
					user ? (
						<DiceBearAvatar
							seed={`user:${user.email || user.name}`}
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

function ImageWithFallback({
	src,
	alt,
	className,
	fallback,
}: {
	src?: string | null;
	alt: string;
	className: string;
	fallback: React.ReactNode;
}) {
	const [failed, setFailed] = useState(false);

	if (!src || failed) return <>{fallback}</>;

	return (
		<img
			src={src}
			alt={alt}
			className={className}
			loading="lazy"
			decoding="async"
			referrerPolicy="no-referrer"
			onError={() => setFailed(true)}
		/>
	);
}

function getAvatarInitial(label: string) {
	const trimmed = label.trim();
	return (trimmed[0] ?? "L").toUpperCase();
}

function getRouteActive(path: string, view: ViewMode) {
	if (view === "workbench") return path === "/" || path.startsWith("/workbench");
	if (view === "chat") return path.startsWith("/chat");
	if (view === "digitalAssistant") return path.startsWith("/assistants");
	if (view === "skills") return path.startsWith("/skills");
	if (view === "knowledge") return path.startsWith("/knowledge");
	if (view === "settings") return path.startsWith("/settings");
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
}: {
	projects: Project[];
	activeProjectId: string | null;
	currentView: ViewMode;
	currentPath?: string;
	onProjectClick: (projectId: string) => void;
	onRenameProject: (project: Project) => void;
	onDeleteProject: (project: Project) => void;
	collapsed: boolean;
}) {
	return (
		<div className="space-y-1">
			{projects.map((project) => {
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
		</div>
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
