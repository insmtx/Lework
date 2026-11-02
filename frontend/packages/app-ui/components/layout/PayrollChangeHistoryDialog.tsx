"use client";

import { Button } from "@lework/ui/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@lework/ui/components/ui/dialog";
import { useEffect, useState } from "react";
import type { PayrollChangeEvent } from "./payroll-service-client";

export function PayrollChangeHistoryDialog({
	open,
	title,
	load,
	onOpenChange,
}: {
	open: boolean;
	title: string;
	load: () => Promise<{ events: PayrollChangeEvent[] }>;
	onOpenChange: (open: boolean) => void;
}) {
	const [events, setEvents] = useState<PayrollChangeEvent[]>([]);
	const [loading, setLoading] = useState(false);
	const [error, setError] = useState("");
	useEffect(() => {
		if (!open) return;
		setLoading(true);
		setError("");
		void load()
			.then((result) => setEvents(result.events))
			.catch((e) => setError(e instanceof Error ? e.message : "加载变更记录失败"))
			.finally(() => setLoading(false));
	}, [open]);
	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="max-h-[80vh] overflow-y-auto sm:max-w-2xl">
				<DialogHeader>
					<DialogTitle>{title} · 变更记录</DialogTitle>
				</DialogHeader>
				{loading ? (
					<p className="py-8 text-center text-sm text-slate-400">加载中...</p>
				) : error ? (
					<p role="alert" className="py-8 text-center text-sm text-red-600">
						{error}
					</p>
				) : events.length === 0 ? (
					<p className="py-8 text-center text-sm text-slate-400">暂无变更记录</p>
				) : (
					<div className="grid gap-4 py-2">
						{events.map((event) => (
							<div key={event.id} className="border-l-2 border-slate-200 pl-4">
								<div className="text-xs text-slate-400">
									{new Date(event.changed_at).toLocaleString("zh-CN")} · V{event.version}
								</div>
								<div className="mt-1 flex items-center gap-2 text-sm font-medium">
									<span>{event.summary}</span>
									<span className="text-xs font-normal text-slate-500">
										来源：{event.source_type === "IMPORT" ? "Excel 导入" : "页面维护"}
									</span>
								</div>
								{event.changes.length ? (
									<div className="mt-2 grid gap-2">
										{event.changes.map((change) => (
											<div key={change.field} className="text-sm">
												<span className="mr-2 text-slate-500">{change.field_name}</span>
												<span>
													{change.before || "无"} → {change.after || "无"}
												</span>
											</div>
										))}
									</div>
								) : null}
								{event.snapshot ? (
									<details className="mt-2 text-sm">
										<summary className="cursor-pointer text-blue-600">
											查看{event.operation === "DELETE" ? "删除前" : "初始"}数据
										</summary>
										<div className="mt-2 grid gap-1 rounded bg-slate-50 p-3 text-xs">
											{Object.entries(event.snapshot)
												.filter(([key]) => key !== "items")
												.map(([key, value]) => (
													<div key={key}>
														<span className="mr-2 text-slate-500">{key}</span>
														{String(value ?? "")}
													</div>
												))}
											{Array.isArray(event.snapshot.items) ? (
												<div className="mt-2 border-t pt-2">
													工资项目：
													{event.snapshot.items
														.map((item) => {
															const value = item as { name?: string; amount?: string };
															return `${value.name || "工资项目"} ${value.amount || ""}`;
														})
														.join("；")}
												</div>
											) : null}
										</div>
									</details>
								) : null}
							</div>
						))}
					</div>
				)}
				<div className="flex justify-end">
					<Button variant="outline" onClick={() => onOpenChange(false)}>
						关闭
					</Button>
				</div>
			</DialogContent>
		</Dialog>
	);
}
