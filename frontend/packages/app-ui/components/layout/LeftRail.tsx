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
	BookOpen,
	ChevronDown,
	CircleCheck,
	CircleHelp,
	Hash,
	LayoutGrid,
	LogOut,
	Network,
	Puzzle,
	Settings,
	UserRound,
} from "lucide-react";

const iconMap: Record<string, React.ReactNode> = {
	IconWorkbench: <LayoutGrid className="size-5" />,
	IconTask: <CircleCheck className="size-5" />,
	IconSkill: <Puzzle className="size-5" />,
	IconKnowledge: <BookOpen className="size-5" />,
	IconProject: <Hash className="size-4" />,
};

const navIdToView: Record<string, ViewMode> = {
	workbench: "workbench",
	tasks: "tasks",
	knowledge: "knowledge",
	skills: "skills",
	"ai-1": "digitalAssistant",
};

export function LeftRail({ logoSrc = "/logo.svg" }: { logoSrc?: string }) {
	const { navGroups, projects, currentView, activeProjectId, switchView, switchProject } =
		useLayoutStore((s) => s);

	const handleNavClick = (item: NavItem) => {
		const view = navIdToView[item.id] ?? "chat";
		switchView(view);
	};

	const isItemActive = (item: NavItem) => {
		const view = navIdToView[item.id] ?? "chat";
		return currentView === view;
	};

	return (
		<aside className="leros-sidebar">
			<div className="leros-brand">
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

			<ScrollArea className="min-h-0 flex-1">
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
										onProjectClick={switchProject}
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
				<DropdownMenu>
					<DropdownMenuTrigger
						render={
							<button type="button" className="leros-profile-trigger">
								<span className="leros-avatar">
									<UserRound className="size-4" />
								</span>
								<span className="min-w-0 flex-1 truncate text-left font-medium">个人中心</span>
								<ChevronDown className="size-4 text-[var(--leros-text-muted)]" />
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
						<DropdownMenuItem variant="destructive">
							<LogOut className="size-4" />
							<span>退出登录</span>
						</DropdownMenuItem>
					</DropdownMenuContent>
				</DropdownMenu>
			</div>
		</aside>
	);
}

function ProjectList({
	projects,
	activeProjectId,
	currentView,
	onProjectClick,
}: {
	projects: Project[];
	activeProjectId: string | null;
	currentView: ViewMode;
	onProjectClick: (projectId: string) => void;
}) {
	return (
		<div className="space-y-1">
			{projects.map((project) => (
				<button
					key={project.id}
					type="button"
					onClick={() => onProjectClick(project.id)}
					data-active={currentView === "project" && activeProjectId === project.id}
					className="leros-nav-item"
				>
					<span className="leros-nav-icon leros-nav-icon-text">
						<Hash className="size-4" />
					</span>
					<span className="truncate font-medium">{project.name}</span>
				</button>
			))}
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
	const icon =
		item.icon === "IconAITeammate" ? (
			<span className="leros-ai-token">{item.label.replace(/\s/g, "")}</span>
		) : (
			iconMap[item.icon]
		);
	return (
		<button type="button" onClick={onClick} data-active={active} className="leros-nav-item">
			<span className={cn("leros-nav-icon", item.icon === "IconProject" && "leros-nav-icon-text")}>
				{icon}
			</span>
			<span className="truncate font-medium">{item.label}</span>
			{item.badge && (
				<span className="ml-auto rounded-full bg-red-100 px-1.5 py-0.5 text-xs text-red-600">
					{item.badge}
				</span>
			)}
		</button>
	);
}
