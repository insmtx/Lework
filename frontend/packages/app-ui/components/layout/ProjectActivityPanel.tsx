"use client";

import {
	type ProjectActivityActor,
	type ProjectActivityItem,
	type ProjectMember,
	projectActivityApi,
} from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@leros/ui/components/ui/popover";
import { cn } from "@leros/ui/lib/utils";
import { Check, ChevronDown, LoaderCircle, Search, UserRound } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { ProtectedImage } from "../avatar/ProtectedImage";
import {
	buildProjectActivityActionParts,
	formatProjectActivitySummary,
	formatProjectActivityTime,
	type ProjectActivityTextPart,
	resolveProjectActivityOperatorAvatar,
	resolveProjectActivityOperatorName,
} from "./project-activity";

type ProjectActivityPanelProps = {
	projectId: string;
	humanMembers: ProjectMember[];
	refreshKey?: number;
};

type MemberFilterState = {
	mode: "all" | "partial";
	selectedIds: Set<string>;
};

function humanMemberPublicId(member: ProjectMember): string | null {
	return member.publicId?.trim() || null;
}

function buildDefaultFilter(humanMembers: ProjectMember[]): MemberFilterState {
	const selectedIds = new Set<string>();
	for (const member of humanMembers) {
		const publicId = humanMemberPublicId(member);
		if (publicId) selectedIds.add(publicId);
	}
	return { mode: "all", selectedIds };
}

function getFilterTriggerLabel(filter: MemberFilterState, totalCount: number): string {
	if (filter.mode === "all" || filter.selectedIds.size === totalCount) {
		return "全部成员";
	}
	return `${filter.selectedIds.size}个成员`;
}

function buildActivityListParams(
	projectId: string,
	filter: MemberFilterState,
	cursor?: string,
): {
	project_id: string;
	limit: number;
	cursor?: string;
	operator_ids?: string[];
} {
	const params: {
		project_id: string;
		limit: number;
		cursor?: string;
		operator_ids?: string[];
	} = {
		project_id: projectId,
		limit: 20,
		cursor,
	};

	if (filter.mode === "partial" && filter.selectedIds.size > 0) {
		params.operator_ids = [...filter.selectedIds].sort();
	}

	return params;
}

function ActivityOperatorAvatar({ name, avatarUrl }: { name: string; avatarUrl?: string }) {
	const fallback = (
		<span className="inline-flex size-7 shrink-0 items-center justify-center rounded-full bg-emerald-50 text-emerald-600">
			<UserRound className="size-3.5" />
		</span>
	);

	if (avatarUrl) {
		return (
			<span className="inline-flex size-7 shrink-0 items-center justify-center">
				<ProtectedImage
					src={avatarUrl}
					alt={name}
					className="size-7 shrink-0 rounded-full object-cover"
					fallback={fallback}
				/>
			</span>
		);
	}

	return fallback;
}

function ProjectActivityActorListPart({
	label,
	actors,
}: {
	label: "成员" | "AI队友";
	actors: ProjectActivityActor[];
}) {
	if (actors.length === 0) {
		return <span>{label}</span>;
	}

	return (
		<span className="inline-flex items-center gap-1">
			<span>{label}</span>
			{actors.map((actor, index) => {
				const displayName = actor.name?.trim() || label;
				return (
					<span
						key={actor.id || `${displayName}-${index}`}
						className="inline-flex items-center gap-1"
					>
						{index > 0 ? <span>，</span> : null}
						<ActivityOperatorAvatar name={displayName} avatarUrl={actor.avatar_url} />
						<span className="font-semibold">{displayName}</span>
					</span>
				);
			})}
		</span>
	);
}

function renderProjectActivityActionPart(part: ProjectActivityTextPart, index: number) {
	if (part.type === "actor-list") {
		return (
			<ProjectActivityActorListPart
				key={`actor-list-${part.label}-${index}`}
				label={part.label}
				actors={part.actors}
			/>
		);
	}

	if (part.bold) {
		return (
			<span key={`text-${index}`} className="font-semibold">
				{part.text}
			</span>
		);
	}

	return <span key={`text-${index}`}>{part.text}</span>;
}

