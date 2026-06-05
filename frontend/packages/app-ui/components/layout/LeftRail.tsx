"use client";

import type { NavItem, Project, ViewMode } from "@leros/store";
import { useLayoutStore } from "@leros/store";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@leros/ui/components/ui/dropdown-menu";
import { ScrollArea } from "@leros/ui/components/ui/scroll-area";
import { cn } from "@leros/ui/lib/utils";
import {
	ChevronsLeft,
	CircleHelp,
	ClipboardList,
	Database,
	Hash,
	LayoutGrid,
	LogOut,
	MoreVertical,
	Network,
	Settings,
	UserRound,
	Zap,
} from "lucide-react";
import { useEffect } from "react";
import { useAuth } from "../auth";

export type AppNavigation = {
	currentPath: string;
	goToRoute: (route: ViewMode) => void;
	goToProject: (projectId: string) => void;
	goToTaskDetail: (projectId: string, taskId: string, sessionId?: string | null) => void;
};

const avatarMap: Record<string, string> = {
	"Ada AI":
		"https://lh3.googleusercontent.com/aida-public/AB6AXuDFpBbS4l95muQqtwMYtUuf8WCwNc5sA8OO0-6u1LGuYyluoaArOURURsMTCrMq_NupAuGHz-JOO1FokisXhPwW2YHHw98AiRCPLBB7pnEkJtJ49IFY1oAvXh91Jm-_COCvYzzzLBiaLG-LYG1u2FkKZ0I32-W4xkWSIw9t0g-REw0_7AApPcTHTUs6YXhMUR8CRrgkQwLTEXmTGIXKdTeB49LdA0NLB84cpa3IeofhyuLdIwA_DqEbSLLGdzjPLvMzaF8LprQnlCI",
	Hopper:
		"https://lh3.googleusercontent.com/aida-public/AB6AXuBeB5b4oXNn4L2BxiToWnXKcmpiqIOQXHgzr--j9T9_QOXVd9oHi1Fm6w-TFVrtUCrsljLwuZTLgUsQO_bm-5a-pTeEhYiqC-XWGCFm29XVQNzs1K_BZsauTofNldKOlXXqefrOEws7yf2OugGY02bc3tTG6Ar6LK_vtTM0LIGPIUtjF4hXiV6_JC78AZjUIIcQ9ZyIsXqZHT4w005HdcD-k2UMVDi9B4zKpMqsRbKjO_uJgC-cMhnEekpNM3Tao6dm5c2dEHGt1m4",
	Mia: "https://lh3.googleusercontent.com/aida-public/AB6AXuBF0owbtXZ299YjKA9U1M8sCOv64scrlTj0dggJ4QzZ3LVWiwaw6F2wdlx-pfng186UXwb39pUr6UYaB3TR0VgvyCzHeq_ftW0GiYK6opisJR6rW9cI41epBVwQ01amJW2zeCfuSC4bO9eHQmG3birvJfEvqhddLBP9UAyGwjti4KWyfS5HGYrOGMI1T2aGvaWbAMOO-dYq22Ezmpl3PWzyb7yd1yYy2LEOqAOSuhmadQKH90cgkhBTISnC5mE8jOrwmrdZuF-Fvs4",
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
	logoSrc = "/logo.svg",
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
		fetchProjects,
		switchView,
		switchProject,
	} = useLayoutStore((s) => s);
	const { isAuthenticated, openAuthDialog, requireAuth, logout, user } = useAuth();

	useEffect(() => {
		fetchProjects();
	}, [fetchProjects]);

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

	return (
		<aside className="leros-sidebar">
			<div className="leros-brand">
				<div className="flex items-center gap-3">
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
					<div className="min-w-0">
						<div className="leros-brand-title">Leros AI</div>
						<div className="leros-brand-version">v0.1</div>
					</div>
				</div>
				<button
					type="button"
					className="text-[var(--leros-text-subtle)] transition-colors hover:text-[var(--leros-text)]"
					aria-label="收起侧边栏"
				>
					<ChevronsLeft className="size-[18px]" />
				</button>
			</div>

			<ScrollArea className="min-h-0 flex-1 overflow-hidden">
				<nav className="leros-nav" aria-label="主导航">
					{navGroups.map((group) => {
						return (
							<div key={group.id} className="leros-nav-section">
								{group.label && <div className="leros-nav-section-label">{group.label}</div>}
								{group.id === "projects" ? (
									<ProjectList
										projects={projects}
										activeProjectId={activeProjectId}
										currentView={currentView}
										currentPath={navigation?.currentPath}
										onProjectClick={handleProjectClick}
									/>
								) : (
									<div className="space-y-1">
										{group.items.map((item: NavItem) => (
											<NavItemButton
												key={item.id}
												item={item}
												active={isItemActive(item)}
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
								<button type="button" className="leros-profile-trigger">
									<ProfileAvatar />
									<div className="flex-1 overflow-hidden text-left">
										<p className="truncate text-[14px] font-bold text-[var(--leros-text-strong)]">
											{user?.name ?? "个人中心"}
										</p>
										<p className="text-[10px] font-bold uppercase tracking-tight text-[var(--leros-primary)]">
											PREMIUM
										</p>
									</div>
									<MoreVertical className="size-4 shrink-0 text-[var(--leros-text-subtle)]" />
								</button>
							}
						/>
						<DropdownMenuContent
							align="end"
							side="top"
							sideOffset={10}
							className="leros-profile-menu"
						>
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
							<DropdownMenuItem variant="destructive" onClick={handleLogout}>
								<LogOut className="size-4" />
								<span>退出登录</span>
							</DropdownMenuItem>
						</DropdownMenuContent>
					</DropdownMenu>
				) : (
					<button type="button" className="leros-profile-trigger" onClick={handleProfileClick}>
						<ProfileAvatar />
						<div className="flex-1 overflow-hidden text-left">
							<p className="truncate text-[14px] font-bold text-[var(--leros-text-strong)]">
								登录 / 注册
							</p>
							<p className="text-[10px] font-bold uppercase tracking-tight text-[var(--leros-primary)]">
								LEROS
							</p>
						</div>
						<UserRound className="size-4 shrink-0 text-[var(--leros-text-subtle)]" />
					</button>
				)}
			</div>
		</aside>
	);
}

function ProfileAvatar() {
	return (
		<span className="leros-avatar overflow-hidden object-cover">
			<img
				src="https://lh3.googleusercontent.com/aida-public/AB6AXuBF0owbtXZ299YjKA9U1M8sCOv64scrlTj0dggJ4QzZ3LVWiwaw6F2wdlx-pfng186UXwb39pUr6UYaB3TR0VgvyCzHeq_ftW0GiYK6opisJR6rW9cI41epBVwQ01amJW2zeCfuSC4bO9eHQmG3birvJfEvqhddLBP9UAyGwjti4KWyfS5HGYrOGMI1T2aGvaWbAMOO-dYq22Ezmpl3PWzyb7yd1yYy2LEOqAOSuhmadQKH90cgkhBTISnC5mE8jOrwmrdZuF-Fvs4"
				alt="Avatar"
				className="w-full h-full object-cover"
			/>
		</span>
	);
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
}: {
	projects: Project[];
	activeProjectId: string | null;
	currentView: ViewMode;
	currentPath?: string;
	onProjectClick: (projectId: string) => void;
}) {
	return (
		<div className="space-y-1">
			{projects.map((project) => {
				const active = currentPath
					? currentPath === `/projects/${project.id}` ||
						currentPath.startsWith(`/projects/${project.id}/`)
					: currentView === "project" && activeProjectId === project.id;
				return (
					<button
						key={project.id}
						type="button"
						onClick={() => onProjectClick(project.id)}
						className={cn(
							"flex w-full items-center gap-3 rounded-[var(--leros-radius-sm)] px-3 py-1.5 text-left text-sm transition-colors",
							active
								? "bg-[var(--leros-primary-softer)] font-semibold text-[var(--leros-primary)]"
								: "text-[var(--leros-text-muted)] hover:text-[var(--leros-text-strong)]",
						)}
					>
						<span className="font-mono text-[14px] text-[var(--leros-text-subtle)]">#</span>
						<span className="truncate">{project.name}</span>
					</button>
				);
			})}
		</div>
	);
}

function NavItemButton({
	item,
	active,
	onClick,
}: {
	item: NavItem;
	active: boolean;
	onClick: () => void;
}) {
	const avatarUrl = item.icon === "IconAITeammate" ? avatarMap[item.label] : null;

	const icon = avatarUrl ? (
		<img src={avatarUrl} alt="" className="h-6 w-6 flex-shrink-0 rounded-full object-cover" />
	) : (
		iconMap[item.icon]
	);

	return (
		<button type="button" onClick={onClick} data-active={active} className="leros-nav-item">
			<span className={cn("leros-nav-icon", item.icon === "IconProject" && "leros-nav-icon-text")}>
				{icon}
			</span>
			<span className="flex-1 truncate font-medium">{item.label}</span>
			{item.badge ? (
				item.icon === "IconAITeammate" ? (
					<div className="mr-1 h-1.5 w-1.5 shrink-0 rounded-full bg-[var(--leros-primary)]" />
				) : (
					<span className="ml-auto rounded-full bg-destructive/10 px-1.5 py-0.5 text-xs text-destructive">
						{item.badge}
					</span>
				)
			) : null}
		</button>
	);
}
