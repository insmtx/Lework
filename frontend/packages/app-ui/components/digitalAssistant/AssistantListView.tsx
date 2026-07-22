"use client";

import type { DigitalAssistantItem } from "@leros/store";
import { useDAStore, useLayoutStore } from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@leros/ui/components/ui/dialog";
import { ScrollArea } from "@leros/ui/components/ui/scroll-area";
import { Bot, CheckCircle2, Plus, Search } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { useAuth } from "../auth";
import type { AppNavigation } from "../layout";
import { navigateToWorkbench } from "../layout/workbench-navigation";
import { buildAssistantWorkbenchPrefill } from "../layout/workbench-prefill";
import { AssistantAvatar } from "./AssistantAvatar";
import { AssistantCard } from "./AssistantCard";
import { AssistantCreateDialog } from "./AssistantCreateDialog";
import { AssistantDeleteDialog } from "./AssistantDeleteDialog";
import { AssistantDetailDialog } from "./AssistantDetailDialog";
import { AssistantEditDialog } from "./AssistantEditDialog";
import { isAssistantAvailable } from "./assistantStatus";

export function AssistantListView({ navigation }: { navigation?: AppNavigation }) {
	const { isAuthenticated, requireAuth } = useAuth();
	const { assistants, assistantSearchQuery, fetchAssistants, setAssistantSearchQuery } = useDAStore(
		(s) => s,
	);
	const { setWorkbenchComposerPrefill, selectWorkbenchProject, selectWorkbenchTask, switchView } =
		useLayoutStore((s) => s);

	const [createDialogOpen, setCreateDialogOpen] = useState(false);
	const [detailTarget, setDetailTarget] = useState<DigitalAssistantItem | null>(null);
	const [editTarget, setEditTarget] = useState<DigitalAssistantItem | null>(null);
	const [deleteTarget, setDeleteTarget] = useState<DigitalAssistantItem | null>(null);
	const [createdAssistantIds, setCreatedAssistantIds] = useState<number[]>([]);
	const [createdAssistantReady, setCreatedAssistantReady] = useState<DigitalAssistantItem | null>(
		null,
	);
	const visibleAssistants = isAuthenticated ? assistants : [];
	const liveDetailTarget = detailTarget
		? (visibleAssistants.find((assistant) => assistant.id === detailTarget.id) ?? detailTarget)
		: null;

	useEffect(() => {
		return () => setAssistantSearchQuery("");
	}, [setAssistantSearchQuery]);

	useEffect(() => {
		if (isAuthenticated) return;
		setCreateDialogOpen(false);
		setDetailTarget(null);
		setEditTarget(null);
		setDeleteTarget(null);
		setCreatedAssistantIds([]);
		setCreatedAssistantReady(null);
	}, [isAuthenticated]);

	useEffect(() => {
		if (!isAuthenticated) return;
		fetchAssistants();
	}, [fetchAssistants, isAuthenticated]);

	useEffect(() => {
		if (!isAuthenticated) return;
		const hasDeployingAssistant = visibleAssistants.some((assistant) =>
			["pending", "provisioning"].includes(assistant.deploymentStatus),
		);
		if (!hasDeployingAssistant) return;

		const timer = window.setInterval(() => {
			fetchAssistants();
		}, 2000);
		return () => window.clearInterval(timer);
	}, [fetchAssistants, isAuthenticated, visibleAssistants]);

	useEffect(() => {
		// 中文注释：按队列追踪本次页面内创建的队友，避免连续创建时遗漏部署成功提示。
		if (createdAssistantReady || createdAssistantIds.length === 0) return;
		const assistant = visibleAssistants.find(
			(item) => createdAssistantIds.includes(item.id) && isAssistantAvailable(item),
		);
		if (!assistant) return;
		setCreatedAssistantReady(assistant);
		setCreatedAssistantIds((ids) => ids.filter((id) => id !== assistant.id));
	}, [createdAssistantIds, createdAssistantReady, visibleAssistants]);

	const filteredAssistants = visibleAssistants.filter((a) => {
		const matchesSearch =
			!assistantSearchQuery ||
			a.name.toLowerCase().includes(assistantSearchQuery.toLowerCase()) ||
			a.roleName.toLowerCase().includes(assistantSearchQuery.toLowerCase()) ||
			a.description.toLowerCase().includes(assistantSearchQuery.toLowerCase());
		return matchesSearch;
	});

	const openCreateDialog = () => {
		if (!isAuthenticated) {
			requireAuth(() => setCreateDialogOpen(true));
			return;
		}
		setCreateDialogOpen(true);
	};

	const handleSelectAssistant = (assistant: DigitalAssistantItem) => {
		requireAuth(() => setDetailTarget(assistant));
	};

	const handleSummonAssistant = (assistant: DigitalAssistantItem, prompt?: string) => {
		requireAuth(() => {
			if (!isAssistantAvailable(assistant)) {
				toast.info("AI 队友仍在部署中，请稍后再试");
				return;
			}
			const assistantIdentity = assistant.publicId || String(assistant.id);

			selectWorkbenchProject(null);
			selectWorkbenchTask(null);
			setWorkbenchComposerPrefill(
				buildAssistantWorkbenchPrefill(assistantIdentity, assistant, prompt),
			);
			navigateToWorkbench(navigation, switchView);
			toast.success(`已成功召唤 ${assistant.name}`);
			setDetailTarget(null);
		});
	};

	return (
		<div
			data-slot="assistant-list-view"
			className="flex h-full min-h-0 min-w-0 flex-1 flex-col bg-white"
		>
			<div className="flex items-center justify-between border-b border-slate-200 px-6 py-4">
				<h2 className="text-lg font-semibold text-slate-900">AI队友</h2>
				<div className="flex items-center gap-3">
					<div className="relative w-60">
						<Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-slate-400" />
						<input
							type="text"
							value={assistantSearchQuery}
							onChange={(e) => setAssistantSearchQuery(e.target.value)}
							placeholder="搜索队友"
							className="w-full rounded-md border border-slate-200 bg-slate-50 py-1.5 pl-7 pr-2 text-xs text-slate-600 transition-colors placeholder:text-slate-400 focus:border-blue-300 focus:bg-white focus:outline-none"
						/>
					</div>
					<Button size="sm" onClick={openCreateDialog}>
						<Plus className="size-4 mr-1" />
						新建队友
					</Button>
				</div>
			</div>

			{/* 中文注释：允许列表区域在固定高度的应用壳内收缩，超出内容由滚动容器承接。 */}
			<ScrollArea className="min-h-0 flex-1">
				<div className="grid grid-cols-1 gap-3 p-6 lg:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
					{filteredAssistants.length === 0 && (
						<div className="col-span-full flex min-h-[calc(100vh-11rem)] flex-col items-center justify-center text-center">
							{visibleAssistants.length === 0 ? (
								<>
									{/* 中文注释：空列表仅保留引导信息，创建入口统一位于页面右上角。 */}
									<div className="flex size-24 items-center justify-center rounded-2xl border border-dashed border-slate-300 text-slate-400">
										<Bot className="size-10" strokeWidth={1.5} />
									</div>
									<p className="mt-6 text-xl font-semibold text-slate-900">暂无 AI 队友</p>
									<p className="mt-3 whitespace-nowrap text-sm leading-6 text-slate-400">
										创建你的第一个 AI 队友，让它加入项目、承接任务并与你持续协作。
									</p>
								</>
							) : (
								<p className="text-sm text-slate-400">未找到相关 AI 队友</p>
							)}
						</div>
					)}
					{filteredAssistants.map((a) => (
						<AssistantCard
							key={a.id}
							assistant={a}
							onSelect={handleSelectAssistant}
							onSummon={handleSummonAssistant}
							onEdit={(assistant) => requireAuth(() => setEditTarget(assistant))}
							onDelete={(assistant) => requireAuth(() => setDeleteTarget(assistant))}
						/>
					))}
				</div>
			</ScrollArea>

			<AssistantCreateDialog
				open={createDialogOpen}
				onOpenChange={setCreateDialogOpen}
				onCreated={(assistant) =>
					setCreatedAssistantIds((ids) =>
						ids.includes(assistant.id) ? ids : [...ids, assistant.id],
					)
				}
			/>
			<AssistantDetailDialog
				assistant={liveDetailTarget}
				open={!!liveDetailTarget}
				summoning={false}
				onOpenChange={(open) => {
					if (!open) setDetailTarget(null);
				}}
				onSummon={handleSummonAssistant}
			/>
			{editTarget && (
				<AssistantEditDialog
					assistant={editTarget}
					open={!!editTarget}
					onOpenChange={(open) => {
						if (!open) setEditTarget(null);
					}}
				/>
			)}
			{deleteTarget && (
				<AssistantDeleteDialog
					assistant={deleteTarget}
					open={!!deleteTarget}
					onOpenChange={(open) => {
						if (!open) setDeleteTarget(null);
					}}
				/>
			)}
			<Dialog
				open={!!createdAssistantReady}
				onOpenChange={(open) => !open && setCreatedAssistantReady(null)}
			>
				<DialogContent className="gap-0 p-8 sm:max-w-md">
					<DialogHeader className="items-center gap-3 pb-6 text-center">
						<CheckCircle2 className="size-12 text-emerald-500" />
						<DialogTitle className="pt-1">AI队友部署完成</DialogTitle>
						<DialogDescription>现在可以在Lework上与它对话。</DialogDescription>
					</DialogHeader>
					{createdAssistantReady ? (
						<div className="flex items-center gap-3 rounded-xl border border-slate-200 bg-slate-50 px-4 py-4">
							<AssistantAvatar
								name={createdAssistantReady.name}
								src={createdAssistantReady.avatar}
							/>
							<div>
								<div className="font-medium text-slate-900">{createdAssistantReady.name}</div>
								{createdAssistantReady.roleName ? (
									<div className="mt-1 text-sm text-slate-500">
										{createdAssistantReady.roleName}
									</div>
								) : null}
							</div>
						</div>
					) : null}
					<DialogFooter className="mt-4">
						<Button
							className="h-10 w-full"
							onClick={() => {
								if (createdAssistantReady) handleSummonAssistant(createdAssistantReady);
								setCreatedAssistantReady(null);
							}}
						>
							开始对话
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		</div>
	);
}
