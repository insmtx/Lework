"use client";

import { type OverviewNode, useControlApi } from "@lework/control-ui";
import { Badge } from "@lework/ui/components/ui/badge";
import { Button } from "@lework/ui/components/ui/button";
import { cn } from "@lework/ui/lib/utils";
import { CheckCircle2, Loader2, RefreshCw, ShieldAlert } from "lucide-react";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useState } from "react";

/**
 * PendingView 聚合所有节点/会话的待审批项。
 *
 * 控制面的审批仍属于具体会话；这里只做跨会话索引，点击后回到
 * ControlConsole 的对应会话处理，避免在第二个地方维护审批状态。
 */
export function PendingView() {
	const router = useRouter();
	const { client } = useControlApi();
	const [nodes, setNodes] = useState<OverviewNode[]>([]);
	const [loading, setLoading] = useState(true);
	const [error, setError] = useState<string | null>(null);

	const load = useCallback(async () => {
		try {
			setNodes(await client.getOverview());
			setError(null);
		} catch (caught) {
			setError(caught instanceof Error ? caught.message : "无法加载待审批");
		} finally {
			setLoading(false);
		}
	}, [client]);

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
					.filter((session) => session.pending_permissions > 0)
					.map((session) => ({ node, session })),
			),
		[nodes],
	);

	const total = pending.reduce((sum, item) => sum + item.session.pending_permissions, 0);

	return (
		<div className="flex h-full min-h-0 flex-col overflow-y-auto bg-[var(--lework-app-bg)]">
			<header className="flex shrink-0 items-start justify-between gap-4 border-b border-[var(--lework-control-border)] bg-white px-6 py-5">
				<div>
					<div className="flex items-center gap-2">
						<ShieldAlert className="size-5 text-[var(--lework-primary)]" />
						<h1 className="text-lg font-semibold text-[var(--lework-text-strong)]">审批</h1>
					</div>
					<p className="mt-1 text-sm text-[var(--lework-text-muted)]">
						跨节点、跨会话的待处理审批；点击进入对应会话处理。
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

			<div className="min-h-0 flex-1 p-6">
				{loading && pending.length === 0 ? (
					<div className="flex items-center gap-2 text-sm text-[var(--lework-text-muted)]">
						<Loader2 className="size-4 animate-spin" /> 正在加载审批…
					</div>
				) : pending.length === 0 ? (
					<div className="mx-auto max-w-lg rounded-2xl border border-dashed border-[var(--lework-control-border)] bg-white p-8 text-center">
						<CheckCircle2 className="mx-auto size-8 text-emerald-500" />
						<h2 className="mt-3 text-sm font-medium text-[var(--lework-text-strong)]">
							没有待审批
						</h2>
						<p className="mt-1 text-sm text-[var(--lework-text-muted)]">
							节点上的 OpenCode 需要授权时会自动出现在这里。
						</p>
					</div>
				) : (
					<div className="space-y-3">
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
									{session.pending_permissions} 条待审批
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
					</div>
				)}
			</div>
		</div>
	);
}