function ProjectActivityMemberFilter({
	humanMembers,
	filter,
	onConfirm,
}: {
	humanMembers: ProjectMember[];
	filter: MemberFilterState;
	onConfirm: (nextFilter: MemberFilterState) => void;
}) {
	const [open, setOpen] = useState(false);
	const [searchKeyword, setSearchKeyword] = useState("");
	const [draftSelectedIds, setDraftSelectedIds] = useState<Set<string>>(new Set());

	const selectableMembers = useMemo(
		() =>
			humanMembers
				.map((member) => {
					const publicId = humanMemberPublicId(member);
					if (!publicId) return null;
					return { member, publicId };
				})
				.filter((item): item is { member: ProjectMember; publicId: string } => item !== null),
		[humanMembers],
	);

	const filteredMembers = useMemo(() => {
		const keyword = searchKeyword.trim().toLowerCase();
		if (!keyword) return selectableMembers;
		return selectableMembers.filter(({ member }) =>
			[member.name, member.description].join(" ").toLowerCase().includes(keyword),
		);
	}, [searchKeyword, selectableMembers]);

	const totalCount = selectableMembers.length;
	const draftCount = draftSelectedIds.size;
	const allDraftSelected = totalCount > 0 && draftCount === totalCount;
	const partialDraftSelected = draftCount > 0 && draftCount < totalCount;
	const confirmDisabled = draftCount === 0;
	const memberFilterControlSelectedClass =
		"border-[var(--leros-primary)] bg-[var(--leros-primary)] text-white";
	const memberFilterControlUnselectedClass = "border-[var(--leros-control-border)] bg-white";

	useEffect(() => {
		if (!open) return;
		setSearchKeyword("");
		setDraftSelectedIds(new Set(filter.selectedIds));
	}, [open, filter.selectedIds]);

	const toggleMember = (publicId: string) => {
		setDraftSelectedIds((current) => {
			const next = new Set(current);
			if (next.has(publicId)) {
				next.delete(publicId);
			} else {
				next.add(publicId);
			}
			return next;
		});
	};

	const toggleSelectAll = () => {
		if (allDraftSelected) {
			setDraftSelectedIds(new Set());
			return;
		}
		setDraftSelectedIds(new Set(selectableMembers.map((item) => item.publicId)));
	};

	const handleConfirm = () => {
		if (confirmDisabled) return;
		const nextFilter: MemberFilterState = {
			mode: allDraftSelected ? "all" : "partial",
			selectedIds: new Set(draftSelectedIds),
		};
		onConfirm(nextFilter);
		setOpen(false);
	};

	return (
		<Popover open={open} onOpenChange={setOpen}>
			<PopoverTrigger
				type="button"
				className="inline-flex h-9 min-w-[108px] items-center justify-between gap-2 rounded-xl border border-[var(--leros-control-border)] bg-white px-3.5 text-[13px] text-[var(--leros-text-strong)] transition-colors hover:border-[var(--leros-primary)]"
			>
				<span>{getFilterTriggerLabel(filter, totalCount)}</span>
				<ChevronDown className="size-4 text-[var(--leros-text-muted)]" />
			</PopoverTrigger>
			<PopoverContent align="end" className="w-[320px] rounded-2xl p-0">
				<div className="border-b border-[var(--leros-control-border)] px-4 py-3">
					<div className="relative">
						<Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-[var(--leros-text-muted)]" />
						<input
							value={searchKeyword}
							onChange={(event) => setSearchKeyword(event.target.value)}
							placeholder="搜索成员"
							className="h-9 w-full rounded-xl border border-[var(--leros-control-border)] bg-white pl-9 pr-3 text-[13px] outline-none transition-colors focus:border-[var(--leros-primary)]"
						/>
					</div>
				</div>

				<div className="flex items-center justify-between px-4 py-3">
					<span className="text-[13px] font-medium text-[var(--leros-text-strong)]">筛选成员</span>
					<button
						type="button"
						onClick={toggleSelectAll}
						className="inline-flex items-center gap-2 text-[13px] text-[var(--leros-text-strong)]"
					>
						<span
							className={cn(
								"inline-flex size-4 items-center justify-center rounded-[4px] border",
								allDraftSelected || partialDraftSelected
									? memberFilterControlSelectedClass
									: memberFilterControlUnselectedClass,
							)}
						>
							{(allDraftSelected || partialDraftSelected) && <Check className="size-3" />}
						</span>
						全部成员 ({draftCount}/{totalCount})
					</button>
				</div>

				<div className="no-scrollbar max-h-[180px] overflow-y-auto px-2 pb-1">
					{filteredMembers.length === 0 ? (
						<div className="px-3 py-6 text-center text-[13px] text-[var(--leros-text-muted)]">
							{searchKeyword.trim() ? "没有匹配的成员" : "暂无成员"}
						</div>
					) : (
						filteredMembers.map(({ member, publicId }) => {
							const selected = draftSelectedIds.has(publicId);
							return (
								<button
									key={publicId}
									type="button"
									onClick={() => toggleMember(publicId)}
									className="flex w-full items-center gap-3 rounded-xl px-3 py-2.5 text-left transition-colors hover:bg-[var(--leros-primary-softer)]/40"
								>
									<span
										className={cn(
											"inline-flex size-4 items-center justify-center rounded-full border",
											selected
												? memberFilterControlSelectedClass
												: memberFilterControlUnselectedClass,
										)}
									>
										{selected && <Check className="size-3" />}
									</span>
									<div className="min-w-0 flex-1">
										<div className="truncate text-[13px] font-medium text-[var(--leros-text-strong)]">
											{member.name}
										</div>
										{member.description && (
											<div className="truncate text-[12px] text-[var(--leros-text-muted)]">
												{member.description}
											</div>
										)}
									</div>
								</button>
							);
						})
					)}
				</div>

				<div className="flex gap-3 px-4 pb-4 pt-3">
					<Button
						type="button"
						variant="outline"
						className="h-10 flex-1"
						onClick={() => setOpen(false)}
					>
						取消
					</Button>
					<Button
						type="button"
						className="h-10 flex-1 bg-[var(--leros-text-strong)] text-white hover:bg-[var(--leros-text-strong)]/90 disabled:opacity-40"
						disabled={confirmDisabled}
						onClick={handleConfirm}
					>
						确定
					</Button>
				</div>
			</PopoverContent>
		</Popover>
	);
}

