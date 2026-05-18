"use client";

import type { DigitalAssistantItem } from "@leros/store";
import { useDAStore } from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import { ScrollArea } from "@leros/ui/components/ui/scroll-area";
import { Tabs, TabsList, TabsTrigger } from "@leros/ui/components/ui/tabs";
import { Plus, Search } from "lucide-react";
import { useEffect, useState } from "react";
import { AssistantCard } from "./AssistantCard";
import { AssistantCreateDialog } from "./AssistantCreateDialog";
import { AssistantDeleteDialog } from "./AssistantDeleteDialog";
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

	const [createDialogOpen, setCreateDialogOpen] = useState(false);
	const [editTarget, setEditTarget] = useState<DigitalAssistantItem | null>(null);
	const [deleteTarget, setDeleteTarget] = useState<DigitalAssistantItem | null>(null);

	useEffect(() => {
		fetchAssistants();
	}, [fetchAssistants]);

	const filteredAssistants = assistants.filter((a) => {
		const matchesSearch =
			!assistantSearchQuery ||
			a.name.toLowerCase().includes(assistantSearchQuery.toLowerCase()) ||
			a.description.toLowerCase().includes(assistantSearchQuery.toLowerCase());
		const matchesStatus = !assistantStatusFilter || a.status === assistantStatusFilter;
		return matchesSearch && matchesStatus;
	});

	return (
		<div data-slot="assistant-list-view" className="flex h-full flex-1 flex-col bg-white">
			<div className="flex items-center justify-between border-b border-slate-200 px-6 py-4">
				<h2 className="text-lg font-semibold text-slate-900">AI 员工</h2>
				<Button size="sm" onClick={() => setCreateDialogOpen(true)}>
					<Plus className="size-4 mr-1" />
					新建员工
				</Button>
			</div>

			<div className="flex items-center gap-4 border-b border-slate-100 px-6 py-3">
				<div className="relative flex-1 max-w-xs">
					<Search className="absolute left-2.5 top-1/2 -translate-y-1/2 size-3.5 text-slate-400" />
					<input
						type="text"
						value={assistantSearchQuery}
						onChange={(e) => setAssistantSearchQuery(e.target.value)}
						placeholder="搜索员工"
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
							<span className="text-sm">暂无 AI 员工</span>
							<Button
								variant="outline"
								size="sm"
								className="mt-4"
								onClick={() => setCreateDialogOpen(true)}
							>
								<Plus className="size-4 mr-1" />
								创建第一个员工
							</Button>
						</div>
					)}
					{filteredAssistants.map((a) => (
						<AssistantCard
							key={a.id}
							assistant={a}
							onEdit={setEditTarget}
							onDelete={setDeleteTarget}
						/>
					))}
				</div>
			</ScrollArea>

			<AssistantCreateDialog open={createDialogOpen} onOpenChange={setCreateDialogOpen} />
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
