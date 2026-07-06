"use client";

import { type AuthUser, useAuthStore, useChatStore, useLayoutStore } from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import { Input } from "@leros/ui/components/ui/input";
import { cn } from "@leros/ui/lib/utils";
import { Check, ChevronRight, Loader2, Plus } from "lucide-react";
import { type FormEvent, useState } from "react";
import { toast } from "sonner";
import { DiceBearAvatar } from "../avatar/DiceBearAvatar";
import { ProtectedImage } from "../avatar/ProtectedImage";
import type { AppNavigation } from "../layout/LeftRail";

type OrganizationSwitchPanelProps = {
	navigation?: AppNavigation;
	onDone?: () => void;
};

type PanelMode = "switch" | "create";

export function OrganizationSwitchPanel({ navigation, onDone }: OrganizationSwitchPanelProps) {
	const user = useAuthStore((s) => s.authUser);
	const switchOrganization = useAuthStore((s) => s.switchOrganization);
	const createOrganization = useAuthStore((s) => s.createOrganization);
	const fetchProjects = useLayoutStore((s) => s.fetchProjects);
	const resetAuthScopedData = useLayoutStore((s) => s.resetAuthScopedData);
	const switchView = useLayoutStore((s) => s.switchView);
	const clearComposerInput = useChatStore((s) => s.clearComposerInput);
	const resetLocalMessages = useChatStore((s) => s.resetLocalMessages);
	const [mode, setMode] = useState<PanelMode>("switch");
	const [organizationName, setOrganizationName] = useState("");
	const [switchingOrgId, setSwitchingOrgId] = useState<number | null>(null);
	const [creating, setCreating] = useState(false);

	const resetOrgScopedData = async () => {
		resetAuthScopedData();
		resetLocalMessages();
		clearComposerInput();
		await fetchProjects();
		if (navigation) {
			navigation.goToRoute("workbench");
		} else {
			switchView("workbench");
		}
	};

	const handleSwitchOrganization = async (orgId: number) => {
		if (!user || user.currentOrg?.id === orgId || switchingOrgId !== null || creating) return;
		setSwitchingOrgId(orgId);
		try {
			await switchOrganization(orgId);
			await resetOrgScopedData();
			onDone?.();
			toast.success("已切换组织");
		} catch (error) {
			toast.error(error instanceof Error ? error.message : "切换组织失败，请稍后重试");
		} finally {
			setSwitchingOrgId(null);
		}
	};

	const handleCreateOrganization = async (event: FormEvent<HTMLFormElement>) => {
		event.preventDefault();
		const name = organizationName.trim();
		if (!name || creating) return;
		setCreating(true);
		try {
			await createOrganization(name);
			await resetOrgScopedData();
			onDone?.();
			toast.success("已创建并切换组织");
		} catch (error) {
			toast.error(error instanceof Error ? error.message : "创建组织失败，请稍后重试");
		} finally {
			setCreating(false);
		}
	};

	if (mode === "create") {
		return (
			<form onSubmit={handleCreateOrganization} className="flex w-full flex-col">
				<div className="border-b border-[var(--leros-control-border)] pb-4">
					<h2 className="text-lg font-semibold text-[var(--leros-text-strong)]">创建新组织</h2>
				</div>
				<div className="py-5">
					<label
						className="block text-sm font-medium text-[var(--leros-text-muted)]"
						htmlFor="organization-name"
					>
						组织名称
					</label>
					<Input
						id="organization-name"
						value={organizationName}
						onChange={(event) => setOrganizationName(event.target.value)}
						placeholder="请输入组织名称"
						maxLength={50}
						autoFocus
						className="mt-2 h-10"
					/>
				</div>
				<div className="flex justify-end gap-3">
					<Button type="button" variant="outline" onClick={() => setMode("switch")}>
						取消
					</Button>
					<Button type="submit" disabled={!organizationName.trim() || creating}>
						{creating ? <Loader2 className="size-4 animate-spin" /> : null}
						<span>创建并切换</span>
					</Button>
				</div>
			</form>
		);
	}

	return (
		<div className="flex w-full flex-col">
			<div className="border-b border-[var(--leros-control-border)] pb-4">
				<h2 className="text-lg font-semibold text-[var(--leros-text-strong)]">切换组织</h2>
			</div>
			<div className="flex justify-end py-3">
				<button
					type="button"
					onClick={() => setMode("create")}
					className="inline-flex items-center gap-1.5 text-sm font-medium text-[var(--leros-text-subtle)] transition-colors hover:text-[var(--leros-text-strong)]"
				>
					<Plus className="size-4" />
					<span>创建新组织</span>
				</button>
			</div>
			<OrganizationList
				user={user}
				switchingOrgId={switchingOrgId}
				onSwitch={(orgId) => void handleSwitchOrganization(orgId)}
			/>
		</div>
	);
}

function OrganizationList({
	user,
	switchingOrgId,
	onSwitch,
}: {
	user: AuthUser | null;
	switchingOrgId: number | null;
	onSwitch: (orgID: number) => void;
}) {
	const organizations = user?.organizations ?? [];
	if (organizations.length === 0) {
		return (
			<div className="rounded-lg bg-slate-50 px-4 py-8 text-center text-sm text-slate-500">
				暂无可切换组织
			</div>
		);
	}

	return (
		<div className="space-y-2">
			{organizations.map((org) => {
				const active = org.id === user?.currentOrg?.id;
				const switching = switchingOrgId === org.id;
				return (
					<button
						key={org.id}
						type="button"
						disabled={active || switchingOrgId !== null}
						onClick={() => onSwitch(org.id)}
						className={cn(
							"flex min-h-[52px] w-full items-center gap-3 rounded-lg border px-3 py-2.5 text-left transition-colors",
							active
								? "cursor-default border-[var(--leros-primary-soft)] bg-[var(--leros-primary-softer)]"
								: "border-[var(--leros-control-border)] bg-[var(--leros-surface)] hover:bg-[var(--leros-primary-softer)]",
							switchingOrgId !== null && !switching && "opacity-60",
						)}
					>
						<span className="flex size-10 shrink-0 overflow-hidden rounded-full bg-[var(--leros-primary)]">
							<ProtectedImage
								src={org.logo}
								alt={org.name}
								className="h-full w-full object-cover"
								fallback={
									<DiceBearAvatar
										seed={`org:${org.name}`}
										alt={org.name}
										className="h-full w-full"
										size={40}
									/>
								}
							/>
						</span>
						<span className="min-w-0 flex-1">
							<span className="block truncate text-sm font-medium text-[var(--leros-text-strong)]">
								{org.name}
							</span>
							{active ? (
								<span className="mt-0.5 inline-flex items-center gap-1 text-xs font-medium text-[var(--leros-primary)]">
									<Check className="size-3.5" />
									当前
								</span>
							) : null}
						</span>
						{switching ? (
							<Loader2 className="size-4 shrink-0 animate-spin text-[var(--leros-text-subtle)]" />
						) : active ? (
							<Check className="size-4 shrink-0 text-[var(--leros-primary)]" />
						) : (
							<ChevronRight className="size-4 shrink-0 text-[var(--leros-text-subtle)]" />
						)}
					</button>
				);
			})}
		</div>
	);
}
