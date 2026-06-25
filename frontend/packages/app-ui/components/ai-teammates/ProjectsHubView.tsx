"use client";

import {
	type Project,
	type SkillInstalledItem,
	skillMarketplaceApi,
	useLayoutStore,
} from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import {
	Command,
	CommandEmpty,
	CommandGroup,
	CommandInput,
	CommandItem,
	CommandList,
} from "@leros/ui/components/ui/command";
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
import { Popover, PopoverContent, PopoverTrigger } from "@leros/ui/components/ui/popover";
import { cn } from "@leros/ui/lib/utils";
import {
	Bot,
	CalendarDays,
	Check,
	FolderKanban,
	MoreHorizontal,
	Pencil,
	Plus,
	Search,
	Sparkles,
	Trash2,
	X,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { useAuth } from "../auth";
import type { AppNavigation } from "../layout/LeftRail";
import { notifyFeatureUnavailable } from "./feature-unavailable";

type ProjectsHubViewProps = {
	navigation?: AppNavigation;
};

type SkillOption = {
	code: string;
	label: string;
	description: string;
	keywords: string[];
};

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null;
}

function stringFromValue(value: unknown): string {
	return typeof value === "string" ? value : "";
}

function skillItemFromValue(value: unknown): SkillInstalledItem | null {
	if (!isRecord(value)) return null;

	const name = stringFromValue(value.name || value.skill_id || value.id);
	if (!name) return null;

	return {
		name,
		description: stringFromValue(value.description),
		category: stringFromValue(value.category),
		source: stringFromValue(value.source || value.source_type),
		trust: stringFromValue(value.trust),
	};
}

function normalizeInstalledSkillsPayload(value: unknown): SkillInstalledItem[] {
	const toItems = (items: unknown[]) =>
		items.map(skillItemFromValue).filter((item): item is SkillInstalledItem => item !== null);

	if (Array.isArray(value)) return toItems(value);
	if (!isRecord(value)) return [];

	const nestedData = value.data;
	if (isRecord(nestedData)) {
		if (Array.isArray(nestedData.skills)) return toItems(nestedData.skills);
		if (Array.isArray(nestedData.items)) return toItems(nestedData.items);
	}

	if (Array.isArray(value.skills)) return toItems(value.skills);
	if (Array.isArray(value.items)) return toItems(value.items);
	return [];
}

function installedSkillToOption(skill: SkillInstalledItem): SkillOption {
	return {
		code: skill.name,
		label: skill.name,
		description: skill.description || skill.category || "已安装技能",
		keywords: [skill.name, skill.description, skill.category, skill.source, skill.trust].filter(
			Boolean,
		),
	};
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
	const { projects, fetchProjects, createProject, updateProject, deleteProject, setProjectRoute } =
		useLayoutStore((s) => s);
	const { isAuthenticated, requireAuth } = useAuth();
	const [keyword, setKeyword] = useState("");
	const [createOpen, setCreateOpen] = useState(false);
	const [renameProject, setRenameProject] = useState<Project | null>(null);
	const [renameValue, setRenameValue] = useState("");
	const [deleteTarget, setDeleteTarget] = useState<Project | null>(null);

	useEffect(() => {
		fetchProjects();
	}, [fetchProjects]);

	const filteredProjects = useMemo(() => {
		const query = keyword.trim().toLowerCase();
		const sorted = [...projects].sort((a, b) => b.createdAt - a.createdAt);
		if (!query) return sorted;

		return sorted.filter((project) =>
			[project.name, project.description].join(" ").toLowerCase().includes(query),
		);
	}, [keyword, projects]);

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

		const updatedProject = await updateProject({ public_id: renameProject.id, name });
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

	return (
		<div
			data-slot="projects-hub-view"
			className="flex h-full min-h-0 flex-1 flex-col bg-[var(--leros-app-bg)]"
		>
			<header className="shrink-0 border-b border-[var(--leros-control-border)] px-6 py-5">
				<div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
					<div>
						<h1 className="text-xl font-bold text-[var(--leros-text-strong)]">项目</h1>
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
								<FolderKanban className="size-6" />
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
					<div className="mt-4">
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
							autoFocus
							className="w-full rounded-md border border-slate-200 bg-white px-3 py-2 text-sm text-slate-800 placeholder:text-slate-400 transition-colors focus:border-blue-300 focus:outline-none"
						/>
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
		</div>
	);
}