function ProjectActivityRow({ item }: { item: ProjectActivityItem }) {
	const operatorName = resolveProjectActivityOperatorName(item);
	const operatorAvatar = resolveProjectActivityOperatorAvatar(item);
	const actionParts = buildProjectActivityActionParts(item);
	const summaryText = formatProjectActivitySummary(item);
	const relativeTime = formatProjectActivityTime(item.created_at);

	return (
		<div className="flex items-center px-5 py-4">
			<div className="grid min-w-0 flex-1 grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3">
				<div className="min-w-0 overflow-hidden">
					<p
						className="truncate text-[14px] leading-normal text-[var(--leros-text-strong)]"
						title={summaryText}
					>
						<span className="inline-flex items-center gap-1">
							<ActivityOperatorAvatar name={operatorName} avatarUrl={operatorAvatar} />
							<span className="font-semibold">{operatorName}</span>
							{actionParts.map((part, index) => renderProjectActivityActionPart(part, index))}
						</span>
					</p>
				</div>
				{relativeTime ? (
					<span className="whitespace-nowrap text-xs font-normal text-[var(--leros-text-subtle)]">
						{relativeTime}
					</span>
				) : null}
			</div>
		</div>
	);
}

export function ProjectActivityPanel({
	projectId,
	humanMembers,
	refreshKey = 0,
}: ProjectActivityPanelProps) {
	const [items, setItems] = useState<ProjectActivityItem[]>([]);
	const [loading, setLoading] = useState(false);
	const [loadingMore, setLoadingMore] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const [nextCursor, setNextCursor] = useState<string | null>(null);
	const [memberFilter, setMemberFilter] = useState<MemberFilterState>(() =>
		buildDefaultFilter(humanMembers),
	);

	useEffect(() => {
		setMemberFilter(buildDefaultFilter(humanMembers));
	}, [humanMembers]);

	const loadActivities = useCallback(
		async (cursor?: string) => {
			const isInitialLoad = !cursor;
			if (isInitialLoad) {
				setLoading(true);
				setError(null);
			} else {
				setLoadingMore(true);
			}

			try {
				const response = await projectActivityApi.list(
					buildActivityListParams(projectId, memberFilter, cursor),
				);
				const data = response.data.data;
				const nextItems = data?.items ?? [];
				setItems((current) => (isInitialLoad ? nextItems : [...current, ...nextItems]));
				setNextCursor(data?.next_cursor?.trim() || null);
			} catch (err) {
				setError(err instanceof Error ? err.message : "加载项目动态失败");
				if (isInitialLoad) {
					setItems([]);
					setNextCursor(null);
				}
			} finally {
				if (isInitialLoad) {
					setLoading(false);
				} else {
					setLoadingMore(false);
				}
			}
		},
		[projectId, memberFilter],
	);

	useEffect(() => {
		void loadActivities();
	}, [loadActivities, refreshKey]);

	return (
		<div className="h-full overflow-y-auto px-10 py-7">
			<div className="mx-auto w-full max-w-[1200px]">
				<div className="mb-7 flex items-center justify-between gap-5">
					<div>
						<h2 className="text-[2rem] font-semibold tracking-tight text-[var(--leros-text-strong)]">
							动态
						</h2>
						<p className="mt-0.5 text-[13px] text-[var(--leros-text-muted)]">
							查看项目内成员的操作记录
						</p>
					</div>
					<ProjectActivityMemberFilter
						humanMembers={humanMembers}
						filter={memberFilter}
						onConfirm={setMemberFilter}
					/>
				</div>

				{loading ? (
					<div className="flex items-center justify-center py-20 text-[var(--leros-text-muted)]">
						<LoaderCircle className="size-6 animate-spin" />
					</div>
				) : error ? (
					<div className="rounded-2xl border border-[var(--leros-danger)]/20 bg-[var(--leros-danger-softer)] px-4 py-6 text-center text-sm text-[var(--leros-danger)]">
						{error}
					</div>
				) : items.length === 0 ? (
					<div className="rounded-2xl border border-[var(--leros-control-border)] bg-white px-6 py-16 text-center text-sm text-[var(--leros-text-muted)]">
						暂无动态
					</div>
				) : (
					<div className="overflow-hidden rounded-2xl border border-[var(--leros-control-border)] bg-white">
						<div className="divide-y divide-[var(--leros-control-border)]/60">
							{items.map((item) => (
								<ProjectActivityRow key={item.id} item={item} />
							))}
						</div>
						{nextCursor && (
							<div className="border-t border-[var(--leros-control-border)] px-5 py-4 text-center">
								<button
									type="button"
									disabled={loadingMore}
									onClick={() => void loadActivities(nextCursor)}
									className="inline-flex items-center gap-2 text-[13px] font-medium text-[var(--leros-primary)] transition-opacity hover:opacity-80 disabled:opacity-50"
								>
									{loadingMore && <LoaderCircle className="size-4 animate-spin" />}
									加载更多
								</button>
							</div>
						)}
					</div>
				)}
			</div>
		</div>
	);
}
