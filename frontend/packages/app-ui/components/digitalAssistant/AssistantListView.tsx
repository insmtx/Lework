"use client";

import type { DigitalAssistantItem } from "@leros/store";
import { useDAStore, useLayoutStore } from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import { ScrollArea } from "@leros/ui/components/ui/scroll-area";
import { Tabs, TabsList, TabsTrigger } from "@leros/ui/components/ui/tabs";
import { ArrowLeft, Plus, Search } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { AssistantCard } from "./AssistantCard";
import { AssistantCreateDialog } from "./AssistantCreateDialog";
import { AssistantDeleteDialog } from "./AssistantDeleteDialog";
import { AssistantDetailDialog } from "./AssistantDetailDialog";
import { AssistantEditDialog } from "./AssistantEditDialog";

const statusFilters = [
	{ value: "", label: "全部" },
	{ value: "active", label: "运行中" },
	{ value: "inactive", label: "已停用" },
	{ value: "draft", label: "草稿" },
];

export function AssistantListView() {
	const {
		assistants,
		assistantSearchQuery,
		assistantStatusFilter,
		fetchAssistants,
		setAssistantSearchQuery,
		setAssistantStatusFilter,
	} = useDAStore((s) => s);
	const { sendWorkbenchMessage } = useLayoutStore((s) => s);

	const [createDialogOpen, setCreateDialogOpen] = useState(false);
	const [detailTarget, setDetailTarget] = useState<DigitalAssistantItem | null>(null);
	const [editTarget, setEditTarget] = useState<DigitalAssistantItem | null>(null);
	const [deleteTarget, setDeleteTarget] = useState<DigitalAssistantItem | null>(null);
	const [summoningId, setSummoningId] = useState<number | null>(null);

	useEffect(() => {
		fetchAssistants();
	}, [fetchAssistants]);

	useEffect(() => {
		const hasDeployingAssistant = assistants.some((assistant) =>
			["pending", "provisioning"].includes(assistant.deploymentStatus),
		);
		if (!hasDeployingAssistant) return;

		const timer = window.setInterval(() => {
			fetchAssistants();
		}, 2000);
		return () => window.clearInterval(timer);
	}, [assistants, fetchAssistants]);

	const filteredAssistants = assistants.filter((a) => {
		const matchesSearch =
			!assistantSearchQuery ||
			a.name.toLowerCase().includes(assistantSearchQuery.toLowerCase()) ||
			a.description.toLowerCase().includes(assistantSearchQuery.toLowerCase());
		const matchesStatus = !assistantStatusFilter || a.status === assistantStatusFilter;
		return matchesSearch && matchesStatus;
	});

	const handleSelectAssistant = (assistant: DigitalAssistantItem) => {
		setDetailTarget(assistant);
	};

	const handleSummonAssistant = async (assistant: DigitalAssistantItem, prompt?: string) => {
		if (summoningId) return;
		setSummoningId(assistant.id);
		try {
			// 召唤走 NewMessage：带 assistant_id 创建任务会话。
			// - 有 prompt（点击「试试这样问我」）：发首条消息，落地后即开始对话。
			// - 无 prompt（点击 footer「召唤」）：空 content，仅创建空任务会话，不发首条消息。
			const content = prompt?.trim() || "";
			const data = await sendWorkbenchMessage(
				content,
				"",
				undefined,
				undefined,
				undefined,
				assistant.id,
			);
			if (!data?.project_id || !data.task_id || !data.session_id) {
				throw new Error("No task session returned");
			}
			navigateToTaskDetail(data.project_id, data.task_id, data.session_id);
			toast.success(`已召唤 ${assistant.name}`);
			setDetailTarget(null);
		} catch (err) {
			console.error("summon my teammate error:", err);
			toast.error("召唤队友失败");
		} finally {
			setSummoningId(null);
		}
	};

	const navigateToAITeammates = () => {
		if (window.location.hash) {
			window.location.hash = "/ai-teammates";
			return;
		}
		window.location.href = "/ai-teammates";
	};

	return (
		<div data-slot="assistant-list-view" className="flex h-full flex-1 flex-col bg-white">
			<div className="flex items-center justify-between border-b border-slate-200 px-6 py-4">
				<h2 className="text-lg font-semibold text-slate-900">AI 队友</h2>
				<div className="flex items-center gap-2">
					<Button variant="outline" size="sm" onClick={navigateToAITeammates}>
						<ArrowLeft className="size-4 mr-1" />
						返回 AI 队友
					</Button>
					<Button size="sm" onClick={() => setCreateDialogOpen(true)}>
						<Plus className="size-4 mr-1" />
						新建队友
					</Button>
				</div>
			</div>

			<div className="flex items-center gap-4 border-b border-slate-100 px-6 py-3">
				<div className="relative flex-1 max-w-xs">
					<Search className="absolute left-2.5 top-1/2 -translate-y-1/2 size-3.5 text-slate-400" />
					<input
						type="text"
						value={assistantSearchQuery}
						onChange={(e) => setAssistantSearchQuery(e.target.value)}
						placeholder="搜索队友"
						className="w-full rounded-md border border-slate-200 bg-slate-50 py-1.5 pl-7 pr-2 text-xs text-slate-600 placeholder:text-slate-400 focus:border-blue-300 focus:bg-white focus:outline-none transition-colors"
					/>
				</div>
				<Tabs value={assistantStatusFilter} onValueChange={setAssistantStatusFilter}>
					<TabsList variant="line">
						{statusFilters.map((f) => (
							<TabsTrigger key={f.value} value={f.value}>
								{f.label}
							</TabsTrigger>
						))}
					</TabsList>
				</Tabs>
			</div>

			<ScrollArea className="flex-1">
				<div className="grid grid-cols-1 gap-3 p-6 lg:grid-cols-2 xl:grid-cols-3">
					{filteredAssistants.length === 0 && (
						<div className="col-span-full flex flex-col items-center justify-center py-16 text-slate-400">
							<span className="text-sm">暂无 AI 队友</span>
							<Button
								variant="outline"
								size="sm"
								className="mt-4"
								onClick={() => setCreateDialogOpen(true)}
							>
								<Plus className="size-4 mr-1" />
								创建第一个队友
							</Button>
						</div>
					)}
					{filteredAssistants.map((a) => (
						<AssistantCard
							key={a.id}
							assistant={a}
							onSelect={handleSelectAssistant}
							onEdit={setEditTarget}
							onDelete={setDeleteTarget}
						/>
					))}
				</div>
			</ScrollArea>

			<AssistantCreateDialog open={createDialogOpen} onOpenChange={setCreateDialogOpen} />
			<AssistantDetailDialog
				assistant={detailTarget}
				open={!!detailTarget}
				summoning={detailTarget ? summoningId === detailTarget.id : false}
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
		</div>
	);
}

function navigateToTaskDetail(projectId: string, taskId: string, sessionId: string) {
	const path = `/projects/${projectId}/tasks/${taskId}?sessionId=${encodeURIComponent(sessionId)}`;
	if (window.location.hash) {
		window.location.hash = path;
		return;
	}
	window.location.href = path;
}
