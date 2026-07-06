"use client";

import { type Department, orgAdminApi, useAuthStore } from "@leros/store";
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
import { cn } from "@leros/ui/lib/utils";
import {
	ChevronDown,
	ChevronRight,
	Loader2,
	MoreHorizontal,
	Plus,
	Search,
	Trash2,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import {
	buildDepartmentTree,
	countDepartments,
	type DepartmentTreeNode,
	filterDepartmentTree,
} from "./departmentTree";

type DialogMode =
	| { type: "create"; parentId: number; parentName: string }
	| { type: "rename"; department: Department }
	| { type: "delete"; department: Department }
	| null;

function DepartmentTreeItem({
	node,
	level,
	selectedId,
	onSelect,
	onOpenMenu,
}: {
	node: DepartmentTreeNode;
	level: number;
	selectedId: number | null;
	onSelect: (id: number) => void;
	onOpenMenu: (department: Department) => void;
}) {
	const [expanded, setExpanded] = useState(true);
	const hasChildren = node.children.length > 0;

	return (
		<div>
			<div
				className={cn(
					"group flex items-center gap-1 rounded-lg pr-1",
					selectedId === node.id && "bg-[var(--leros-primary-softer)]",
				)}
				style={{ paddingLeft: `${level * 16 + 8}px` }}
			>
				<button
					type="button"
					className="flex size-6 shrink-0 items-center justify-center rounded-md text-[var(--leros-text-subtle)] hover:bg-slate-100"
					onClick={() => setExpanded((value) => !value)}
					aria-label={expanded ? "收起" : "展开"}
				>
					{hasChildren ? (
						expanded ? (
							<ChevronDown className="size-3.5" />
						) : (
							<ChevronRight className="size-3.5" />
						)
					) : (
						<span className="size-3.5" />
					)}
				</button>
				<button
					type="button"
					className="min-w-0 flex-1 truncate py-2 text-left text-sm text-[var(--leros-text)]"
					onClick={() => onSelect(node.id)}
				>
					{node.name}
				</button>
				<button
					type="button"
					className="rounded-md p-1 opacity-0 transition-opacity hover:bg-slate-100 group-hover:opacity-100"
					onClick={() => onOpenMenu(node)}
					aria-label={`管理 ${node.name}`}
				>
					<MoreHorizontal className="size-4 text-[var(--leros-text-subtle)]" />
				</button>
			</div>
			{expanded &&
				node.children.map((child) => (
					<DepartmentTreeItem
						key={child.id}
						node={child}
						level={level + 1}
						selectedId={selectedId}
						onSelect={onSelect}
						onOpenMenu={onOpenMenu}
					/>
				))}
		</div>
	);
}

export function DepartmentTreePanel({ compact = false }: { compact?: boolean }) {
	const user = useAuthStore((s) => s.authUser);
	const orgId = user?.currentOrg?.id;
	const orgName = user?.currentOrg?.name ?? "当前组织";

	const [loading, setLoading] = useState(true);
	const [departments, setDepartments] = useState<Department[]>([]);
	const [search, setSearch] = useState("");
	const [selectedId, setSelectedId] = useState<number | null>(null);
	const [dialogMode, setDialogMode] = useState<DialogMode>(null);
	const [dialogValue, setDialogValue] = useState("");
	const [submitting, setSubmitting] = useState(false);
	const [menuDepartment, setMenuDepartment] = useState<Department | null>(null);

	const loadDepartments = useCallback(async () => {
		if (!orgId) {
			setLoading(false);
			return;
		}
		setLoading(true);
		try {
			const resp = await orgAdminApi.listDepartments({ org_id: orgId, list_all: true });
			setDepartments(resp.data.data.items ?? []);
		} catch (err) {
			const message = err instanceof Error ? err.message : "部门加载失败";
			toast.error(message);
		} finally {
			setLoading(false);
		}
	}, [orgId]);

	useEffect(() => {
		void loadDepartments();
	}, [loadDepartments]);

	const tree = useMemo(() => buildDepartmentTree(departments), [departments]);
	const filteredTree = useMemo(() => filterDepartmentTree(tree, search), [tree, search]);
	const departmentCount = useMemo(() => countDepartments(tree), [tree]);
	const selectedDepartment = departments.find((item) => item.id === selectedId) ?? null;

	const openCreateDialog = (parentId: number, parentName: string) => {
		setDialogMode({ type: "create", parentId, parentName });
		setDialogValue("");
	};

	const openRenameDialog = (department: Department) => {
		setMenuDepartment(null);
		setDialogMode({ type: "rename", department });
		setDialogValue(department.name);
	};

	const openDeleteDialog = (department: Department) => {
		setMenuDepartment(null);
		setDialogMode({ type: "delete", department });
		setDialogValue("");
	};

	const handleDialogConfirm = async () => {
		if (!orgId || !dialogMode) return;
		setSubmitting(true);
		try {
			if (dialogMode.type === "create") {
				const name = dialogValue.trim();
				if (!name) {
					toast.error("部门名称不能为空");
					return;
				}
				await orgAdminApi.createDepartment({
					org_id: orgId,
					name,
					parent_id: dialogMode.parentId,
				});
				toast.success("部门已创建");
			}
			if (dialogMode.type === "rename") {
				const name = dialogValue.trim();
				if (!name) {
					toast.error("部门名称不能为空");
					return;
				}
				await orgAdminApi.updateDepartment({ id: dialogMode.department.id, name });
				toast.success("部门已更新");
			}
			if (dialogMode.type === "delete") {
				await orgAdminApi.deleteDepartment({ id: dialogMode.department.id });
				toast.success("部门已删除");
				if (selectedId === dialogMode.department.id) {
					setSelectedId(null);
				}
			}
			setDialogMode(null);
			await loadDepartments();
		} catch (err) {
			const message = err instanceof Error ? err.message : "操作失败";
			toast.error(message);
		} finally {
			setSubmitting(false);
		}
	};

	if (!user?.currentOrg) {
		return (
			<div className="rounded-2xl border border-[var(--leros-control-border)] bg-[var(--leros-surface,#fff)] p-8 text-sm text-[var(--leros-text-subtle)]">
				请先登录并选择组织后再管理部门。
			</div>
		);
	}

	return (
		<div
			className={cn("flex h-full min-h-0 flex-col", compact ? "min-h-[480px]" : "min-h-[560px]")}
		>
			{!compact && (
				<div className="mb-4 flex shrink-0 items-center justify-between gap-3">
					<h1 className="text-xl font-semibold text-[var(--leros-text-strong)]">部门管理</h1>
				</div>
			)}

			<div className="flex min-h-0 flex-1 overflow-hidden rounded-2xl border border-[var(--leros-control-border)] bg-[var(--leros-surface,#fff)]">
				<aside
					className={cn(
						"flex shrink-0 flex-col border-r border-[var(--leros-control-border)]",
						compact ? "w-[240px]" : "w-[280px]",
					)}
				>
					<div className="border-b border-[var(--leros-control-border)] p-3">
						<div className="relative">
							<Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-[var(--leros-text-subtle)]" />
							<Input
								value={search}
								onChange={(event) => setSearch(event.target.value)}
								placeholder="搜索部门"
								className="pl-9"
							/>
						</div>
					</div>
					<div className="min-h-0 flex-1 overflow-y-auto p-2">
						{loading ? (
							<div className="flex items-center justify-center py-10 text-sm text-[var(--leros-text-subtle)]">
								<Loader2 className="mr-2 size-4 animate-spin" />
								加载中...
							</div>
						) : (
							<>
								<div
									className={cn(
										"mb-1 flex items-center gap-1 rounded-lg px-2 py-2",
										selectedId === null && "bg-[var(--leros-primary-softer)]",
									)}
								>
									<span className="size-6 shrink-0" />
									<button
										type="button"
										className="min-w-0 flex-1 truncate text-left text-sm font-medium text-[var(--leros-text-strong)]"
										onClick={() => setSelectedId(null)}
									>
										{orgName}
									</button>
									<button
										type="button"
										className="rounded-md p-1 hover:bg-slate-100"
										onClick={() => openCreateDialog(0, orgName)}
										aria-label="新建一级部门"
									>
										<Plus className="size-4 text-[var(--leros-text-subtle)]" />
									</button>
								</div>
								{filteredTree.map((node) => (
									<DepartmentTreeItem
										key={node.id}
										node={node}
										level={1}
										selectedId={selectedId}
										onSelect={setSelectedId}
										onOpenMenu={setMenuDepartment}
									/>
								))}
							</>
						)}
					</div>
				</aside>

				<section className="flex min-h-0 min-w-0 flex-1 flex-col p-4 sm:p-6">
					<div className="mb-4 shrink-0 sm:mb-6">
						<h2 className="text-lg font-semibold text-[var(--leros-text-strong)]">
							{selectedDepartment?.name ?? orgName}
						</h2>
						<p className="mt-1 text-sm text-[var(--leros-text-subtle)]">
							当前组织共有 {departmentCount} 个部门
						</p>
					</div>

					{selectedDepartment ? (
						<div className="min-h-0 space-y-4 overflow-y-auto">
							<div className="rounded-xl border border-[var(--leros-control-border)] p-4 text-sm text-[var(--leros-text-muted)]">
								<p>部门 ID：{selectedDepartment.id}</p>
								<p className="mt-1">
									上级部门 ID：{selectedDepartment.parent_id || "无（一级部门）"}
								</p>
							</div>
							<div className="flex flex-wrap gap-2">
								<Button
									type="button"
									variant="outline"
									onClick={() => openCreateDialog(selectedDepartment.id, selectedDepartment.name)}
								>
									<Plus className="size-4" />
									新建子部门
								</Button>
								<Button
									type="button"
									variant="outline"
									onClick={() => openRenameDialog(selectedDepartment)}
								>
									重命名
								</Button>
								<Button
									type="button"
									variant="outline"
									className="text-red-600 hover:text-red-700"
									onClick={() => openDeleteDialog(selectedDepartment)}
								>
									<Trash2 className="size-4" />
									删除部门
								</Button>
							</div>
						</div>
					) : (
						<div className="leros-dept-empty-panel flex min-h-[220px] flex-1 flex-col items-center justify-center rounded-xl border border-dashed border-[var(--leros-control-border)] bg-[var(--leros-surface-soft,#f6f8fc)] px-4 py-10 text-center sm:px-6">
							<p className="text-sm text-[var(--leros-text-muted)]">成员管理功能即将上线</p>
							<p className="mt-2 max-w-sm text-xs leading-relaxed text-[var(--leros-text-subtle)]">
								当前阶段仅支持维护组织下的部门结构，可在左侧创建或管理部门。
							</p>
							<Button type="button" className="mt-6" onClick={() => openCreateDialog(0, orgName)}>
								<Plus className="size-4" />
								新建一级部门
							</Button>
						</div>
					)}
				</section>
			</div>

			{menuDepartment && (
				<div className="fixed inset-0 z-40">
					<button
						type="button"
						className="absolute inset-0 cursor-default bg-transparent"
						aria-label="关闭菜单"
						onClick={() => setMenuDepartment(null)}
					/>
					<div className="absolute left-[calc(280px+1rem)] top-40 z-50 w-44 rounded-xl border border-[var(--leros-control-border)] bg-white p-1 shadow-lg">
						<button
							type="button"
							className="flex w-full items-center rounded-lg px-3 py-2 text-sm hover:bg-slate-100"
							onClick={() => openCreateDialog(menuDepartment.id, menuDepartment.name)}
						>
							新建子部门
						</button>
						<button
							type="button"
							className="flex w-full items-center rounded-lg px-3 py-2 text-sm hover:bg-slate-100"
							onClick={() => openRenameDialog(menuDepartment)}
						>
							重命名
						</button>
						<button
							type="button"
							className="flex w-full items-center rounded-lg px-3 py-2 text-sm text-red-600 hover:bg-red-50"
							onClick={() => openDeleteDialog(menuDepartment)}
						>
							删除
						</button>
					</div>
				</div>
			)}

			<Dialog open={dialogMode !== null} onOpenChange={(open) => !open && setDialogMode(null)}>
				<DialogContent className="sm:max-w-md">
					<DialogHeader>
						<DialogTitle>
							{dialogMode?.type === "create"
								? "新建部门"
								: dialogMode?.type === "rename"
									? "重命名部门"
									: "删除部门"}
						</DialogTitle>
						<DialogDescription>
							{dialogMode?.type === "create"
								? `将在「${dialogMode.parentName}」下创建子部门`
								: dialogMode?.type === "rename"
									? "请输入新的部门名称"
									: `确定删除「${dialogMode?.department.name}」吗？若存在子部门将无法删除。`}
						</DialogDescription>
					</DialogHeader>

					{dialogMode?.type !== "delete" ? (
						<Input
							value={dialogValue}
							onChange={(event) => setDialogValue(event.target.value)}
							placeholder="部门名称"
							autoFocus
						/>
					) : null}

					<DialogFooter>
						<Button
							type="button"
							variant="outline"
							onClick={() => setDialogMode(null)}
							disabled={submitting}
						>
							取消
						</Button>
						<Button
							type="button"
							variant={dialogMode?.type === "delete" ? "destructive" : "default"}
							onClick={() => void handleDialogConfirm()}
							disabled={submitting}
						>
							{submitting ? <Loader2 className="size-4 animate-spin" /> : null}
							{dialogMode?.type === "delete" ? "删除" : "确定"}
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		</div>
	);
}
