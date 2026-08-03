"use client";

import {
	type AuthUser,
	type PluginComposerOption,
	type PluginListItem,
	type Project,
	type ProjectMember,
	pluginApi,
	projectMembersToInputs,
	useLayoutStore,
	useProjectsMenuCapabilities,
} from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import {
	Command,
	CommandEmpty,
	CommandGroup,
	CommandInput,
	CommandItem,
	CommandList,
	CommandSeparator,
} from "@leros/ui/components/ui/command";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@leros/ui/components/ui/dialog";
import { Popover, PopoverContent, PopoverTrigger } from "@leros/ui/components/ui/popover";
import { cn } from "@leros/ui/lib/utils";
import {
	Bot,
	CalendarDays,
	Check,
	MessageSquare,
	Plus,
	Search,
	Server,
	Sparkles,
	X,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { useAuth } from "../auth";
import { MCPConnectorIcon } from "../common/MCPConnectorIcon";
import { renderHighlightedText } from "../common/searchText";
import { FieldWithError, RequiredMark, useFormFieldValidation } from "../form/formValidation";
import { bindSkillsToProject, useSkillPickerOptions } from "../input/useSkillPickerOptions";
import type { AppNavigation } from "../layout/LeftRail";
import { ProjectIcon } from "../layout/project-icon";
import { ProjectActionsDropdown } from "../project/ProjectActionsDropdown";
import {
	ProjectMemberChip,
	ProjectMemberPickerDialog,
	projectMemberChipClassName,
	projectMemberListClassName,
} from "../project-members/ProjectMemberPickerDialog";

type ProjectsHubViewProps = {
	navigation?: AppNavigation;
};

function authUserToOwnerMember(user: AuthUser | null): ProjectMember[] {
	if (!user?.publicId) return [];
	return [
		{
			id: `user-${user.publicId}`,
			memberId: user.uin ?? 0,
			publicId: user.publicId,
			type: "user",
			role: "owner",
			// 中文注释：与左下角个人中心一致，优先组织内 uin_name，避免跨组织显示成全局 name。
			name: user.uinName || user.name || user.phone || user.email || "我",
			description: [user.email, user.phone].filter(Boolean).join(" / "),
			avatarUrl: user.avatarUrl,
		},
	];
}

function mergeOwnerMember(
	ownerMembers: ProjectMember[],
	members: ProjectMember[],
): ProjectMember[] {
	if (ownerMembers.length === 0) return members;
	const owner = ownerMembers[0];
	if (!owner) return members;
	const withoutOwner = members.filter(
		(member) => !(member.type === owner.type && member.publicId === owner.publicId),
	);
	// 中文注释：创建项目时当前用户固定作为 owner 成员，避免需要手动再选一次自己。
	return [owner, ...withoutOwner];
}

function formatProjectDate(timestamp: number) {
	if (!timestamp) return "未知时间";
	return new Date(timestamp).toLocaleString("zh-CN", {
		year: "numeric",
		month: "2-digit",
		day: "2-digit",
		hour: "2-digit",
		minute: "2-digit",
		second: "2-digit",
		hour12: false,
	});
}

export function ProjectsHubView({ navigation }: ProjectsHubViewProps) {
	const {
		projects,
		fetchProjects,
		createProject,
		updateProject,
		deleteProject,
		leaveProject,
		setProjectRoute,
		activeProjectId,
		switchView,
	} = useLayoutStore((s) => s);
	const { isAuthenticated, requireAuth } = useAuth();
	const visibleProjects = isAuthenticated ? projects : [];
	useProjectsMenuCapabilities(visibleProjects.map((project) => project.id));
	const [keyword, setKeyword] = useState("");
	const [createOpen, setCreateOpen] = useState(false);
	const [renameProject, setRenameProject] = useState<Project | null>(null);
	const [renameValue, setRenameValue] = useState("");
	const [deleteTarget, setDeleteTarget] = useState<Project | null>(null);
	const [leaveTarget, setLeaveTarget] = useState<Project | null>(null);

	useEffect(() => {
		if (isAuthenticated) return;
		setCreateOpen(false);
		setRenameProject(null);
		setDeleteTarget(null);
		setLeaveTarget(null);
	}, [isAuthenticated]);

	useEffect(() => {
		if (!isAuthenticated) return;
		fetchProjects();
	}, [fetchProjects, isAuthenticated]);

	const filteredProjects = useMemo(() => {
		const query = keyword.trim().toLowerCase();
		const sorted = [...visibleProjects].sort((a, b) => b.createdAt - a.createdAt);
		if (!query) return sorted;

		return sorted.filter((project) =>
			[project.name, project.description].join(" ").toLowerCase().includes(query),
		);
	}, [keyword, visibleProjects]);

	const openProject = (projectId: string) => {
		requireAuth(() => {
			setProjectRoute(projectId, "chat");
			navigation?.goToProject(projectId);
		});
	};

	const openCreateDialog = () => {
		if (!isAuthenticated) {
			requireAuth(() => setCreateOpen(true));
			return;
		}
		setCreateOpen(true);
	};

	const handleProjectCreated = (project: Project) => {
		setCreateOpen(false);
		toast.success("项目创建成功");
		openProject(project.id);
	};

	const openRename = (project: Project) => {
		setRenameProject(project);
		setRenameValue(project.name);
	};

	const confirmRename = async () => {
		if (!renameProject) return;
		const name = renameValue.trim();
		if (!name) return;

		const updatedProject = await updateProject({
			public_id: renameProject.id,
			name,
		});
		if (updatedProject) {
			setRenameProject(null);
			toast.success("项目已重命名");
		}
	};

	const confirmDelete = async () => {
		if (!deleteTarget) return;
		const deleted = await deleteProject(deleteTarget.id);
		if (deleted) {
			setDeleteTarget(null);
			toast.success("项目已删除");
		}
	};

	const confirmLeave = async () => {
		if (!leaveTarget) return;

		const leavingActiveProject =
			activeProjectId === leaveTarget.id ||
			navigation?.currentPath === `/projects/${leaveTarget.id}` ||
			navigation?.currentPath?.startsWith(`/projects/${leaveTarget.id}/`);

		const left = await leaveProject(leaveTarget.id);
		if (!left) return;

		setLeaveTarget(null);
		toast.success("已离开项目");

		if (leavingActiveProject) {
			if (navigation) {
				navigation.goToRoute("workbench");
				return;
			}
			switchView("workbench");
		}
	};

	return (
		<div
			data-slot="projects-hub-view"
			className="flex h-full min-h-0 flex-1 flex-col bg-[var(--leros-app-bg)]"
		>
			<header className="shrink-0 border-b border-[var(--leros-control-border)] px-6 py-5">
				<div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
					<div>
						<h1 className="text-xl font-semibold text-[var(--leros-text-strong)]">项目</h1>
						<p className="mt-2 text-sm text-[var(--leros-text-muted)]">多人协同，打造超级团队</p>
					</div>
					<div className="flex w-full flex-col gap-3 sm:flex-row sm:items-center lg:w-auto">
						<div className="relative flex-1 sm:min-w-[240px] lg:w-[280px]">
							<Search className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-[var(--leros-text-subtle)]" />
							<input
								type="text"
								value={keyword}
								onChange={(event) => setKeyword(event.target.value)}
								placeholder="搜索项目"
								className="h-9 w-full rounded-lg border border-[var(--leros-control-border)] bg-[var(--leros-surface)] pl-9 pr-3 text-sm text-[var(--leros-text)] placeholder:text-[var(--leros-text-subtle)] transition-colors focus:border-[var(--leros-primary)] focus:outline-none"
							/>
						</div>
						<Button type="button" size="sm" className="shrink-0 gap-2" onClick={openCreateDialog}>
							<Plus className="size-4" />
							新建项目
						</Button>
					</div>
				</div>
			</header>

			<main className="flex min-h-0 flex-1 flex-col px-6 py-6">
				<div className="mb-4 flex shrink-0 items-center justify-between gap-4">
					<h2 className="text-lg font-semibold text-[var(--leros-text-strong)]">我的项目</h2>
					<span className="text-xs text-[var(--leros-text-subtle)]">
						{filteredProjects.length} 个项目
					</span>
				</div>

				<div className="min-h-0 flex-1 overflow-y-auto pr-1 no-scrollbar">
					{filteredProjects.length === 0 ? (
						<div className="flex min-h-[280px] flex-col items-center justify-center rounded-xl border border-dashed border-[var(--leros-control-border)] bg-white/70 px-6 text-center">
							<div className="mb-3 flex size-12 items-center justify-center rounded-xl bg-[var(--leros-primary-softer)] text-[var(--leros-primary)]">
								<ProjectIcon className="size-6" />
							</div>
							<p className="text-sm font-semibold text-[var(--leros-text-strong)]">还没有项目</p>
							<p className="mt-1 text-sm text-[var(--leros-text-muted)]">
								点击右上角新建一个空项目。
							</p>
						</div>
					) : (
						<div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
							{filteredProjects.map((project) => (
								<ProjectCard
									key={project.id}
									project={project}
									onOpen={openProject}
									onRename={openRename}
									onDelete={setDeleteTarget}
									onLeave={setLeaveTarget}
								/>
							))}
						</div>
					)}
				</div>
			</main>

			<CreateProjectDialog
				open={createOpen}
				onOpenChange={setCreateOpen}
				onCreate={createProject}
				onCreated={handleProjectCreated}
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
					<div className="mt-4 relative">
						<input
							type="text"
							value={renameValue}
							onChange={(event) => setRenameValue(event.target.value)}
							onKeyDown={(event) => {
								if (event.key === "Enter") {
									confirmRename();
								}
							}}
							placeholder="项目名称"
							maxLength={30}
							autoFocus
							className="w-full rounded-md border border-slate-200 bg-white px-3 py-2 pr-14 text-sm text-slate-800 placeholder:text-slate-400 transition-colors focus:border-blue-300 focus:outline-none"
						/>
						<span className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-xs text-slate-400">
							{renameValue.length}/30
						</span>
					</div>
					<DialogFooter className="mt-4">
						<Button variant="outline" onClick={() => setRenameProject(null)}>
							取消
						</Button>
						<Button onClick={confirmRename} disabled={!renameValue.trim()}>
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
						<Button variant="destructive" onClick={confirmDelete}>
							删除
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>

			<Dialog open={leaveTarget !== null} onOpenChange={(open) => !open && setLeaveTarget(null)}>
				<DialogContent className="sm:max-w-md" showCloseButton={false}>
					<DialogHeader>
						<DialogTitle>离开项目</DialogTitle>
						<DialogDescription>
							确定要离开 <strong>{leaveTarget?.name}</strong> 吗？离开后你将无法继续访问该项目。
						</DialogDescription>
					</DialogHeader>
					<DialogFooter className="mt-4">
						<Button variant="outline" onClick={() => setLeaveTarget(null)}>
							取消
						</Button>
						<Button variant="destructive" onClick={confirmLeave}>
							离开
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		</div>
	);
}

function ProjectCard({
	project,
	onOpen,
	onRename,
	onDelete,
	onLeave,
}: {
	project: Project;
	onOpen: (projectId: string) => void;
	onRename: (project: Project) => void;
	onDelete: (project: Project) => void;
	onLeave: (project: Project) => void;
}) {
	const [actionsOpen, setActionsOpen] = useState(false);

	return (
		// biome-ignore lint/a11y/useSemanticElements: The card contains a nested menu button, so the card itself cannot be a button.
		<div
			role="button"
			tabIndex={0}
			className={cn(
				"group relative flex min-h-[132px] w-full cursor-pointer flex-col rounded-lg border border-slate-200 bg-white p-4 text-left transition-colors",
				"hover:border-blue-200 hover:bg-blue-50/30",
			)}
			onClick={() => {
				if (actionsOpen) return;
				onOpen(project.id);
			}}
			onKeyDown={(event) => {
				if (event.key === "Enter" || event.key === " ") {
					event.preventDefault();
					if (actionsOpen) return;
					onOpen(project.id);
				}
			}}
		>
			<div className="mb-3 flex items-start gap-3 pr-7">
				<div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-[var(--leros-surface-soft)] text-[var(--leros-text-muted)] transition-colors group-hover:bg-[var(--leros-primary-soft)] group-hover:text-[var(--leros-primary)]">
					<ProjectIcon className="size-5" />
				</div>
				<div className="min-w-0 flex-1">
					<h3 className="truncate text-sm font-semibold text-[var(--leros-text-strong)]">
						{project.name}
					</h3>
					<p className="mt-1 line-clamp-2 text-xs leading-5 text-[var(--leros-text-muted)]">
						{project.description || "暂无项目描述"}
					</p>
				</div>
			</div>

			<div className="mt-auto flex items-center justify-between gap-3 text-xs text-[var(--leros-text-subtle)]">
				<div className="flex min-w-0 items-center gap-1.5">
					<CalendarDays className="size-3.5 shrink-0" />
					<span className="truncate">创建于 {formatProjectDate(project.createdAt)}</span>
				</div>
				<div className="flex shrink-0 items-center gap-1.5">
					<MessageSquare className="size-3.5" />
					<span>{project.taskCount} 个任务</span>
				</div>
			</div>

			<ProjectActionsDropdown
				project={project}
				onRename={onRename}
				onDelete={onDelete}
				onLeave={onLeave}
				variant="card"
				onOpenChange={setActionsOpen}
			/>
		</div>
	);
}

function CreateProjectDialog({
	open,
	onOpenChange,
	onCreate,
	onCreated,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	onCreate: (params: {
		name: string;
		description?: string;
		members?: { type: "assistant" | "user"; id: string }[];
		metadata?: Record<string, unknown>;
	}) => Promise<Project | null>;
	onCreated: (project: Project) => void;
}) {
	const { user } = useAuth();
	const ownerMembers = useMemo(() => authUserToOwnerMember(user), [user]);
	const [name, setName] = useState("");
	const [description, setDescription] = useState("");
	const [selectedMembers, setSelectedMembers] = useState<ProjectMember[]>(ownerMembers);
	const [memberOpen, setMemberOpen] = useState(false);
	const [selectedSkills, setSelectedSkills] = useState<PluginComposerOption[]>([]);
	const [skillOpen, setSkillOpen] = useState(false);
	const [skillSearch, setSkillSearch] = useState("");
	const [selectedMCPs, setSelectedMCPs] = useState<PluginListItem[]>([]);
	const [mcpOptions, setMCPOptions] = useState<PluginListItem[]>([]);
	const [mcpOpen, setMCPOpen] = useState(false);
	const [mcpSearch, setMCPSearch] = useState("");
	const [mcpsLoading, setMCPsLoading] = useState(false);
	const [submitting, setSubmitting] = useState(false);
	const { resetValidation, shouldShowError, handleFieldBlur } = useFormFieldValidation();
	const { skillOptions, skillsLoading, skillsError } = useSkillPickerOptions({
		includeBuiltin: false,
		enabled: open && skillOpen,
	});

	useEffect(() => {
		if (!open) {
			setName("");
			setDescription("");
			setSelectedMembers(ownerMembers);
			setMemberOpen(false);
			setSelectedSkills([]);
			setSkillSearch("");
			setSelectedMCPs([]);
			setMCPOptions([]);
			setMCPSearch("");
			setSubmitting(false);
			resetValidation();
			return;
		}
		setSelectedMembers((current) => mergeOwnerMember(ownerMembers, current));
	}, [open, ownerMembers, resetValidation]);

	useEffect(() => {
		if (!open || !mcpOpen) return;
		let cancelled = false;
		setMCPsLoading(true);
		pluginApi
			.list({ kind: "mcp", status: "active", limit: 100 })
			.then((response) => {
				if (!cancelled) setMCPOptions(response.data.data.plugins ?? []);
			})
			.catch(() => {
				if (!cancelled) setMCPOptions([]);
			})
			.finally(() => {
				if (!cancelled) setMCPsLoading(false);
			});
		return () => {
			cancelled = true;
		};
	}, [mcpOpen, open]);

	const selectedSkillCodes = useMemo(
		() => selectedSkills.map((skill) => skill.code),
		[selectedSkills],
	);
	const selectedSkillCodeSet = useMemo(
		() => new Set(selectedSkillCodes.map((code) => code.toLowerCase())),
		[selectedSkillCodes],
	);
	const filteredSkills = useMemo(() => {
		const query = skillSearch.trim().toLowerCase();
		return (skillOptions ?? []).filter((skill) => {
			if (selectedSkillCodeSet.has(skill.code.toLowerCase())) return false;
			if (!query) return true;
			return [skill.label, skill.code, skill.description].join(" ").toLowerCase().includes(query);
		});
	}, [selectedSkillCodeSet, skillOptions, skillSearch]);
	const selectedMCPIDs = useMemo(
		() => new Set(selectedMCPs.map((connector) => connector.public_id)),
		[selectedMCPs],
	);
	const filteredMCPs = useMemo(() => {
		const query = mcpSearch.trim().toLowerCase();
		return mcpOptions.filter((connector) => {
			if (selectedMCPIDs.has(connector.public_id)) return false;
			if (!query) return true;
			return [connector.name, connector.code, connector.description]
				.filter(Boolean)
				.join(" ")
				.toLowerCase()
				.includes(query);
		});
	}, [mcpOptions, mcpSearch, selectedMCPIDs]);

	const addSkill = (skill: PluginComposerOption) => {
		setSelectedSkills((current) => {
			if (current.some((item) => item.code.toLowerCase() === skill.code.toLowerCase())) {
				return current;
			}
			return [...current, skill];
		});
	};

	const removeSkill = (skillCode: string) => {
		setSelectedSkills((current) =>
			current.filter((skill) => skill.code.toLowerCase() !== skillCode.toLowerCase()),
		);
	};

	const trimmedName = name.trim();
	const nameValid = trimmedName.length > 0;
	const showNameError = shouldShowError("name") && !nameValid;

	const submit = async () => {
		if (!nameValid || submitting) return;

		setSubmitting(true);
		try {
			const project = await onCreate({
				name: trimmedName,
				description: description.trim(),
				members: projectMembersToInputs(mergeOwnerMember(ownerMembers, selectedMembers)),
			});
			if (project) {
				const binding = await bindSkillsToProject(project.id, selectedSkills);
				const mcpResults = await Promise.allSettled(
					selectedMCPs.map((connector) =>
						pluginApi.addToProject({
							public_id: project.id,
							plugin_id: connector.public_id,
						}),
					),
				);
				const failedMCPCount = mcpResults.filter((result) => result.status === "rejected").length;
				onCreated(project);
				if (binding.failedCount > 0) {
					toast.error(
						binding.installedButUnboundCount > 0
							? `${binding.failedCount} 个技能关联失败，其中 ${binding.installedButUnboundCount} 个已安装到组织`
							: `${binding.failedCount} 个技能关联失败`,
					);
				}
				if (failedMCPCount > 0) {
					toast.error(`${failedMCPCount} 个 MCP 连接器关联失败`);
				}
			}
		} finally {
			setSubmitting(false);
		}
	};

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="sm:max-w-[680px]" showCloseButton={false}>
				<DialogHeader>
					<div className="flex items-center justify-between gap-4">
						<DialogTitle>新建项目</DialogTitle>
						<Button
							type="button"
							variant="ghost"
							size="icon-sm"
							onClick={() => onOpenChange(false)}
							aria-label="关闭"
						>
							<X className="size-4" />
						</Button>
					</div>
					<DialogDescription>创建一个没有任务的空项目，后续可在项目内继续协作。</DialogDescription>
				</DialogHeader>

				<div className="mt-5 space-y-5">
					<FieldWithError error={showNameError ? "请输入项目名称" : undefined}>
						<label className="block" htmlFor="create-project-name">
							<span className="mb-2 block text-sm font-semibold text-[var(--leros-text-strong)]">
								项目名称
								<RequiredMark />
							</span>
							<div className="relative">
								<input
									id="create-project-name"
									value={name}
									onChange={(event) => setName(event.target.value)}
									onBlur={handleFieldBlur("name")}
									placeholder="请输入项目名称"
									maxLength={30}
									autoFocus
									className="h-10 w-full rounded-lg border border-[var(--leros-control-border)] bg-white px-3 pr-14 text-sm text-[var(--leros-text)] placeholder:text-[var(--leros-text-subtle)] transition-colors focus:border-[var(--leros-primary)] focus:outline-none"
								/>
								<span className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-xs text-[var(--leros-text-subtle)]">
									{name.length}/30
								</span>
							</div>
						</label>
					</FieldWithError>

					<label className="block">
						<span className="mb-2 block text-sm font-semibold text-[var(--leros-text-strong)]">
							项目描述
						</span>
						<div className="relative">
							<textarea
								value={description}
								onChange={(event) => setDescription(event.target.value)}
								placeholder="简单描述这个项目的目标、背景或协作范围"
								maxLength={500}
								className="min-h-28 w-full resize-none rounded-lg border border-[var(--leros-control-border)] bg-white px-3 py-2 pb-7 pr-16 text-sm leading-6 text-[var(--leros-text)] placeholder:text-[var(--leros-text-subtle)] transition-colors focus:border-[var(--leros-primary)] focus:outline-none"
							/>
							<span className="pointer-events-none absolute bottom-2 right-3 text-xs text-[var(--leros-text-subtle)]">
								{description.length}/500
							</span>
						</div>
					</label>

					<div className="space-y-3">
						<div className="rounded-lg border border-[var(--leros-control-border)] bg-white px-3 py-2.5">
							<div className="flex items-center justify-between gap-3">
								<div className="flex items-center gap-2">
									<Bot className="size-4 text-[var(--leros-text-muted)]" />
									<div>
										<div className="text-sm font-semibold text-[var(--leros-text-strong)]">
											项目队友{" "}
											<span className="font-normal text-[var(--leros-text-subtle)]">（可选）</span>
										</div>
									</div>
								</div>
								<Button type="button" variant="ghost" size="sm" onClick={() => setMemberOpen(true)}>
									+ 添加
								</Button>
							</div>
							{selectedMembers.length > 0 && (
								<div className={cn("mt-3", projectMemberListClassName)}>
									{selectedMembers.map((member) => (
										<ProjectMemberChip
											key={`${member.type}-${member.publicId ?? member.memberId}`}
											member={member}
											readonly={member.role === "owner"}
											className={projectMemberChipClassName}
											onRemove={() =>
												setSelectedMembers((current) =>
													current.filter(
														(item) =>
															item.type !== member.type || item.memberId !== member.memberId,
													),
												)
											}
										/>
									))}
								</div>
							)}
							<ProjectMemberPickerDialog
								open={memberOpen}
								onOpenChange={setMemberOpen}
								selectedMembers={selectedMembers}
								onConfirm={setSelectedMembers}
							/>
						</div>

						<div className="rounded-lg border border-[var(--leros-control-border)] bg-white px-3 py-2.5">
							<div className="flex items-center justify-between gap-3">
								<div className="flex items-center gap-2">
									<Sparkles className="size-4 text-[var(--leros-text-muted)]" />
									<div className="text-sm font-semibold text-[var(--leros-text-strong)]">
										技能{" "}
										<span className="font-normal text-[var(--leros-text-subtle)]">（可选）</span>
									</div>
								</div>
								<Popover open={skillOpen} onOpenChange={setSkillOpen}>
									<PopoverTrigger
										type="button"
										className="inline-flex h-8 items-center rounded-md px-3 text-sm font-medium text-[var(--leros-text)] transition-colors hover:bg-[var(--leros-surface-soft)]"
									>
										+ 添加
									</PopoverTrigger>
									{/* 固定在按钮上方，避免创建项目弹窗内的技能选择层随空间动态换位。 */}
									<PopoverContent
										align="end"
										side="top"
										sideOffset={10}
										collisionAvoidance={{
											side: "none",
											align: "shift",
											fallbackAxisSide: "none",
										}}
										className="w-[340px] p-1.5"
									>
										<Command shouldFilter={false} className="rounded-xl! bg-transparent p-0">
											<div className="px-2 py-1 text-sm font-semibold text-slate-800">选择技能</div>
											<CommandInput
												value={skillSearch}
												onValueChange={setSkillSearch}
												placeholder="搜索技能"
												className="placeholder:text-slate-300"
											/>
											<CommandSeparator className="mx-1 my-2 bg-slate-200/80" />
											<CommandList className="max-h-64 px-1">
												<CommandEmpty className="py-6 text-slate-400">
													没有可继续添加的技能
												</CommandEmpty>
												<CommandGroup className="p-0">
													{skillsLoading && (
														<div className="px-2 py-1.5 text-xs text-slate-400">技能加载中...</div>
													)}
													{!skillsLoading && skillsError && (
														<div className="px-2 py-1.5 text-xs text-red-400">{skillsError}</div>
													)}
													{filteredSkills.map((skill) => (
														<CommandItem
															key={skill.code}
															value={skill.label}
															onSelect={() => addSkill(skill)}
															className="rounded-lg px-2 py-1.5"
														>
															<div className="flex size-7 shrink-0 items-center justify-center rounded-lg bg-violet-50 text-violet-600">
																<Sparkles className="size-3.5" />
															</div>
															<div className="min-w-0 flex-1">
																<div className="truncate font-medium">
																	{renderHighlightedText(skill.label, skillSearch)}
																</div>
																<div className="truncate text-xs text-slate-400">
																	{renderHighlightedText(skill.description, skillSearch)}
																</div>
															</div>
															<Check className="size-4 opacity-0" />
														</CommandItem>
													))}
												</CommandGroup>
											</CommandList>
										</Command>
									</PopoverContent>
								</Popover>
							</div>
							{selectedSkills.length > 0 && (
								<div className="mt-3 flex flex-wrap gap-1.5">
									{selectedSkills.map((skill) => (
										<button
											key={skill.code}
											type="button"
											onClick={() => removeSkill(skill.code)}
											className="inline-flex items-center gap-1 rounded-full bg-violet-50 px-2 py-1 text-xs text-violet-700 transition-colors hover:bg-violet-100"
										>
											{skill.label}
											<X className="size-3" />
										</button>
									))}
								</div>
							)}
						</div>

						<div className="rounded-lg border border-[var(--leros-control-border)] bg-white px-3 py-2.5">
							<div className="flex items-center justify-between gap-3">
								<div className="flex items-center gap-2">
									<Server className="size-4 text-[var(--leros-text-muted)]" />
									<div className="text-sm font-semibold text-[var(--leros-text-strong)]">
										MCP 连接器{" "}
										<span className="font-normal text-[var(--leros-text-subtle)]">（可选）</span>
									</div>
								</div>
								<Popover open={mcpOpen} onOpenChange={setMCPOpen}>
									<PopoverTrigger
										type="button"
										className="inline-flex h-8 items-center rounded-md px-3 text-sm font-medium hover:bg-[var(--leros-surface-soft)]"
									>
										+ 添加
									</PopoverTrigger>
									<PopoverContent
										align="end"
										side="top"
										sideOffset={10}
										className="w-[340px] p-1.5"
									>
										<Command shouldFilter={false} className="rounded-xl! bg-transparent p-0">
											<div className="px-2 py-1 text-sm font-semibold text-slate-800">
												选择 MCP 连接器
											</div>
											<CommandInput
												value={mcpSearch}
												onValueChange={setMCPSearch}
												placeholder="搜索 MCP 连接器"
											/>
											<CommandSeparator className="mx-1 my-2 bg-slate-200/80" />
											<CommandList className="max-h-64 px-1">
												<CommandEmpty className="py-6 text-slate-400">
													没有可继续添加的 MCP 连接器
												</CommandEmpty>
												<CommandGroup className="p-0">
													{mcpsLoading && (
														<div className="px-2 py-1.5 text-xs text-slate-400">加载中...</div>
													)}
													{filteredMCPs.map((connector) => (
														<CommandItem
															key={connector.public_id}
															value={connector.name}
															onSelect={() => setSelectedMCPs((current) => [...current, connector])}
															className="rounded-lg px-2 py-1.5"
														>
															<MCPConnectorIcon code={connector.code} name={connector.name} />
															<div className="min-w-0 flex-1">
																<div className="truncate font-medium">{connector.name}</div>
																<div className="truncate text-xs text-slate-400">
																	{connector.description || connector.code}
																</div>
															</div>
														</CommandItem>
													))}
												</CommandGroup>
											</CommandList>
										</Command>
									</PopoverContent>
								</Popover>
							</div>
							{selectedMCPs.length > 0 && (
								<div className="mt-3 flex flex-wrap gap-1.5">
									{selectedMCPs.map((connector) => (
										<button
											key={connector.public_id}
											type="button"
											onClick={() =>
												setSelectedMCPs((current) =>
													current.filter((item) => item.public_id !== connector.public_id),
												)
											}
											className="inline-flex items-center gap-1 rounded-full bg-blue-50 px-2 py-1 text-xs text-blue-700 hover:bg-blue-100"
										>
											{connector.name}
											<X className="size-3" />
										</button>
									))}
								</div>
							)}
						</div>
					</div>
				</div>

				<DialogFooter className="mt-6 border-t border-[var(--leros-control-border)] pt-5">
					<Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
						取消
					</Button>
					<Button type="button" onClick={submit} disabled={!nameValid || submitting}>
						确定
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