function ProjectCard({
	project,
	onOpen,
	onRename,
	onDelete,
}: {
	project: Project;
	onOpen: (projectId: string) => void;
	onRename: (project: Project) => void;
	onDelete: (project: Project) => void;
}) {
	return (
		<button
			type="button"
			className={cn(
				"group relative flex min-h-[132px] w-full cursor-pointer flex-col rounded-lg border border-slate-200 bg-white p-4 text-left transition-colors",
				"hover:border-blue-200 hover:bg-blue-50/30",
			)}
			onClick={() => onOpen(project.id)}
		>
			<div className="mb-3 flex items-start gap-3 pr-7">
				<div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-[var(--leros-surface-soft)] text-[var(--leros-text-muted)] transition-colors group-hover:bg-[var(--leros-primary-soft)] group-hover:text-[var(--leros-primary)]">
					<FolderKanban className="size-5" />
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

			<div className="mt-auto flex items-center gap-1.5 text-xs text-[var(--leros-text-subtle)]">
				<CalendarDays className="size-3.5" />
				<span>创建于 {formatProjectDate(project.createdAt)}</span>
			</div>

			<DropdownMenu>
				<DropdownMenuTrigger
					render={
						<Button
							variant="ghost"
							size="icon-xs"
							className="absolute right-3 top-3 opacity-0 transition-opacity group-hover:opacity-100"
							onClick={(event: React.MouseEvent) => event.stopPropagation()}
							aria-label={`管理项目 ${project.name}`}
						>
							<MoreHorizontal className="size-3.5" />
						</Button>
					}
				/>
				<DropdownMenuContent align="end" sideOffset={4}>
					<DropdownMenuItem
						onClick={(event: React.MouseEvent) => {
							event.stopPropagation();
							onRename(project);
						}}
					>
						<Pencil className="mr-2 size-3.5" />
						重命名
					</DropdownMenuItem>
					<DropdownMenuItem
						variant="destructive"
						onClick={(event: React.MouseEvent) => {
							event.stopPropagation();
							onDelete(project);
						}}
					>
						<Trash2 className="mr-2 size-3.5" />
						删除
					</DropdownMenuItem>
				</DropdownMenuContent>
			</DropdownMenu>
		</button>
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
		metadata?: Record<string, unknown>;
	}) => Promise<Project | null>;
	onCreated: (project: Project) => void;
}) {
	const [name, setName] = useState("");
	const [description, setDescription] = useState("");
	const [selectedSkills, setSelectedSkills] = useState<SkillOption[]>([]);
	const [skillOpen, setSkillOpen] = useState(false);
	const [skillSearch, setSkillSearch] = useState("");
	const [skillOptions, setSkillOptions] = useState<SkillOption[]>([]);
	const [skillsLoading, setSkillsLoading] = useState(false);
	const [skillsLoaded, setSkillsLoaded] = useState(false);
	const [skillsError, setSkillsError] = useState<string | null>(null);
	const [submitting, setSubmitting] = useState(false);

	useEffect(() => {
		if (!open) {
			setName("");
			setDescription("");
			setSelectedSkills([]);
			setSkillSearch("");
			setSubmitting(false);
		}
	}, [open]);

	useEffect(() => {
		if (!skillOpen || skillsLoaded) return;

		setSkillsLoading(true);
		setSkillsError(null);
		skillMarketplaceApi
			.installed()
			.then((response) => {
				const raw = normalizeInstalledSkillsPayload(response.data);
				setSkillOptions(raw.map(installedSkillToOption));
				setSkillsLoaded(true);
			})
			.catch((error: unknown) => {
				const message = error instanceof Error ? error.message : "技能加载失败";
				setSkillsError(message);
				setSkillOptions([]);
			})
			.finally(() => {
				setSkillsLoading(false);
			});
	}, [skillOpen, skillsLoaded]);

	const selectedSkillCodes = useMemo(
		() => selectedSkills.map((skill) => skill.code),
		[selectedSkills],
	);
	const filteredSkills = useMemo(() => {
		const query = skillSearch.trim().toLowerCase();
		return skillOptions.filter((skill) => {
			if (selectedSkillCodes.includes(skill.code)) return false;
			if (!query) return true;
			return [skill.label, skill.code, skill.description, ...skill.keywords]
				.join(" ")
				.toLowerCase()
				.includes(query);
		});
	}, [selectedSkillCodes, skillOptions, skillSearch]);

	const addSkill = (skill: SkillOption) => {
		setSelectedSkills((current) => {
			if (current.some((item) => item.code === skill.code)) return current;
			return [...current, skill];
		});
	};

	const removeSkill = (skillCode: string) => {
		setSelectedSkills((current) => current.filter((skill) => skill.code !== skillCode));
	};

	const submit = async () => {
		const trimmedName = name.trim();
		if (!trimmedName || submitting) return;

		setSubmitting(true);
		try {
			const metadata =
				selectedSkills.length > 0
					? {
							extra: {
								skills: selectedSkills.map((skill) => ({
									code: skill.code,
									name: skill.label,
									description: skill.description,
								})),
							},
						}
					: undefined;
			// 中文注释：新建项目只调用项目接口，不创建任务；可选技能仅作为项目元数据保存。
			const project = await onCreate({
				name: trimmedName,
				description: description.trim(),
				metadata,
			});
			if (project) {
				onCreated(project);
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
					<label className="block">
						<span className="mb-2 block text-sm font-semibold text-[var(--leros-text-strong)]">
							项目名称
						</span>
						<input
							value={name}
							onChange={(event) => setName(event.target.value)}
							placeholder="请输入项目名称"
							autoFocus
							className="h-10 w-full rounded-lg border border-[var(--leros-control-border)] bg-white px-3 text-sm text-[var(--leros-text)] placeholder:text-[var(--leros-text-subtle)] transition-colors focus:border-[var(--leros-primary)] focus:outline-none"
						/>
					</label>

					<label className="block">
						<span className="mb-2 block text-sm font-semibold text-[var(--leros-text-strong)]">
							项目描述
						</span>
						<textarea
							value={description}
							onChange={(event) => setDescription(event.target.value)}
							placeholder="简单描述这个项目的目标、背景或协作范围"
							className="min-h-28 w-full resize-none rounded-lg border border-[var(--leros-control-border)] bg-white px-3 py-2 text-sm leading-6 text-[var(--leros-text)] placeholder:text-[var(--leros-text-subtle)] transition-colors focus:border-[var(--leros-primary)] focus:outline-none"
						/>
					</label>

					<div className="space-y-3">
						<div className="flex items-center justify-between rounded-lg border border-[var(--leros-control-border)] bg-white px-3 py-2.5">
							<div className="flex items-center gap-2">
								<Bot className="size-4 text-[var(--leros-text-muted)]" />
								<div>
									<div className="text-sm font-semibold text-[var(--leros-text-strong)]">
										AI队友{" "}
										<span className="font-normal text-[var(--leros-text-subtle)]">（可选）</span>
									</div>
								</div>
							</div>
							<Button type="button" variant="ghost" size="sm" onClick={notifyFeatureUnavailable}>
								+ 添加
							</Button>
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
									<PopoverContent align="end" sideOffset={10} className="w-[340px] p-1.5">
										<Command shouldFilter={false} className="rounded-xl! bg-transparent p-0">
											<div className="px-2 py-1 text-xs font-medium text-slate-400">选择技能</div>
											<CommandInput
												value={skillSearch}
												onValueChange={setSkillSearch}
												placeholder="搜索技能"
											/>
											{selectedSkills.length > 0 && (
												<div className="px-2 pb-2 pt-1">
													<div className="mb-1 text-[11px] font-medium text-slate-400">
														已选技能
													</div>
													<div className="flex flex-wrap gap-1.5">
														{selectedSkills.map((skill) => (
															<button
																key={skill.code}
																type="button"
																onClick={() => removeSkill(skill.code)}
																className="inline-flex items-center gap-1 rounded-full bg-violet-50 px-2 py-1 text-[11px] text-violet-700 transition-colors hover:bg-violet-100"
															>
																/{skill.label}
																<X className="size-3" />
															</button>
														))}
													</div>
												</div>
											)}
											<CommandList className="max-h-64">
												<CommandEmpty className="py-6 text-slate-400">
													没有可继续添加的技能
												</CommandEmpty>
												<CommandGroup className="p-0">
													{skillsLoading && (
														<div className="px-3 py-2 text-xs text-slate-400">技能加载中...</div>
													)}
													{!skillsLoading && skillsError && (
														<div className="px-3 py-2 text-xs text-red-400">{skillsError}</div>
													)}
													{filteredSkills.map((skill) => (
														<CommandItem
															key={skill.code}
															value={skill.label}
															onSelect={() => addSkill(skill)}
															className="rounded-xl px-2.5 py-2"
														>
															<div className="flex size-7 shrink-0 items-center justify-center rounded-lg bg-violet-50 text-violet-600">
																<Sparkles className="size-3.5" />
															</div>
															<div className="min-w-0 flex-1">
																<div className="truncate font-medium">/{skill.label}</div>
																<div className="truncate text-xs text-slate-400">
																	{skill.description}
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
											/{skill.label}
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
					<Button type="button" onClick={submit} disabled={!name.trim() || submitting}>
						确定
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
