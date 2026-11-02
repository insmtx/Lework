"use client";

import { Button } from "@lework/ui/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@lework/ui/components/ui/dialog";
import {
	Pagination,
	PaginationContent,
	PaginationEllipsis,
	PaginationItem,
} from "@lework/ui/components/ui/pagination";
import { ChevronLeft, ChevronRight, FolderOpen, Upload } from "lucide-react";
import { Fragment, useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { PayrollChangeHistoryDialog } from "./PayrollChangeHistoryDialog";
import {
	PAYROLL_EMPLOYEE_TYPES,
	type PayrollEmployeeType,
	type PayrollStoredPerson,
} from "./payroll-accounting-mock";
import {
	deletePayrollPerson,
	importPayrollRoster,
	listPayrollPeople,
	listPayrollRosterChangeHistory,
	updatePayrollPerson,
} from "./payroll-service-client";

const GENDERS = ["未知", "男", "女"] as const;
const SIZES = [10, 20, 50];

export function PayrollStoredPeopleTable({
	onInitializedChange,
	refreshToken = 0,
	onPickProject,
}: {
	onInitializedChange?: (initialized: boolean) => void;
	refreshToken?: number;
	onPickProject?: () => void;
}) {
	const [rows, setRows] = useState<PayrollStoredPerson[]>([]);
	const [loading, setLoading] = useState(true);
	const [loadError, setLoadError] = useState("");
	const [busy, setBusy] = useState(false);
	const [total, setTotal] = useState(0);
	const [page, setPage] = useState(1);
	const [pageSize, setPageSize] = useState(10);
	const [name, setName] = useState("");
	const [projectName, setProjectName] = useState("");
	const [companyName, setCompanyName] = useState("");
	const [editing, setEditing] = useState<PayrollStoredPerson | null>(null);
	const [draft, setDraft] = useState<PayrollStoredPerson | null>(null);
	const [historyPerson, setHistoryPerson] = useState<PayrollStoredPerson | null>(null);
	const fileInput = useRef<HTMLInputElement>(null);

	const refresh = useCallback(async () => {
		setLoading(true);
		try {
			const result = await listPayrollPeople({
				offset: (page - 1) * pageSize,
				limit: pageSize,
				name,
				projectName,
				companyName,
			});
			if (page > 1 && result.total <= (page - 1) * pageSize) {
				setPage(Math.max(1, Math.ceil(result.total / pageSize)));
				return;
			}
			setRows(result.items);
			setTotal(result.total);
			setLoadError("");
			onInitializedChange?.(result.initialized);
		} catch {
			setLoadError("暂时无法读取人员底表，请稍后重试。");
		} finally {
			setLoading(false);
		}
	}, [page, pageSize, name, projectName, companyName, onInitializedChange, refreshToken]);

	useEffect(() => {
		void refresh();
	}, [refresh]);
	const setFilter = (setter: (value: string) => void, value: string) => {
		setter(value);
		setPage(1);
	};
	const importFile = async (file?: File) => {
		if (!file) return;
		setBusy(true);
		try {
			const result = await importPayrollRoster(file);
			toast.success(
				`${file.name}：导入 ${result.success_count}/${result.total_count} 条，失败 ${result.failed_count} 条`,
			);
			await refresh();
		} catch {
			toast.error("人员底表导入失败，请检查文件内容后重试。");
		} finally {
			setBusy(false);
		}
	};
	const save = async () => {
		if (!draft) return;
		setBusy(true);
		try {
			await updatePayrollPerson(draft);
			setEditing(null);
			toast.success("人员已更新");
			await refresh();
		} catch {
			toast.error("人员资料保存失败，请检查内容后重试。");
		} finally {
			setBusy(false);
		}
	};
	const remove = async (person: PayrollStoredPerson) => {
		if (!window.confirm(`确定删除人员“${person.name}”吗？`)) return;
		setBusy(true);
		try {
			await deletePayrollPerson(person.id);
			toast.success("人员已删除");
			if (page > 1 && rows.length === 1) setPage(page - 1);
			else await refresh();
		} catch {
			toast.error("人员资料删除失败，请稍后重试。");
		} finally {
			setBusy(false);
		}
	};
	const pages = Math.ceil(total / pageSize);
	const pageNumbers = Array.from(
		new Set(
			[
				1,
				...Array.from({ length: pages }, (_, i) => i + 1).filter(
					(n) => n >= page - 2 && n <= page + 2,
				),
				pages,
			].filter((n) => n > 0 && n <= pages),
		),
	).sort((a, b) => a - b);

	return (
		<section className="min-w-0 rounded-xl border border-slate-200 bg-white p-4">
			<div className="mb-3 flex flex-wrap items-center justify-between gap-3">
				<h2 className="text-sm font-semibold text-slate-800">人员底表{busy ? " · 处理中" : ""}</h2>
				<div className="flex gap-2">
					<Button
						size="sm"
						variant="outline"
						disabled={busy}
						onClick={() => fileInput.current?.click()}
					>
						<Upload className="size-4" />
						上传文件
					</Button>
					{onPickProject ? (
						<Button size="sm" variant="outline" disabled={busy} onClick={onPickProject}>
							<FolderOpen className="size-4" />
							项目文件
						</Button>
					) : null}
					<input
						ref={fileInput}
						aria-label="上传人员底表文件"
						className="hidden"
						type="file"
						accept=".xlsx"
						onChange={(e) => {
							void importFile(e.target.files?.[0]);
							e.target.value = "";
						}}
					/>
				</div>
			</div>
			<div className="mb-3 grid gap-2 sm:grid-cols-3">
				<input
					aria-label="筛选人员名称"
					className="h-8 rounded-md border px-2 text-sm"
					placeholder="姓名"
					value={name}
					onChange={(e) => setFilter(setName, e.target.value)}
				/>
				<input
					aria-label="筛选项目"
					className="h-8 rounded-md border px-2 text-sm"
					placeholder="项目"
					value={projectName}
					onChange={(e) => setFilter(setProjectName, e.target.value)}
				/>
				<input
					aria-label="筛选公司或部门"
					className="h-8 rounded-md border px-2 text-sm"
					placeholder="公司/部门"
					value={companyName}
					onChange={(e) => setFilter(setCompanyName, e.target.value)}
				/>
			</div>
			{loading ? (
				<p className="py-8 text-center text-sm text-slate-400">加载中...</p>
			) : loadError ? (
				<p role="alert" className="py-8 text-center text-sm text-red-600">
					{loadError}
				</p>
			) : (
				<div className="overflow-x-auto rounded-lg border border-slate-100">
					<table className="w-full min-w-[780px] text-left text-sm">
						<thead className="text-xs text-slate-500">
							<tr>
								{["姓名", "项目", "公司/部门", "岗位", "人员类别", "性别", "操作"].map((v) => (
									<th key={v} className="border-b px-3 py-2.5 font-medium">
										{v}
									</th>
								))}
							</tr>
						</thead>
						<tbody>
							{rows.length ? (
								rows.map((person) => (
									<tr key={person.id} className="border-b border-slate-50">
										<td className="px-3 py-2">{person.name}</td>
										<td className="px-3 py-2">{person.projectName}</td>
										<td className="px-3 py-2">{person.companyName}</td>
										<td className="px-3 py-2">{person.position}</td>
										<td className="px-3 py-2">{person.employeeType}</td>
										<td className="px-3 py-2">{person.gender}</td>
										<td className="whitespace-nowrap px-3 py-2">
											<Button
												size="sm"
												variant="ghost"
												onClick={() => {
													setEditing(person);
													setDraft({ ...person });
												}}
											>
												编辑
											</Button>
											<Button size="sm" variant="ghost" onClick={() => setHistoryPerson(person)}>
												变更记录
											</Button>
											<Button
												size="sm"
												variant="ghost"
												disabled={busy}
												onClick={() => void remove(person)}
											>
												删除
											</Button>
										</td>
									</tr>
								))
							) : (
								<tr>
									<td colSpan={7} className="px-3 py-8 text-center text-slate-400">
										没有人员底表记录
									</td>
								</tr>
							)}
						</tbody>
					</table>
				</div>
			)}
			<Paging
				total={total}
				page={page}
				pages={pages}
				pageSize={pageSize}
				loading={loading}
				setPage={setPage}
				setPageSize={(size) => {
					setPageSize(size);
					setPage(1);
				}}
				pageNumbers={pageNumbers}
			/>
			<Dialog
				open={Boolean(editing)}
				onOpenChange={(open) => {
					if (!open) {
						setEditing(null);
						setDraft(null);
					}
				}}
			>
				<DialogContent>
					<DialogHeader>
						<DialogTitle>编辑人员</DialogTitle>
					</DialogHeader>
					{draft && (
						<div className="grid gap-3 py-2">
							{(
								[
									["name", "姓名"],
									["companyName", "公司/部门"],
									["projectName", "项目"],
									["position", "岗位"],
								] as const
							).map(([key, label]) => (
								<label key={key} className="grid gap-1 text-sm">
									{label}
									<input
										className="h-9 rounded-md border px-3"
										value={draft[key]}
										onChange={(e) => setDraft({ ...draft, [key]: e.target.value })}
									/>
								</label>
							))}
							<label className="grid gap-1 text-sm">
								人员类别
								<select
									className="h-9 rounded-md border bg-white px-3"
									value={draft.employeeType}
									onChange={(e) =>
										setDraft({ ...draft, employeeType: e.target.value as PayrollEmployeeType })
									}
								>
									{PAYROLL_EMPLOYEE_TYPES.map((v) => (
										<option key={v}>{v}</option>
									))}
								</select>
							</label>
							<label className="grid gap-1 text-sm">
								性别
								<select
									className="h-9 rounded-md border bg-white px-3"
									value={draft.gender}
									onChange={(e) =>
										setDraft({ ...draft, gender: e.target.value as PayrollStoredPerson["gender"] })
									}
								>
									{GENDERS.map((v) => (
										<option key={v}>{v}</option>
									))}
								</select>
							</label>
						</div>
					)}
					<DialogFooter>
						<Button variant="outline" onClick={() => setEditing(null)}>
							取消
						</Button>
						<Button disabled={busy} onClick={() => void save()}>
							保存
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
			<PayrollChangeHistoryDialog
				open={Boolean(historyPerson)}
				title={historyPerson?.name || "人员"}
				onOpenChange={(open) => {
					if (!open) setHistoryPerson(null);
				}}
				load={() => listPayrollRosterChangeHistory(historyPerson?.id || 0)}
			/>
		</section>
	);
}

export function Paging({
	total,
	page,
	pages,
	pageSize,
	loading,
	setPage,
	setPageSize,
	pageNumbers,
}: {
	total: number;
	page: number;
	pages: number;
	pageSize: number;
	loading: boolean;
	setPage: (n: number) => void;
	setPageSize: (n: number) => void;
	pageNumbers: number[];
}) {
	return (
		<div className="mt-3 flex flex-wrap items-center justify-between gap-3 text-sm text-slate-500">
			<span>共 {total} 条</span>
			<div className="flex items-center gap-1">
				<Pagination className="mx-0 w-auto">
					<PaginationContent>
						<PaginationItem>
							<Button
								aria-label="上一页"
								variant="ghost"
								size="icon-sm"
								disabled={loading || page <= 1}
								onClick={() => setPage(page - 1)}
							>
								<ChevronLeft />
							</Button>
						</PaginationItem>
						{pageNumbers.map((n, i) => (
							<Fragment key={n}>
								{i > 0 && n - (pageNumbers[i - 1] ?? n) > 1 ? (
									<PaginationItem>
										<PaginationEllipsis />
									</PaginationItem>
								) : null}
								<PaginationItem>
									<Button
										size="icon-sm"
										variant={page === n ? "outline" : "ghost"}
										aria-current={page === n ? "page" : undefined}
										disabled={loading}
										onClick={() => setPage(n)}
									>
										{n}
									</Button>
								</PaginationItem>
							</Fragment>
						))}
						<PaginationItem>
							<Button
								aria-label="下一页"
								variant="ghost"
								size="icon-sm"
								disabled={loading || page >= pages}
								onClick={() => setPage(page + 1)}
							>
								<ChevronRight />
							</Button>
						</PaginationItem>
					</PaginationContent>
				</Pagination>
				<select
					aria-label="每页数量"
					className="ml-2 h-8 rounded-md border bg-white px-2"
					value={pageSize}
					onChange={(e) => setPageSize(Number(e.target.value))}
				>
					{SIZES.map((size) => (
						<option key={size} value={size}>
							{size} 条/页
						</option>
					))}
				</select>
			</div>
		</div>
	);
}
