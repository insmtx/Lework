"use client";

import type { ModelItem } from "@leros/store";
import { useModelStore } from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
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
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@leros/ui/components/ui/dropdown-menu";
import { ScrollArea } from "@leros/ui/components/ui/scroll-area";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@leros/ui/components/ui/table";
import { Bot, Loader2, MoreHorizontal, Plus, Trash2, Wifi } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { ModelFormDialog } from "./ModelFormDialog";

const PROVIDER_LABELS: Record<string, string> = {
	openai: "自定义 (OpenAI 兼容)",
};

export function ModelManagementView() {
	const { models, loading, loaded, fetchModels, deleteModel, setDefault, setStatus, testModel } =
		useModelStore((s) => s);
	const [createOpen, setCreateOpen] = useState(false);
	const [editTarget, setEditTarget] = useState<ModelItem | null>(null);
	const [deleteTarget, setDeleteTarget] = useState<ModelItem | null>(null);
	const [deleting, setDeleting] = useState(false);
	const [testingId, setTestingId] = useState<number | null>(null);

	useEffect(() => {
		void fetchModels();
	}, [fetchModels]);

	const handleDelete = useCallback(async () => {
		if (!deleteTarget || deleting) return;
		if (deleteTarget.isSystem) {
			toast.error("系统内置模型不可删除");
			setDeleteTarget(null);
			return;
		}
		setDeleting(true);
		try {
			await deleteModel(deleteTarget.id);
			toast.success("模型已删除");
			setDeleteTarget(null);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : "删除失败，请稍后重试");
		} finally {
			setDeleting(false);
		}
	}, [deleteTarget, deleting, deleteModel]);

	const handleSetDefault = async (item: ModelItem) => {
		try {
			await setDefault(item.id);
			toast.success("已设为默认模型");
		} catch (err) {
			toast.error(err instanceof Error ? err.message : "设置默认失败");
		}
	};

	const handleSetStatus = async (item: ModelItem, status: string) => {
		const verb = status === "active" ? "启用" : "禁用";
		try {
			await setStatus(item.id, status);
			toast.success(`模型已${verb}`);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : `${verb}失败`);
		}
	};

	const handleTestModel = async (item: ModelItem) => {
		if (testingId !== null) return;
		setTestingId(item.id);
		try {
			const res = await testModel({ id: item.id });
			const data = res.data.data;
			if (data?.success) {
				toast.success(data.message || "连接成功");
			} else {
				toast.error(data?.message || "连接测试未通过");
			}
		} catch (err) {
			toast.error(err instanceof Error ? err.message : "连接测试失败");
		} finally {
			setTestingId(null);
		}
	};

	return (
		<div
			data-slot="model-management-view"
			className="flex h-full min-h-0 min-w-0 flex-1 flex-col bg-white"
		>
			<div className="flex h-14 shrink-0 items-center justify-between border-b border-slate-200 px-6">
				<h2 className="text-lg font-semibold text-slate-900">模型管理</h2>
				<div className="flex items-center gap-3">
					<Button size="sm" onClick={() => setCreateOpen(true)}>
						<Plus className="mr-1 size-4" />
						新建模型
					</Button>
				</div>
			</div>

			<ScrollArea className="min-h-0 flex-1">
				{loading && !loaded ? (
					<div className="flex items-center justify-center py-20 text-sm text-slate-500">
						<Loader2 className="mr-2 size-4 animate-spin" />
						加载模型列表…
					</div>
				) : models.length === 0 ? (
					<div className="flex flex-col items-center justify-center py-24 text-center">
						<div className="flex size-24 items-center justify-center rounded-2xl border border-dashed border-slate-300 text-slate-400">
							<Bot className="size-10" strokeWidth={1.5} />
						</div>
						<p className="mt-6 text-xl font-semibold text-slate-900">暂无模型</p>
						<p className="mt-3 text-sm leading-6 text-slate-400">
							创建你的第一个模型，为组织的智能助手提供推理能力。
						</p>
					</div>
				) : (
					<div className="p-6">
						<Table>
							<TableHeader>
								<TableRow>
									<TableHead>名称</TableHead>
									<TableHead>供应商</TableHead>
									<TableHead>Model</TableHead>
									<TableHead>Base URL</TableHead>
									<TableHead>用途</TableHead>
									<TableHead>状态</TableHead>
									<TableHead>默认</TableHead>
									<TableHead className="text-right">操作</TableHead>
								</TableRow>
							</TableHeader>
							<TableBody>
								{models.map((item) => (
									<TableRow key={item.id}>
										<TableCell className="font-medium text-slate-900">
											{item.name || item.model}
											{item.isSystem ? (
												<span className="ml-2 inline-flex items-center rounded-full bg-slate-100 px-2 py-0.5 text-xs font-medium text-slate-600">
													系统内置
												</span>
											) : null}
										</TableCell>
										<TableCell>
											{PROVIDER_LABELS[item.provider] ?? "自定义 (OpenAI 兼容)"}
										</TableCell>
										<TableCell className="truncate">{item.model}</TableCell>
										<TableCell className="max-w-[240px] truncate text-slate-500">
											{item.baseUrl}
										</TableCell>
										<TableCell>
											{item.purpose === "translation" ? (
												<span className="inline-flex items-center rounded-full bg-amber-50 px-2 py-0.5 text-xs font-medium text-amber-700">
													翻译
												</span>
											) : (
												<span className="inline-flex items-center rounded-full bg-slate-100 px-2 py-0.5 text-xs font-medium text-slate-600">
													对话
												</span>
											)}
										</TableCell>
										<TableCell>
											<span
												className={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ${
													item.status === "active"
														? "bg-emerald-50 text-emerald-700"
														: "bg-slate-100 text-slate-600"
												}`}
											>
												{item.status === "active"
													? "启用"
													: item.status === "inactive"
														? "禁用"
														: item.status}
											</span>
										</TableCell>
										<TableCell>
											{item.isDefault ? (
												<span className="inline-flex items-center rounded-full bg-indigo-50 px-2 py-0.5 text-xs font-medium text-indigo-600">
													默认
												</span>
											) : (
												<span className="text-xs text-slate-400">—</span>
											)}
										</TableCell>
										<TableCell className="text-right">
											<DropdownMenu>
												<DropdownMenuTrigger
													render={
														<Button variant="ghost" size="sm" className="h-8 w-8">
															<MoreHorizontal className="size-4" />
														</Button>
													}
												/>
												<DropdownMenuContent align="end">
													<DropdownMenuItem
														onClick={() => void handleTestModel(item)}
														disabled={testingId === item.id}
													>
														{testingId === item.id ? (
															<Loader2 className="size-4 animate-spin" />
														) : (
															<Wifi className="size-4" />
														)}
														测试连接
													</DropdownMenuItem>
													{!item.isDefault && item.status === "active" ? (
														<DropdownMenuItem onClick={() => void handleSetDefault(item)}>
															设为默认
														</DropdownMenuItem>
													) : null}
													{item.status === "active" ? (
														<DropdownMenuItem
															onClick={() => void handleSetStatus(item, "inactive")}
														>
															禁用
														</DropdownMenuItem>
													) : (
														<DropdownMenuItem onClick={() => void handleSetStatus(item, "active")}>
															启用
														</DropdownMenuItem>
													)}
													{item.status === "inactive" ? (
														<DropdownMenuItem onClick={() => setEditTarget(item)}>
															编辑
														</DropdownMenuItem>
													) : null}
													<DropdownMenuSeparator />
													{!item.isSystem && item.status === "inactive" ? (
														<DropdownMenuItem
															variant="destructive"
															onClick={() => setDeleteTarget(item)}
														>
															<Trash2 className="size-4" />
															删除
														</DropdownMenuItem>
													) : null}
												</DropdownMenuContent>
											</DropdownMenu>
										</TableCell>
									</TableRow>
								))}
							</TableBody>
						</Table>
					</div>
				)}
			</ScrollArea>

			<ModelFormDialog open={createOpen} onOpenChange={setCreateOpen} />
			<ModelFormDialog
				open={!!editTarget}
				model={editTarget}
				onOpenChange={(open) => {
					if (!open) setEditTarget(null);
				}}
			/>
			<Dialog
				open={!!deleteTarget}
				onOpenChange={(open) => {
					if (!open && !deleting) setDeleteTarget(null);
				}}
			>
				<DialogContent className="sm:max-w-md" showCloseButton={false}>
					<DialogHeader>
						<DialogTitle>删除模型</DialogTitle>
						<DialogDescription>
							确定要删除「{deleteTarget?.name || deleteTarget?.model}」吗？该操作不可撤销。
						</DialogDescription>
					</DialogHeader>
					<DialogFooter className="mt-4">
						<Button variant="outline" onClick={() => setDeleteTarget(null)} disabled={deleting}>
							取消
						</Button>
						<Button variant="destructive" onClick={() => void handleDelete()} disabled={deleting}>
							{deleting ? <Loader2 className="animate-spin" /> : null}
							确认删除
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		</div>
	);
}
