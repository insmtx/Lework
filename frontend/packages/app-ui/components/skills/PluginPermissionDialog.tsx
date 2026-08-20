"use client";

import {
	type PluginListItem,
	type PluginPermission,
	type PluginPermissionMember,
	type PluginPermissionRole,
	type PluginPermissionSettings,
	type PluginVisibility,
	pluginApi,
	type UserInfo,
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
import { Input } from "@leros/ui/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger } from "@leros/ui/components/ui/select";
import { Loader2, Search, UserPlus, X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";

interface PluginPermissionDialogProps {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	plugin: Pick<PluginListItem, "public_id" | "name" | "display_name" | "kind">;
	permission?: PluginPermission;
	onSaved?: (settings: PluginPermissionSettings) => void;
}

const ROLE_LABELS: Record<PluginPermissionRole, string> = {
	owner: "所有者",
	admin: "管理员",
	viewer: "查看者",
};

const ROLE_DESCRIPTIONS: Record<Exclude<PluginPermissionRole, "owner">, string> = {
	admin: "可查看、使用、编辑插件并管理协作成员。",
	viewer: "只能查看和使用插件。",
};

function displayName(plugin: PluginPermissionDialogProps["plugin"]) {
	return plugin.display_name || plugin.name;
}

function userLabel(user: PluginPermissionMember["user"]) {
	return user.name || user.email || user.public_id;
}

function userSecondaryLabel(user: PluginPermissionMember["user"]) {
	return (
		user.departments?.map((department) => department.name).join("、") || user.email || "组织成员"
	);
}

function toPermissionMember(user: UserInfo): PluginPermissionMember {
	return {
		user: {
			public_id: user.public_id,
			name: user.name,
			email: user.email,
			avatar_url: user.avatar_url,
			departments: user.departments,
		},
		role: "viewer",
	};
}

export function PluginPermissionDialog({
	open,
	onOpenChange,
	plugin,
	permission,
	onSaved,
}: PluginPermissionDialogProps) {
	const [settings, setSettings] = useState<PluginPermissionSettings | null>(null);
	const [visibility, setVisibility] = useState<PluginVisibility>("private");
	const [members, setMembers] = useState<PluginPermissionMember[]>([]);
	const [search, setSearch] = useState("");
	const [candidates, setCandidates] = useState<UserInfo[]>([]);
	const [loading, setLoading] = useState(false);
	const [searching, setSearching] = useState(false);
	const [saving, setSaving] = useState(false);

	const canEditMembers = permission?.role === "owner" || permission?.role === "admin";
	const canChangeVisibility = permission?.role === "owner";

	const memberIDs = useMemo(
		() => new Set(members.map((member) => member.user.public_id)),
		[members],
	);

	useEffect(() => {
		if (!open) return;
		let cancelled = false;
		setLoading(true);
		pluginApi
			.getPermissions(plugin.public_id)
			.then((response) => {
				if (cancelled) return;
				const next = response.data.data;
				setSettings(next);
				setVisibility(next.visibility);
				setMembers(next.members ?? []);
			})
			.catch((error: any) => {
				if (!cancelled) {
					toast.error(error?.response?.data?.message ?? error?.message ?? "加载权限失败");
				}
			})
			.finally(() => {
				if (!cancelled) setLoading(false);
			});
		return () => {
			cancelled = true;
		};
	}, [open, plugin.public_id]);

	useEffect(() => {
		const keyword = search.trim();
		if (!open || !canEditMembers || keyword.length === 0) {
			setCandidates([]);
			return;
		}
		let cancelled = false;
		const timer = window.setTimeout(() => {
			setSearching(true);
			userApi
				.list({ keyword, offset: 0, limit: 20 })
				.then((response) => {
					if (!cancelled) setCandidates(response.data.data.items ?? []);
				})
				.catch(() => {
					if (!cancelled) setCandidates([]);
				})
				.finally(() => {
					if (!cancelled) setSearching(false);
				});
		}, 300);
		return () => {
			cancelled = true;
			window.clearTimeout(timer);
		};
	}, [canEditMembers, open, search]);

	const addMember = (user: UserInfo) => {
		if (memberIDs.has(user.public_id)) return;
		setMembers((current) => [...current, toPermissionMember(user)]);
		setSearch("");
		setCandidates([]);
	};

	const updateMemberRole = (publicID: string, role: PluginPermissionRole) => {
		setMembers((current) =>
			current.map((member) =>
				member.user.public_id === publicID && member.role !== "owner"
					? { ...member, role }
					: member,
			),
		);
	};

	const removeMember = (publicID: string) => {
		setMembers((current) =>
			current.filter((member) => member.user.public_id !== publicID || member.role === "owner"),
		);
	};

	const handleSave = async () => {
		if (!settings || !canEditMembers) return;
		setSaving(true);
		try {
			const response = await pluginApi.updatePermissions(plugin.public_id, {
				visibility: canChangeVisibility ? visibility : settings.visibility,
				members,
			});
			const next = response.data.data;
			setSettings(next);
			setVisibility(next.visibility);
			setMembers(next.members ?? []);
			onSaved?.(next);
			onOpenChange(false);
			toast.success("权限保存成功");
		} catch (error: any) {
			toast.error(error?.response?.data?.message ?? error?.message ?? "保存权限失败");
		} finally {
			setSaving(false);
		}
	};

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="flex max-h-[min(88dvh,760px)] w-[min(620px,94vw)] max-w-none flex-col gap-0 overflow-hidden p-0 sm:rounded-2xl">
				<DialogHeader className="shrink-0 border-b border-[var(--leros-control-border)] px-6 py-5 pr-14">
					<DialogTitle className="text-xl text-[var(--leros-text-strong)]">共享</DialogTitle>
					<DialogDescription className="text-sm text-[var(--leros-text-muted)]">
						{displayName(plugin)}
					</DialogDescription>
				</DialogHeader>

				{loading ? (
					<div className="flex min-h-64 items-center justify-center text-sm text-[var(--leros-text-subtle)]">
						<Loader2 className="mr-2 size-4 animate-spin" />
						加载权限...
					</div>
				) : (
					<div className="min-h-0 flex-1 overflow-y-auto px-6 py-5">
						<section className="mb-5">
							<div className="mb-2 text-sm font-semibold text-[var(--leros-text-strong)]">
								可见范围
							</div>
							<div className="grid grid-cols-2 gap-2">
								{(["public", "private"] as const).map((value) => (
									<button
										key={value}
										type="button"
										disabled={!canChangeVisibility}
										onClick={() => setVisibility(value)}
										className={`rounded-lg border px-3 py-3 text-left transition-colors ${
											visibility === value
												? "border-[var(--leros-primary)] bg-[var(--leros-primary-soft)]"
												: "border-[var(--leros-control-border)] bg-white"
										} disabled:cursor-not-allowed disabled:opacity-70`}
									>
										<div className="text-sm font-medium text-[var(--leros-text-strong)]">
											{value === "public" ? "组织公开" : "私有"}
										</div>
										<div className="mt-1 text-xs text-[var(--leros-text-muted)]">
											{value === "public"
												? "组织成员均可查看和使用。"
												: "仅协作者可查看；关联项目任务执行时，项目成员仍可使用。"}
										</div>
									</button>
								))}
							</div>
							{!canChangeVisibility && (
								<p className="mt-2 text-xs text-[var(--leros-text-subtle)]">
									仅所有者可修改可见范围。
								</p>
							)}
						</section>

						<section>
							<div className="mb-2 text-sm font-semibold text-[var(--leros-text-strong)]">
								协作成员
							</div>
							{canEditMembers && (
								<div className="relative mb-3 flex gap-2">
									<div className="relative min-w-0 flex-1">
										<Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-[var(--leros-text-subtle)]" />
										<Input
											value={search}
											onChange={(event) => setSearch(event.target.value)}
											placeholder="搜索姓名、部门或邮箱"
											className="h-10 pl-9"
										/>
										{search.trim() && (
											<div className="absolute inset-x-0 top-11 z-20 overflow-hidden rounded-lg border border-[var(--leros-control-border)] bg-white shadow-lg">
												{searching ? (
													<div className="px-3 py-3 text-xs text-[var(--leros-text-subtle)]">
														搜索中...
													</div>
												) : candidates.filter((user) => !memberIDs.has(user.public_id)).length >
													0 ? (
													candidates
														.filter((user) => !memberIDs.has(user.public_id))
														.map((user) => (
															<button
																key={user.public_id}
																type="button"
																onClick={() => addMember(user)}
																className="flex w-full items-center gap-2 px-3 py-2 text-left hover:bg-[var(--leros-surface-soft)]"
															>
																<span className="flex size-7 shrink-0 items-center justify-center rounded-full bg-[var(--leros-primary-soft)] text-xs font-medium text-[var(--leros-primary)]">
																	{user.name?.slice(0, 1) || "?"}
																</span>
																<span className="min-w-0 flex-1">
																	<span className="block truncate text-sm text-[var(--leros-text-strong)]">
																		{userLabel(user)}
																	</span>
																	<span className="block truncate text-xs text-[var(--leros-text-subtle)]">
																		{userSecondaryLabel(user)}
																	</span>
																</span>
																<UserPlus className="size-4 text-[var(--leros-primary)]" />
															</button>
														))
												) : (
													<div className="px-3 py-3 text-xs text-[var(--leros-text-subtle)]">
														未找到可添加成员
													</div>
												)}
											</div>
										)}
									</div>
								</div>
							)}

							<div className="space-y-2">
								{members.map((member) => {
									const isOwner = member.role === "owner";
									return (
										<div
											key={member.user.public_id}
											className="flex items-center gap-3 rounded-lg border border-[var(--leros-control-border)] bg-white px-3 py-2.5"
										>
											<span className="flex size-8 shrink-0 items-center justify-center rounded-full bg-[var(--leros-primary-soft)] text-sm font-medium text-[var(--leros-primary)]">
												{member.user.name?.slice(0, 1) || "?"}
											</span>
											<div className="min-w-0 flex-1">
												<div className="truncate text-sm font-medium text-[var(--leros-text-strong)]">
													{userLabel(member.user)}
												</div>
												<div className="truncate text-xs text-[var(--leros-text-subtle)]">
													{userSecondaryLabel(member.user)}
												</div>
											</div>
											<Select
												value={member.role}
												disabled={!canEditMembers || isOwner}
												onValueChange={(value) =>
													updateMemberRole(
														member.user.public_id,
														(value ?? "viewer") as PluginPermissionRole,
													)
												}
											>
												<SelectTrigger
													size="sm"
													className="w-24"
													aria-label={`设置 ${userLabel(member.user)} 的角色`}
												>
													<span>{ROLE_LABELS[member.role]}</span>
												</SelectTrigger>
												<SelectContent align="end">
													<SelectItem value="admin">管理员</SelectItem>
													<SelectItem value="viewer">查看者</SelectItem>
												</SelectContent>
											</Select>
											{canEditMembers && !isOwner && (
												<Button
													variant="ghost"
													size="icon-sm"
													aria-label={`移除 ${userLabel(member.user)}`}
													onClick={() => removeMember(member.user.public_id)}
												>
													<X className="size-4" />
												</Button>
											)}
										</div>
									);
								})}
							</div>
							<div className="mt-4 rounded-lg bg-[var(--leros-primary-soft)] px-3 py-2.5 text-xs leading-5 text-[var(--leros-text-muted)]">
								<div>
									<span className="font-semibold text-[var(--leros-text-strong)]">管理员</span>　
									{ROLE_DESCRIPTIONS.admin}
								</div>
								<div className="mt-1">
									<span className="font-semibold text-[var(--leros-text-strong)]">查看者</span>　
									{ROLE_DESCRIPTIONS.viewer}
								</div>
							</div>
						</section>
					</div>
				)}

				<DialogFooter className="shrink-0 border-t border-[var(--leros-control-border)] px-6 py-4">
					<Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
						取消
					</Button>
					<Button onClick={handleSave} disabled={loading || saving || !settings || !canEditMembers}>
						{saving && <Loader2 className="mr-1.5 size-4 animate-spin" />}
						保存
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
