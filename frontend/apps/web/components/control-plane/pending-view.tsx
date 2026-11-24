"use client";

import { FormCard, type OverviewNode, type PendingForm, useControlApi } from "@lework/control-ui";
import { Badge } from "@lework/ui/components/ui/badge";
import { Button } from "@lework/ui/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogHeader,
	DialogTitle,
} from "@lework/ui/components/ui/dialog";
import { cn } from "@lework/ui/lib/utils";
import { CheckCircle2, Loader2, RefreshCw, ShieldAlert } from "lucide-react";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useState } from "react";

interface GlobalItem {
	node: OverviewNode;
	form: PendingForm;
}

/**
 * PendingView 聚合所有节点/会话的待审批项与位置级表单。
 *
 * 控制面的审批仍属于具体会话；这里只做跨会话索引，点击后回到
 * ControlConsole 的对应会话处理，避免在第二个地方维护审批状态。
 */
export function PendingView() {
	const router = useRouter();
	const { client } = useControlApi();
	const [nodes, setNodes] = useState<OverviewNode[]>([]);
	const [globalItems, setGlobalItems] = useState<GlobalItem[]>([]);
	const [active, setActive] = useState<GlobalItem | null>(null);
	const [dialogBusy, setDialogBusy] = useState(false);
	const [dialogError, setDialogError] = useState<string | null>(null);
	const [loading, setLoading] = useState(true);
	const [error, setError] = useState<string | null>(null);

	// 位置级表单只有节点接口能列，逐个在线节点查询；旧节点失败直接跳过。
	const loadGlobal = useCallback(
		async (current: OverviewNode[]) => {
			const items: GlobalItem[] = [];
			await Promise.all(
				current
					.filter((node) => node.connected)
					.map(async (node) => {
						try {
							const forms = await client.listNodeForms(node.node_id);
							for (const form of forms) items.push({ node, form });
						} catch {
							// 旧控制面/旧节点没有 form.* 时整段降级为空。
						}
					}),
			);
			setGlobalItems(items);
		},
		[client],
	);

	const load = useCallback(async () => {
		try {
			const next = await client.getOverview();
			setNodes(next);
			await loadGlobal(next);
			setError(null);
		} catch (caught) {
			setError(caught instanceof Error ? caught.message : "无法加载待处理");
		} finally {
			setLoading(false);
		}
	}, [client, loadGlobal]);

	useEffect(() => {
		void load();
	}, [load]);

	useEffect(() => {
		const timer = setInterval(() => void load(), 10_000);
		return () => clearInterval(timer);
	}, [load]);

	const pending = useMemo(
		() =>
			nodes.flatMap((node) =>
				node.sessions
					.filter((session) => sessionTotal(session) > 0)
					.map((session) => ({ node, session })),
			),
		[nodes],
	);

	const total = pending.reduce((sum, item) => sum + sessionTotal(item.session), 0);

	const handleGlobal = async (run: (item: GlobalItem) => Promise<unknown>) => {
		if (!active) return;
		setDialogBusy(true);
		setDialogError(null);
		try {
			await run(active);
			setActive(null);
			await load();
		} catch (caught) {
			setDialogError(caught instanceof Error ? caught.message : "处理失败");
		} finally {
			setDialogBusy(false);
		}
	};

	return (
		<div className="flex h-full min-h-0 flex-col overflow-y-auto bg-[var(--lework-app-bg)]">
			<header className="flex shrink-0 items-start justify-between gap-4 border-b border-[var(--lework-control-border)] bg-white px-6 py-5">
				<div>
					<div className="flex items-center gap-2">
						<ShieldAlert className="size-5 text-[var(--lework-primary)]" />
						<h1 className="text-lg font-semibold text-[var(--lework-text-strong)]">待处理</h1>
					</div>
					<p className="mt-1 text-sm text-[var(--lework-text-muted)]">
						跨节点、跨会话的待审批与待回答表单；点击进入对应会话处理。
					</p>
					<p className="mt-1 text-xs text-[var(--lework-text-subtle)]">{total} 条待处理</p>
				</div>
				<Button variant="outline" size="sm" onClick={() => void load()} disabled={loading}>
					<RefreshCw className={cn("size-3.5", loading && "animate-spin")} /> 刷新
				</Button>
			</header>

			{error ? (
				<p className="border-b border-red-200 bg-red-50 px-6 py-2 text-sm text-red-700">{error}</p>
			) : null}

			<div className="min-h-0 flex-1 space-y-3 p-6">
				{loading && pending.length === 0 && globalItems.length === 0 ? (
					<div className="flex items-center gap-2 text-sm text-[var(--lework-text-muted)]">
						<Loader2 className="size-4 animate-spin" /> 正在加载待处理…
					</div>
				) : pending.length === 0 && globalItems.length === 0 ? (
					<div className="mx-auto max-w-lg rounded-2xl border border-dashed border-[var(--lework-control-border)] bg-white p-8 text-center">
						<CheckCircle2 className="mx-auto size-8 text-emerald-500" />
						<h2 className="mt-3 text-sm font-medium text-[var(--lework-text-strong)]">
							没有待处理
						</h2>
						<p className="mt-1 text-sm text-[var(--lework-text-muted)]">
							需要你确认或回答的内容会出现在这里。
						</p>
					</div>
				) : (
					<>
						{pending.map(({ node, session }) => (
							<article
								key={`${node.node_id}:${session.session_id}`}
								className="flex flex-wrap items-center gap-3 rounded-2xl border border-[var(--lework-control-border)] bg-white px-4 py-3 shadow-sm"
							>
								<div className="min-w-0 flex-1">
									<div className="flex min-w-0 items-center gap-2">
										<Badge variant={node.connected ? "secondary" : "outline"}>
											{node.connected ? node.name : `${node.name}（离线）`}
										</Badge>
										<span className="truncate text-sm font-medium text-[var(--lework-text-strong)]">
											{session.title || session.session_id}
										</span>
									</div>
									<code className="mt-1 block truncate font-mono text-[11px] text-[var(--lework-text-muted)]">
										{session.session_id}
									</code>
								</div>
								<Badge className="shrink-0 bg-amber-100 text-amber-900">
									{sessionTotal(session)} 条待处理
								</Badge>
								<Button
									size="sm"
									onClick={() =>
										router.push(
											`/workspaces/console?node=${encodeURIComponent(node.node_id)}&session=${encodeURIComponent(session.session_id)}`,
										)
									}
								>
									处理
								</Button>
							</article>
						))}

						{globalItems.length > 0 ? (
							<section className="space-y-3">
								<h2 className="text-sm font-medium text-[var(--lework-text-strong)]">全局表单</h2>
								{globalItems.map(({ node, form }) => (
									<article
										key={`${node.node_id}:${form.id}`}
										className="flex flex-wrap items-center gap-3 rounded-2xl border border-[var(--lework-control-border)] bg-white px-4 py-3 shadow-sm"
									>
										<div className="min-w-0 flex-1">
											<div className="flex min-w-0 items-center gap-2">
												<Badge variant="secondary">{node.name}</Badge>
												<span className="truncate text-sm font-medium text-[var(--lework-text-strong)]">
													{form.title || form.id}
												</span>
											</div>
											{form.directory ? (
												<code className="mt-1 block truncate font-mono text-[11px] text-[var(--lework-text-muted)]">
													{form.directory}
												</code>
											) : null}
										</div>
										<Badge className="shrink-0 bg-amber-100 text-amber-900">待回答</Badge>
										<Button
											size="sm"
											onClick={() => {
												setDialogError(null);
												setActive({ node, form });
											}}
										>
											处理
										</Button>
									</article>
								))}
							</section>
						) : null}
					</>
				)}
			</div>

			<Dialog
				open={active !== null}
				onOpenChange={(open) => {
					if (!open) setActive(null);
				}}
			>
				<DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-2xl">
					<DialogHeader>
						<DialogTitle>全局表单</DialogTitle>
						<DialogDescription>{active?.node.name}</DialogDescription>
					</DialogHeader>
					{dialogError ? <p className="text-xs text-red-600">{dialogError}</p> : null}
					{active ? (
						<FormCard
							form={active.form}
							disabled={dialogBusy}
							onSubmit={async (answer) => {
								await handleGlobal((item) =>
									client.replyNodeForm(
										item.node.node_id,
										item.form.id,
										item.form.directory ?? "",
										answer,
									),
								);
							}}
							onCancel={async () => {
								await handleGlobal((item) =>
									client.cancelNodeForm(item.node.node_id, item.form.id, item.form.directory ?? ""),
								);
							}}
						/>
					) : null}
				</DialogContent>
			</Dialog>
		</div>
	);
}

function sessionTotal(session: OverviewNode["sessions"][number]): number {
	return session.pending_permissions + (session.pending_forms ?? 0);
}
