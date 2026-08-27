"use client";

import {
	type DigitalAssistantItem,
	type HumanProjectMemberOption,
	isSystemDefaultAssistant,
	type ProjectMember,
	projectMemberApi,
	useDAStore,
} from "@leros/store";
import { Badge } from "@leros/ui/components/ui/badge";
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
import { Select, SelectContent, SelectItem, SelectTrigger } from "@leros/ui/components/ui/select";
import { cn } from "@leros/ui/lib/utils";
import { LoaderCircle, UserRound, X } from "lucide-react";
import { type ReactNode, useEffect, useMemo, useRef, useState } from "react";
import { useAuth } from "../auth";
import { ProtectedImage } from "../avatar/ProtectedImage";
import { renderHighlightedText } from "../common/searchText";
import { AssistantAvatar } from "../digitalAssistant/AssistantAvatar";
import { isAssistantAvailable } from "../digitalAssistant/assistantStatus";

/** 项目成员 chip 列表容器：两列换行排列 */
export const projectMemberListClassName = "flex flex-wrap items-start gap-2";

/** 单个成员 chip 宽度：与添加成员弹窗「已选择」区域一致 */
// 中文注释：窄侧栏中成员卡片改为单列，避免“所有者”等角色标签挤掉姓名；宽弹窗仍可自动双列排列。
export const projectMemberChipClassName = "min-w-[180px] flex-1";

type MemberTab = "assistant" | "human";
type AssistantRefreshState = "idle" | "loading" | "ready" | "error";

type MemberListItem = ProjectMember & {
	disabled?: boolean;
	disabledReason?: string;
	selected?: boolean;
};

type ProjectMemberPickerDialogProps = {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	selectedMembers: ProjectMember[];
	onConfirm: (members: ProjectMember[]) => void;
};

function assistantToProjectMember(assistant: DigitalAssistantItem): ProjectMember {
	return {
		id: `assistant-${assistant.publicId}`,
		memberId: assistant.id,
		publicId: assistant.publicId,
		type: "assistant",
		role: "member",
		name: assistant.name,
		roleName: assistant.roleName,
		description:
			assistant.description ||
			(assistant.expertise.length > 0 ? assistant.expertise.join("、") : "AI 队友"),
		avatarUrl: assistant.avatar,
	};
}

function humanToProjectMember(member: HumanProjectMemberOption): ProjectMember {
	// 中文注释：真人队友副标题只展示手机号或邮箱，避免把无业务价值的 github_login 暴露在成员选择列表里。
	const descriptionParts = [member.phone, member.email].filter(Boolean);

	return {
		id: `user-${member.public_id}`,
		memberId: member.id ?? 0,
		publicId: member.public_id,
		type: "user",
		role: "member",
		name: member.name,
		description: descriptionParts.join(" / "),
		avatarUrl: member.avatar_url,
	};
}

function memberKey(member: Pick<ProjectMember, "type" | "memberId" | "publicId">) {
	return `${member.type}-${member.publicId ?? member.memberId}`;
}

function memberMatchesQuery(member: ProjectMember, query: string) {
	if (!query) return true;
	return [member.name, member.roleName, member.description, member.role]
		.join(" ")
		.toLowerCase()
		.includes(query);
}

function projectMemberSortRank(member: ProjectMember): number {
	if (member.role === "owner") return 0;
	if (member.role === "admin") return 1;
	if (member.type === "user") return 2;
	return 3;
}

export function sortProjectMembers(members: ProjectMember[]): ProjectMember[] {
	return [...members].sort((a, b) => projectMemberSortRank(a) - projectMemberSortRank(b));
}

export function isSameProjectMember(
	a: Pick<ProjectMember, "type" | "memberId" | "publicId">,
	b: Pick<ProjectMember, "type" | "memberId" | "publicId">,
) {
	if (a.type !== b.type) return false;
	if (a.publicId && b.publicId) return a.publicId === b.publicId;
	return a.memberId > 0 && b.memberId > 0 && a.memberId === b.memberId;
}

