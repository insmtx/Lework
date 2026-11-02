"use client";

import { Button } from "@lework/ui/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@lework/ui/components/ui/dialog";
import { FolderOpen, Upload } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { PayrollChangeHistoryDialog } from "./PayrollChangeHistoryDialog";
import { Paging } from "./PayrollStoredPeopleTable";
import type { PayrollStoredPerson } from "./payroll-accounting-mock";
import {
	deletePayrollSalary,
	importPayrollSalary,
	listPayrollPeople,
	listPayrollSalary,
	listPayrollSalaryChangeHistory,
	type PayrollSalaryRecord,
	updatePayrollSalary,
} from "./payroll-service-client";

const formatMoney = (value: string) => {
	const amount = Number(value);
	return Number.isFinite(amount)
		? new Intl.NumberFormat("zh-CN", { style: "currency", currency: "CNY" }).format(amount)
		: value;
};

export function PayrollHistoricalSalaryTable({
	refreshToken = 0,
	onInitializedChange,
	onPickProject,
}: {
	refreshToken?: number;
	onInitializedChange?: (value: boolean) => void;
	onPickProject?: () => void;
}) {
	const [rows, setRows] = useState<PayrollSalaryRecord[]>([]);
	const [total, setTotal] = useState(0);
	const [page, setPage] = useState(1);
	const [pageSize, setPageSize] = useState(10);
	const [month, setMonth] = useState("");
	const [name, setName] = useState("");
	const [loading, setLoading] = useState(true);
	const [error, setError] = useState("");
	const [busy, setBusy] = useState(false);
	const [editing, setEditing] = useState<PayrollSalaryRecord | null>(null);
	const [draft, setDraft] = useState<PayrollSalaryRecord | null>(null);
	const [historyRow, setHistoryRow] = useState<PayrollSalaryRecord | null>(null);
	const [rosterSearch, setRosterSearch] = useState("");
	const [people, setPeople] = useState<PayrollStoredPerson[]>([]);
	const fileInput = useRef<HTMLInputElement>(null);
	const refresh = useCallback(async () => {
		setLoading(true);
		try {
			const result = await listPayrollSalary({
				offset: (page - 1) * pageSize,
				limit: pageSize,
				payrollMonth: month,
				name,
			});
			if (page > 1 && result.total <= (page - 1) * pageSize) {
				setPage(Math.max(1, Math.ceil(result.total / pageSize)));
				return;
			}
			setRows(result.items);
			setTotal(result.total);
			setError("");
			onInitializedChange?.(result.initialized);
		} catch {
			setError("暂时无法读取历史工资，请稍后重试。");
		} finally {
			setLoading(false);
		}
	}, [page, pageSize, month, name, onInitializedChange, refreshToken]);
	useEffect(() => {
		void refresh();
	}, [refresh]);
	useEffect(() => {
		if (!editing) return;
		let active = true;
		void listPayrollPeople({ limit: 50, name: rosterSearch })
			.then((result) => {
				if (active) setPeople(result.items);
			})
			.catch(() => {
				if (active) toast.error("暂时无法读取人员资料，请稍后重试。");
			});
		return () => {
			active = false;
		};
	}, [editing, rosterSearch]);
	const importFile = async (file?: File) => {
		if (!file) return;
		setBusy(true);
		try {
			const r = await importPayrollSalary(file);
			toast.success(
				`${file.name}：导入 ${r.success_count}/${r.total_count} 条，失败 ${r.failed_count} 条${r.message ? `；${r.message}` : ""}`,
			);
			await refresh();
		} catch {
			toast.error("历史工资导入失败，请检查文件内容后重试。");
		} finally {
			setBusy(false);
		}
	};
	const save = async () => {
		if (!draft) return;
		setBusy(true);
		try {
			await updatePayrollSalary(draft);
			setEditing(null);
			toast.success("历史工资已更新");
			await refresh();
		} catch {
			toast.error("历史工资资料保存失败，请检查内容后重试。");
		} finally {
			setBusy(false);
		}
	};
	const remove = async (row: PayrollSalaryRecord) => {
		if (!window.confirm(`确定删除“${row.name} ${row.payroll_month}”的历史工资记录吗？`)) return;
		setBusy(true);
		try {
			await deletePayrollSalary(row.id);
			toast.success("历史工资已删除");
			if (page > 1 && rows.length === 1) setPage(page - 1);
			else await refresh();
		} catch {
			toast.error("历史工资资料删除失败，请稍后重试。");
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
				<h2 className="text-sm font-semibold text-slate-800">
					历史工资表{busy ? " · 处理中" : ""}
				</h2>
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
						aria-label="上传历史工资文件"
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
			<div className="mb-3 grid gap-2 sm:grid-cols-2">
				<input
					aria-label="筛选工资月份"
					className="h-8 rounded-md border px-2 text-sm"
					type="month"
					value={month}
					onChange={(e) => {
						setMonth(e.target.value);
						setPage(1);
					}}
				/>
				<input
					aria-label="筛选历史工资姓名"
					className="h-8 rounded-md border px-2 text-sm"
					placeholder="姓名"
					value={name}
					onChange={(e) => {
						setName(e.target.value);
						setPage(1);
					}}
				/>
			</div>
			{loading ? (
				<p className="py-8 text-center text-sm text-slate-400">加载中...</p>
			) : error ? (
				<p role="alert" className="py-8 text-center text-sm text-red-600">
					{error}
				</p>
			) : (
				<div className="overflow-x-auto rounded-lg border border-slate-100">
					<table className="w-full min-w-[760px] text-left text-sm">
						<thead className="text-xs text-slate-500">
							<tr>
								{["工资月份", "姓名", "岗位", "工作天数", "应发工资", "操作"].map((v) => (
									<th key={v} className="border-b px-3 py-2.5 font-medium">
										{v}
									</th>
								))}
							</tr>
						</thead>
						<tbody>
							{rows.length ? (
								rows.map((row) => (
									<tr key={row.id} className="border-b border-slate-50">
										<td className="px-3 py-2">{row.payroll_month}</td>
										<td className="px-3 py-2">{row.name}</td>
										<td className="px-3 py-2">{row.position}</td>
										<td className="px-3 py-2">{row.work_days}</td>
										<td className="px-3 py-2">{formatMoney(row.gross_amount)}</td>
										<td className="whitespace-nowrap px-3 py-2">
											<Button
												size="sm"
												variant="ghost"
												onClick={() => {
													setEditing(row);
													setDraft({ ...row });
													setRosterSearch(row.name);
												}}
											>
												编辑
											</Button>
											<Button size="sm" variant="ghost" onClick={() => setHistoryRow(row)}>
												变更记录
											</Button>
											<Button
												size="sm"
												variant="ghost"
												disabled={busy}
												onClick={() => void remove(row)}
											>
												删除
											</Button>
										</td>
									</tr>
								))
							) : (
								<tr>
									<td colSpan={6} className="px-3 py-8 text-center text-slate-400">
										没有历史工资记录
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
						<DialogTitle>编辑历史工资</DialogTitle>
					</DialogHeader>
					{draft && (
						<div className="grid gap-3 py-2">
							<label className="grid gap-1 text-sm">
								工资月份
								<input
									className="h-9 rounded-md border px-3"
									type="month"
									value={draft.payroll_month.slice(0, 7)}
									onChange={(e) => setDraft({ ...draft, payroll_month: e.target.value })}
								/>
							</label>
							<label className="grid gap-1 text-sm">
								人员搜索
								<input
									className="h-9 rounded-md border px-3"
									placeholder="输入姓名查询 Payroll 人员"
									value={rosterSearch}
									onChange={(e) => setRosterSearch(e.target.value)}
								/>
							</label>
							<label className="grid gap-1 text-sm">
								人员
								<select
									className="h-9 rounded-md border bg-white px-3"
									value={draft.roster_id}
									onChange={(e) => {
										const person = people.find((p) => p.id === Number(e.target.value));
										if (person)
											setDraft({
												...draft,
												roster_id: person.id,
												name: person.name,
												position: person.position,
											});
									}}
								>
									<option value={draft.roster_id}>{draft.name}（当前）</option>
									{people
										.filter((p) => p.id !== draft.roster_id)
										.map((p) => (
											<option key={p.id} value={p.id}>
												{p.name} · {p.position} · {p.projectName}
											</option>
										))}
								</select>
							</label>
							<label className="grid gap-1 text-sm">
								岗位
								<input
									className="h-9 rounded-md border px-3"
									value={draft.position}
									onChange={(e) => setDraft({ ...draft, position: e.target.value })}
								/>
							</label>
							<label className="grid gap-1 text-sm">
								工作天数
								<input
									className="h-9 rounded-md border px-3"
									inputMode="decimal"
									value={draft.work_days}
									onChange={(e) => setDraft({ ...draft, work_days: e.target.value })}
								/>
							</label>
							<label className="grid gap-1 text-sm">
								应发工资
								<input
									className="h-9 rounded-md border px-3"
									inputMode="decimal"
									value={draft.gross_amount}
									onChange={(e) => setDraft({ ...draft, gross_amount: e.target.value })}
								/>
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
				open={Boolean(historyRow)}
				title={historyRow ? `${historyRow.name} ${historyRow.payroll_month}` : "历史工资"}
				onOpenChange={(open) => {
					if (!open) setHistoryRow(null);
				}}
				load={() => listPayrollSalaryChangeHistory(historyRow?.id || 0)}
			/>
		</section>
	);
}
