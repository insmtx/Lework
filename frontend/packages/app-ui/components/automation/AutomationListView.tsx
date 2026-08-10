"use client";

import type { AutomationItem } from "@leros/store";
import { useAutomationStore } from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import { ScrollArea } from "@leros/ui/components/ui/scroll-area";
import { Skeleton } from "@leros/ui/components/ui/skeleton";
import { Clock, Plus, TriangleAlert } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { useAuth } from "../auth";
import type { AppNavigation } from "../layout";
import { AutomationCard } from "./AutomationCard";
import { AutomationDeleteDialog } from "./AutomationDeleteDialog";
import { AutomationFormDialog } from "./AutomationFormDialog";

export function AutomationListView({ navigation }: { navigation?: AppNavigation }) {
	const {
		automations,
		loaded,
		loading,
		error,
		fetchAutomations,
		refreshAutomations,
		toggleAutomation,
		runAutomationNow,
	} = useAutomationStore((s) => s);

	const { isAuthenticated, requireAuth } = useAuth();

	const [createOpen, setCreateOpen] = useState(false);
	const [editTarget, setEditTarget] = useState<AutomationItem | null>(null);
	const [deleteTarget, setDeleteTarget] = useState<AutomationItem | null>(null);

	const visibleAutomations = isAuthenticated ? automations : [];

	useEffect(() => {
		if (!isAuthenticated) return;
		fetchAutomations();
	}, [fetchAutomations, isAuthenticated]);

	// 页面可见时轮询刷新（执行期间状态需及时更新）
	useEffect(() => {
		if (!isAuthenticated) return;
		const interval = window.setInterval(() => {
			if (document.hidden) return;
			refreshAutomations();
		}, 10_000);
		return () => window.clearInterval(interval);
	}, [isAuthenticated, refreshAutomations]);

	const openCreate = () => {
		if (!isAuthenticated) {
			requireAuth(() => setCreateOpen(true));
			return;
		}
		setCreateOpen(true);
	};

	const handleToggle = (automation: AutomationItem, enabled: boolean) => {
		if (!isAuthenticated) return;
		void toggleAutomation(automation.publicId, enabled);
	};

	const handleRunNow = async (automation: AutomationItem) => {
		if (!isAuthenticated) return;
		const result = await runAutomationNow(automation.publicId);
		if (result === "ok") {
			toast.success("已开始运行，执行结果稍后可见");
			void refreshAutomations();
		} else if (result === "conflict") {
			toast.info("该自动化已有执行正在进行，请稍后再试");
		} else {
			toast.error("立即运行失败，请稍后重试");
		}
	};

	return (
		<div
			data-slot="automation-list-view"
			className="flex h-full min-h-0 min-w-0 flex-1 flex-col bg-white"
		>
			<div className="flex items-center justify-between border-b border-slate-200 px-6 py-4">
				<h2 className="text-lg font-semibold text-slate-900">自动化</h2>
				<Button size="sm" onClick={openCreate}>
					<Plus className="mr-1 size-4" />
					创建自动化
				</Button>
			</div>

			<ScrollArea className="min-h-0 flex-1">
				<div className="grid grid-cols-[repeat(auto-fill,minmax(280px,1fr))] gap-4 p-6">
					{loading && !loaded ? (
						Array.from({ length: 4 }).map((_, i) => (
							<Skeleton key={i} className="h-44 w-full rounded-2xl" />
						))
					) : isAuthenticated && error && visibleAutomations.length === 0 ? (
						<div className="col-span-full flex min-h-[calc(100vh-11rem)] flex-col items-center justify-center text-center">
							<div className="flex size-24 items-center justify-center rounded-2xl border border-dashed border-red-300 text-red-400">
								<TriangleAlert className="size-10" strokeWidth={1.5} />
							</div>
							<p className="mt-6 text-xl font-semibold text-slate-900">加载失败</p>
							<p className="mt-3 whitespace-nowrap text-sm leading-6 text-slate-400">{error}</p>
							<Button variant="outline" size="sm" className="mt-4" onClick={refreshAutomations}>
								重试
							</Button>
						</div>
					) : visibleAutomations.length === 0 ? (
						<div className="col-span-full flex min-h-[calc(100vh-11rem)] flex-col items-center justify-center text-center">
							<div className="flex size-24 items-center justify-center rounded-2xl border border-dashed border-slate-300 text-slate-400">
								<Clock className="size-10" strokeWidth={1.5} />
							</div>
							<p className="mt-6 text-xl font-semibold text-slate-900">暂无自动化</p>
							<p className="mt-3 whitespace-nowrap text-sm leading-6 text-slate-400">
								创建你的第一条自动化，让 Agent 按计划自动执行任务。
							</p>
						</div>
					) : (
						visibleAutomations.map((automation) => (
							<AutomationCard
								key={automation.publicId}
								automation={automation}
								onOpen={(a) => requireAuth(() => navigation?.goToAutomationDetail(a.publicId))}
								onRunNow={handleRunNow}
								onEdit={(a) => requireAuth(() => setEditTarget(a))}
								onDelete={(a) => requireAuth(() => setDeleteTarget(a))}
								onToggle={handleToggle}
							/>
						))
					)}
				</div>
			</ScrollArea>

			<AutomationFormDialog open={createOpen} onOpenChange={setCreateOpen} editTarget={null} />
			{editTarget && (
				<AutomationFormDialog
					open={!!editTarget}
					onOpenChange={(open) => {
						if (!open) setEditTarget(null);
					}}
					editTarget={editTarget}
				/>
			)}
			<AutomationDeleteDialog
				open={!!deleteTarget}
				onOpenChange={(open) => {
					if (!open) setDeleteTarget(null);
				}}
				target={deleteTarget}
			/>
		</div>
	);
}