function isAlreadySelectedMember(selectedMembers: ProjectMember[], candidate: ProjectMember) {
	return selectedMembers.some((selected) => {
		if (isSameProjectMember(selected, candidate)) return true;
		// 中文注释：兼容旧项目详情缺少 public_id 的数据，避免已加入成员继续出现在候选列表。
		return (
			!selected.publicId &&
			selected.type === candidate.type &&
			selected.name.trim() !== "" &&
			selected.name === candidate.name
		);
	});
}

function resolveMemberPublicIdentity(
	member: ProjectMember,
	options: ProjectMember[],
): ProjectMember {
	if (member.publicId) return member;
	const matched = options.find(
		(option) =>
			option.type === member.type && option.memberId > 0 && option.memberId === member.memberId,
	);
	if (!matched?.publicId) return member;

	return {
		...member,
		id: matched.id,
		memberId: matched.memberId || member.memberId,
		// 中文注释：项目队友详情可能只带内部 memberId，提交更新前要回填候选列表里的 public_id。
		publicId: matched.publicId,
		description: member.description || matched.description,
		avatarUrl: member.avatarUrl || matched.avatarUrl,
	};
}

export function ProjectMemberPickerDialog({
	open,
	onOpenChange,
	selectedMembers,
	onConfirm,
}: ProjectMemberPickerDialogProps) {
	const { user } = useAuth();
	const { assistants, fetchAssistants } = useDAStore((s) => s);
	const [activeTab, setActiveTab] = useState<MemberTab>("assistant");
	const [draftMembers, setDraftMembers] = useState<ProjectMember[]>([]);
	const [assistantSearch, setAssistantSearch] = useState("");
	const [humanSearch, setHumanSearch] = useState("");
	const [humanOptions, setHumanOptions] = useState<ProjectMember[]>([]);
	const [humansLoading, setHumansLoading] = useState(false);
	const [humansError, setHumansError] = useState<string | null>(null);
	const [assistantRefreshState, setAssistantRefreshState] = useState<AssistantRefreshState>("idle");
	const wasOpenRef = useRef(false);
	const assistantRefreshCycleRef = useRef(0);

	useEffect(() => {
		const wasOpen = wasOpenRef.current;
		wasOpenRef.current = open;
		if (!open) {
			assistantRefreshCycleRef.current += 1;
			setAssistantRefreshState("idle");
			return;
		}
		if (wasOpen) return;

		// 中文注释：弹窗草稿只在打开瞬间初始化，避免 AI/成员列表异步刷新把用户刚删除的成员重置回来。
		// 中文注释：该弹窗是项目成员的完整编辑入口，打开时回显当前成员，支持后续增删改。
		setDraftMembers(selectedMembers);
		setActiveTab("assistant");
		setAssistantSearch("");
		setHumanSearch("");
		const refreshCycle = ++assistantRefreshCycleRef.current;
		setAssistantRefreshState("loading");
		void fetchAssistants().then((succeeded) => {
			if (refreshCycle !== assistantRefreshCycleRef.current) return;
			setAssistantRefreshState(succeeded ? "ready" : "error");
		});
	}, [fetchAssistants, open, selectedMembers]);

	useEffect(() => {
		if (!open) return;

		const timer = window.setTimeout(() => {
			setHumansLoading(true);
			setHumansError(null);
			projectMemberApi
				.listHumanMembers({ keyword: humanSearch.trim(), limit: 50 })
				.then((items) => {
					setHumanOptions(
						items.map((item) => {
							const option = humanToProjectMember(item);
							const existing = selectedMembers.find((member) =>
								isSameProjectMember(member, option),
							);
							// 中文注释：候选成员也要复用项目详情中的真实角色，避免管理员重新显示为成员。
							return existing ? { ...option, role: existing.role } : option;
						}),
					);
				})
				.catch((error: unknown) => {
					const message = error instanceof Error ? error.message : "成员加载失败";
					setHumansError(message);
					setHumanOptions([]);
				})
				.finally(() => {
					setHumansLoading(false);
				});
		}, 200);

		return () => window.clearTimeout(timer);
	}, [humanSearch, open, selectedMembers]);

	const assistantOptions = useMemo(
		() => assistants.filter(isAssistantAvailable).map(assistantToProjectMember),
		[assistants],
	);
	const identityOptions = useMemo(
		() => [...assistantOptions, ...humanOptions],
		[assistantOptions, humanOptions],
	);
	useEffect(() => {
		if (!open || identityOptions.length === 0) return;
		setDraftMembers((current) =>
			current.map((member) => resolveMemberPublicIdentity(member, identityOptions)),
		);
	}, [identityOptions, open]);
	const selectedKeys = useMemo(
		() => new Set(draftMembers.map((member) => memberKey(member))),
		[draftMembers],
	);
	const visibleDraftMembers = useMemo(
		() =>
			sortProjectMembers(
				draftMembers.filter(
					// 中文注释：默认 AI 员工是系统兜底成员，不在添加成员弹窗的已选择列表里展示。
					(member) =>
						!(
							member.type === "assistant" &&
							(member.isDefault || isSystemDefaultAssistant(member.publicId))
						),
				),
			),
		[draftMembers],
	);
	const filteredAssistants = useMemo(() => {
		const query = assistantSearch.trim().toLowerCase();
		return assistantOptions.filter(
			(member) =>
				!selectedKeys.has(memberKey(member)) &&
				!isAlreadySelectedMember(draftMembers, member) &&
				memberMatchesQuery(member, query),
		);
	}, [assistantOptions, assistantSearch, draftMembers, selectedKeys]);
	const assistantListLoading = open && (assistantRefreshState === "loading" || !wasOpenRef.current);
	const assistantListError =
		assistantRefreshState === "error" ? "AI 队友加载失败，请关闭后重试" : null;
	const filteredHumans = useMemo((): MemberListItem[] => {
		const query = humanSearch.trim().toLowerCase();
		return humanOptions.filter(
			(member) =>
				member.publicId !== user?.publicId &&
				!selectedKeys.has(memberKey(member)) &&
				!isAlreadySelectedMember(draftMembers, member) &&
				memberMatchesQuery(member, query),
		);
	}, [draftMembers, humanOptions, humanSearch, selectedKeys, user?.publicId]);

	const addMember = (member: ProjectMember, roleOverride?: string) => {
		const nextMember = roleOverride ? { ...member, role: roleOverride } : member;
		setDraftMembers((current) => {
			if (
				current.some(
					(item) =>
						memberKey(item) === memberKey(member) || isAlreadySelectedMember([item], member),
				)
			) {
				return current;
			}
			return [...current, nextMember];
		});
	};

	const removeMember = (member: ProjectMember) => {
		if (isProtectedMember(member)) {
			return;
		}
		setDraftMembers((current) => current.filter((item) => memberKey(item) !== memberKey(member)));
	};

	const setDraftMemberRole = (member: ProjectMember, role: string) => {
		setDraftMembers((current) =>
			current.map((item) => (isSameProjectMember(item, member) ? { ...item, role } : item)),
		);
	};

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent
				className="flex max-h-[calc(100vh-48px)] flex-col overflow-hidden sm:max-w-[820px]"
				showCloseButton={false}
			>
				<DialogHeader className="shrink-0">
					<div className="flex items-center justify-between gap-4">
						<DialogTitle>添加项目队友</DialogTitle>
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
					<DialogDescription>
						选择 AI 队友或真人队友加入项目，点击成员信息即可添加。
					</DialogDescription>
				</DialogHeader>

				<div className="mt-4 grid h-[420px] max-h-[calc(100vh-220px)] min-h-0 gap-4 overflow-hidden sm:grid-cols-[minmax(0,1fr)_340px]">
					<div className="flex min-h-0 flex-col">
						<div className="grid h-9 shrink-0 grid-cols-2 rounded-lg bg-[var(--leros-surface-soft)] p-1">
							<MemberTabButton
								active={activeTab === "assistant"}
								onClick={() => setActiveTab("assistant")}
							>
								AI 队友
							</MemberTabButton>
							<MemberTabButton active={activeTab === "human"} onClick={() => setActiveTab("human")}>
								真人队友
							</MemberTabButton>
						</div>
						<div className="mt-3 min-h-0 flex-1">
							{activeTab === "assistant" ? (
								<MemberCommandList
									search={assistantSearch}
									onSearchChange={setAssistantSearch}
									placeholder="搜索 AI 队友"
									emptyText="没有可添加的 AI 队友"
									members={filteredAssistants}
									onSelect={addMember}
									loading={assistantListLoading}
									error={assistantListError}
								/>
							) : (
								<MemberCommandList
									search={humanSearch}
									onSearchChange={setHumanSearch}
									placeholder="搜索成员名称"
									emptyText="没有可添加的真人队友"
									members={filteredHumans}
									onSelect={addMember}
									loading={humansLoading}
									error={humansError}
								/>
							)}
						</div>
					</div>

					<div className="flex min-h-0 flex-col rounded-xl border border-[var(--leros-control-border)] bg-white p-3">
						<div className="mb-2 text-xs font-medium text-[var(--leros-text-muted)]">
							已选择 {visibleDraftMembers.length} 位
						</div>
						{visibleDraftMembers.length === 0 ? (
							<div className="flex flex-1 items-center justify-center rounded-lg border border-dashed border-[var(--leros-control-border)] px-3 py-6 text-center text-xs text-[var(--leros-text-subtle)]">
								暂未选择成员
							</div>
						) : (
							<div className="min-h-0 flex-1 overflow-y-auto pr-1 no-scrollbar">
								<div className={projectMemberListClassName}>
									{visibleDraftMembers.map((member) => (
										<ProjectMemberChip
											key={memberKey(member)}
											member={member}
											readonly={isProtectedMember(member)}
											onRemove={() => removeMember(member)}
											onRoleChange={
												member.type === "user" && member.role !== "owner"
													? (role) => setDraftMemberRole(member, role)
													: undefined
											}
											className="w-full"
										/>
									))}
								</div>
							</div>
						)}
					</div>
				</div>

				<DialogFooter className="mt-5 shrink-0 border-t border-[var(--leros-control-border)] pt-4">
					<Button variant="outline" onClick={() => onOpenChange(false)}>
						取消
					</Button>
					<Button
						onClick={() => {
							const nextMembers = draftMembers.map((member) =>
								resolveMemberPublicIdentity(member, identityOptions),
							);
							onConfirm(nextMembers);
							onOpenChange(false);
						}}
					>
						确认添加
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

function MemberTabButton({
	active,
	onClick,
	children,
}: {
	active: boolean;
	onClick: () => void;
	children: ReactNode;
}) {
	return (
		<button
			type="button"
			className={cn(
				"rounded-md px-3 text-sm font-medium transition-colors",
				active
					? "bg-white text-[var(--leros-text-strong)] shadow-sm"
					: "text-[var(--leros-text-muted)] hover:text-[var(--leros-text-strong)]",
			)}
			onClick={onClick}
		>
			{children}
		</button>
	);
}

const PROJECT_ROLE_OPTIONS: { value: string; label: string }[] = [
	{ value: "member", label: "成员" },
	{ value: "admin", label: "管理员" },
];

function projectRoleOptionLabel(role: string | undefined): string {
	return PROJECT_ROLE_OPTIONS.find((option) => option.value === role)?.label ?? "成员";
}

export function MemberRoleSelect({
	memberName,
	role,
	onRoleChange,
	disabled = false,
}: {
	memberName: string;
	role: string;
	onRoleChange: (role: string) => void;
	disabled?: boolean;
}) {
	const pendingRoleRef = useRef<string | null>(null);
	const [displayRole, setDisplayRole] = useState(role);

	useEffect(() => {
		setDisplayRole(role);
	}, [role]);

	return (
		<Select
			value={displayRole}
			disabled={disabled}
			onValueChange={(value) => {
				// 中文注释：Base UI Select 的 value 可能为 null，这里统一回落为 member。
				const nextRole = value ?? "member";
				pendingRoleRef.current = nextRole;
				setDisplayRole(nextRole);
			}}
			onOpenChangeComplete={(open) => {
				if (open) return;
				const nextRole = pendingRoleRef.current;
				pendingRoleRef.current = null;
				if (!nextRole || nextRole === role) return;
				// 中文注释：等下拉关闭动画结束后再提交角色，避免卡片重排带着弹层一起跳。
				onRoleChange(nextRole);
			}}
		>
			<SelectTrigger
				size="sm"
				aria-label={`设置 ${memberName} 的项目角色`}
				className="h-8 min-w-[88px] shrink-0 rounded-lg border border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] px-2.5 text-sm font-medium text-[var(--leros-text)] shadow-none transition-colors hover:border-[var(--leros-control-border)] focus-visible:border-[var(--leros-primary)] focus-visible:ring-[3px] focus-visible:ring-[var(--leros-primary)]/12"
			>
				<span className="min-w-0 truncate text-left">{projectRoleOptionLabel(displayRole)}</span>
			</SelectTrigger>
			<SelectContent
				align="end"
				side="bottom"
				sideOffset={4}
				alignItemWithTrigger={false}
				className="min-w-[88px] rounded-lg border border-[var(--leros-control-border)] bg-[var(--leros-surface)] p-1 shadow-md"
			>
				{PROJECT_ROLE_OPTIONS.map((option) => (
					<SelectItem
						key={option.value}
						value={option.value}
						className="rounded-md px-2.5 py-1.5 text-sm font-medium text-[var(--leros-text)]"
					>
						{option.label}
					</SelectItem>
				))}
			</SelectContent>
		</Select>
	);
}

function MemberCommandList({
	search,
	onSearchChange,
	placeholder,
	emptyText,
	members,
	onSelect,
	loading,
	error,
}: {
	search: string;
	onSearchChange: (value: string) => void;
	placeholder: string;
	emptyText: string;
	members: MemberListItem[];
	onSelect: (member: MemberListItem) => void;
	loading?: boolean;
	error?: string | null;
}) {
	const listWrapperRef = useRef<HTMLDivElement>(null);

	const preserveListScroll = (select: () => void) => {
		const list = listWrapperRef.current?.querySelector<HTMLElement>('[data-slot="command-list"]');
		const scrollTop = list?.scrollTop ?? 0;
		select();

		// 中文注释：选中后候选会从列表移除，cmdk 可能自动滚到顶部；恢复原位置便于连续添加底部成员。
		window.requestAnimationFrame(() => {
			if (!list) return;
			list.scrollTop = scrollTop;
			window.requestAnimationFrame(() => {
				list.scrollTop = scrollTop;
			});
		});
	};
	return (
		<div ref={listWrapperRef} className="flex h-full min-h-0 flex-col">
			<Command
				shouldFilter={false}
				className="flex h-full min-h-0 flex-col rounded-none bg-transparent p-0"
			>
				<CommandInput value={search} onValueChange={onSearchChange} placeholder={placeholder} />
				<CommandList className="max-h-none min-h-0 flex-1">
					{!loading && !error && (
						<CommandEmpty className="py-6 text-slate-400">{emptyText}</CommandEmpty>
					)}
					<CommandGroup className={cn("p-1", loading && "flex h-full items-center justify-center")}>
						{loading && (
							<div className="flex items-center gap-2 px-3 py-2 text-xs text-slate-400">
								<LoaderCircle className="size-3.5 animate-spin" />
								加载中...
							</div>
						)}
						{!loading && error && <div className="px-3 py-2 text-xs text-red-400">{error}</div>}
						{!loading &&
							!error &&
							members.map((member) => {
								const key = memberKey(member);
								return (
									<CommandItem
										key={key}
										value={key}
										disabled={member.disabled}
										onSelect={() => {
											if (member.disabled) return;
											preserveListScroll(() => onSelect(member));
										}}
										className={cn(
											"group cursor-pointer items-center gap-3 rounded-lg border border-transparent px-2.5 py-2 transition-all hover:border-[var(--leros-primary)]/25 hover:bg-[var(--leros-primary-softer)] hover:shadow-sm active:scale-[0.99] data-selected:bg-[var(--leros-surface-soft)]",
											member.disabled && "cursor-not-allowed opacity-50",
										)}
										title={member.disabled ? undefined : "点击添加"}
									>
										<MemberAvatar member={member} />
										<div className="min-w-0 flex-1">
											<div className="truncate font-medium text-slate-700">
												{renderHighlightedText(member.name, search)}
											</div>
											{/* 中文注释：AI 队友候选固定两行，名称在上、角色名称在下。 */}
											{member.type === "assistant" && member.roleName ? (
												<div className="truncate text-xs text-slate-500">
													{renderHighlightedText(member.roleName, search)}
												</div>
											) : member.type !== "assistant" ? (
												<div className="truncate text-xs text-slate-400">
													{renderHighlightedText(member.description ?? "", search)}
												</div>
											) : null}
										</div>
										{member.disabledReason && (
											<Badge
												variant="outline"
												className="h-5 shrink-0 px-1.5 py-0 text-[10px] font-normal text-slate-400"
											>
												{member.disabledReason}
											</Badge>
										)}
									</CommandItem>
								);
							})}
					</CommandGroup>
				</CommandList>
			</Command>
		</div>
	);
}

function isProtectedMember(member: ProjectMember) {
	return member.role === "owner" || member.isDefault === true;
}

function formatProjectMemberRoleLabel(role: string | undefined): string | null {
	switch (role) {
		case "owner":
			return "所有者";
		case "admin":
			return "管理员";
		case "member":
		case "":
		case undefined:
			return "成员";
		default:
			return null;
	}
}

function formatProjectMemberSubtitle(member: ProjectMember): string {
	if (member.type === "user") {
		const description = member.description?.trim();
		if (description) return description;
		return "";
	}
	if (member.isDefault) {
		return "默认 AI 队友";
	}
	if (member.type === "assistant") {
		return member.roleName?.trim() || "AI 队友";
	}
	return "成员";
}

export function ProjectMemberChip({
	member,
	onRemove,
	readonly = false,
	canRemove = true,
	className,
	onRoleChange,
}: {
	member: ProjectMember;
	onRemove?: () => void;
	readonly?: boolean;
	canRemove?: boolean;
	className?: string;
	onRoleChange?: (role: string) => void;
}) {
	const canEditRole = Boolean(onRoleChange) && member.type === "user" && member.role !== "owner";
	const userRoleLabel =
		member.type === "user" && !canEditRole ? formatProjectMemberRoleLabel(member.role) : null;
	const subtitle = formatProjectMemberSubtitle(member);

	return (
		<div
			className={cn(
				"group flex min-h-[52px] min-w-0 items-center gap-2 rounded-lg border border-[var(--leros-control-border)] bg-[var(--leros-surface)] py-1.5 pl-1.5 pr-2",
				className,
			)}
		>
			<MemberAvatar member={member} />
			<div className="min-w-0 flex-1">
				<div className="flex min-w-0 items-center gap-1.5">
					<div className="truncate text-xs font-medium text-[var(--leros-text)]">{member.name}</div>
					{userRoleLabel && (
						<Badge
							variant="outline"
							className="h-4 shrink-0 px-1.5 py-0 text-[10px] font-normal text-[var(--leros-text-subtle)]"
						>
							{userRoleLabel}
						</Badge>
					)}
				</div>
				{subtitle ? (
					<div className="truncate text-[10px] leading-4 text-[var(--leros-text-subtle)]">
						{subtitle}
					</div>
				) : null}
			</div>
			{canEditRole && onRoleChange && (
				<MemberRoleSelect
					memberName={member.name}
					role={member.role || "member"}
					onRoleChange={onRoleChange}
				/>
			)}
			<div className="flex size-4 shrink-0 items-center justify-center">
				{!readonly && canRemove && onRemove && (
					<button
						type="button"
						className="rounded-full p-0.5 text-[var(--leros-text-subtle)] opacity-0 transition-opacity hover:bg-[var(--leros-control-border)] hover:text-[var(--leros-text)] group-hover:opacity-100"
						aria-label={`移除成员 ${member.name}`}
						onClick={onRemove}
					>
						<X className="size-3" />
					</button>
				)}
			</div>
		</div>
	);
}

function MemberAvatar({ member }: { member: ProjectMember }) {
	if (member.type === "assistant") {
		return <AssistantAvatar name={member.name} src={member.avatarUrl} size="sm" />;
	}

	const fallback = (
		<div className="flex size-7 shrink-0 items-center justify-center rounded-lg bg-emerald-50 text-emerald-600">
			<UserRound className="size-3.5" />
		</div>
	);

	if (member.avatarUrl) {
		return (
			<ProtectedImage
				src={member.avatarUrl}
				alt={member.name}
				className="size-7 shrink-0 rounded-lg object-cover"
				fallback={fallback}
			/>
		);
	}

	return fallback;
}
