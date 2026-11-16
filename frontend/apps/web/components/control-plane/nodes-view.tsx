"use client";

import { type ClaimCode, type NodeHealth, type NodeView, useControlApi } from "@lework/control-ui";
import { Badge } from "@lework/ui/components/ui/badge";
import { Button } from "@lework/ui/components/ui/button";
import { cn } from "@lework/ui/lib/utils";
import {
	Check,
	Copy,
	Loader2,
	Play,
	Plus,
	RefreshCw,
	RotateCcw,
	Server,
	Square,
	Trash2,
} from "lucide-react";
import { useCallback, useEffect, useState } from "react";

type Action = "start" | "restart" | "stop";

function shortTime(value?: string): string {
	if (!value) return "未知";
	const date = new Date(value);
	if (Number.isNaN(date.getTime())) return "未知";
	return date.toLocaleString();
}

/**
 * NodesView 是 PC 的节点管理页：查看在线状态、启停 OpenCode、添加节点、移除节点。
 *
 * 「添加节点」走控制面的短期认领码：网页出码，机器执行 `lework agent --code`，
 * 不需要在网页侧直接连节点，也不要求节点开入站端口。
 */
export function NodesView() {
	const { client } = useControlApi();
	const [nodes, setNodes] = useState<NodeView[]>([]);
	const [health, setHealth] = useState<Record<string, NodeHealth | null>>({});
	const [loading, setLoading] = useState(true);
	const [error, setError] = useState<string | null>(null);
	const [busy, setBusy] = useState<string | null>(null);
	const [claim, setClaim] = useState<ClaimCode | null>(null);
	const [claiming, setClaiming] = useState(false);
	const [copied, setCopied] = useState(false);
	const [confirmDelete, setConfirmDelete] = useState<string | null>(null);

	const load = useCallback(async () => {
		try {
			const list = await client.listNodes();
			setNodes(list);
			setError(null);
			const connected = list.filter((node) => node.connected);
			const results = await Promise.allSettled(
				connected.map((node) => client.getNodeHealth(node.id)),
			);
			const next: Record<string, NodeHealth | null> = {};
			connected.forEach((node, index) => {
				const result = results[index];
				next[node.id] = result && result.status === "fulfilled" ? result.value : null;
			});
			setHealth(next);
		} catch (caught) {
			setError(caught instanceof Error ? caught.message : "无法加载节点");
		} finally {
			setLoading(false);
		}
	}, [client]);

	useEffect(() => {
		void load();
	}, [load]);

	useEffect(() => {
		const timer = setInterval(() => void load(), 15_000);
		return () => clearInterval(timer);
	}, [load]);

	const runLifecycle = async (node: NodeView, action: Action) => {
		setBusy(`${node.id}:${action}`);
		setError(null);
		try {
			const call =
				action === "start"
					? client.startOpenCode
					: action === "restart"
						? client.restartOpenCode
						: client.stopOpenCode;
			await call(node.id);
			await load();
		} catch (caught) {
			setError(caught instanceof Error ? caught.message : `无法${action} OpenCode`);
		} finally {
			setBusy(null);
		}
	};

	const removeNode = async (node: NodeView) => {
		setBusy(`${node.id}:delete`);
		setError(null);
		try {
			await client.deleteNode(node.id);
			setConfirmDelete(null);
			await load();
		} catch (caught) {
			setError(caught instanceof Error ? caught.message : "无法移除节点");
		} finally {
			setBusy(null);
		}
	};

	const createClaim = async () => {
		setClaiming(true);
		setError(null);
		setCopied(false);
		try {
			setClaim(await client.createClaim());
		} catch (caught) {
			setError(caught instanceof Error ? caught.message : "无法生成认领码");
		} finally {
			setClaiming(false);
		}
	};

	const copyClaim = async () => {
		if (!claim) return;
		try {
			await navigator.clipboard.writeText(`lework agent --code ${claim.code}`);
			setCopied(true);
		} catch {
			// 剪贴板不可用时保留可见文本，用户仍可手抄。
		}
	};

	return (
		<div className="flex h-full min-h-0 flex-col overflow-y-auto bg-[var(--lework-app-bg)]">
			<header className="flex shrink-0 items-start justify-between gap-4 border-b border-[var(--lework-control-border)] bg-white px-6 py-5">
				<div>
					<div className="flex items-center gap-2">
						<Server className="size-5 text-[var(--lework-primary)]" />
						<h1 className="text-lg font-semibold text-[var(--lework-text-strong)]">节点</h1>
					</div>
					<p className="mt-1 text-sm text-[var(--lework-text-muted)]">
						节点主动出站连接控制面，不需要开放端口；在这里启停 OpenCode 或添加新机器。
					</p>
				</div>
				<div className="flex shrink-0 gap-2">
					<Button variant="outline" size="sm" onClick={() => void load()} disabled={loading}>
						<RefreshCw className={cn("size-3.5", loading && "animate-spin")} /> 刷新
					</Button>
					<Button size="sm" onClick={() => void createClaim()} disabled={claiming}>
						{claiming ? <Loader2 className="size-3.5 animate-spin" /> : <Plus className="size-4" />}
						添加节点
					</Button>
				</div>
			</header>

			{error ? (
				<p className="border-b border-red-200 bg-red-50 px-6 py-2 text-sm text-red-700">{error}</p>
			) : null}

			<div className="min-h-0 flex-1 space-y-6 p-6">
				{claim ? (
					<section className="rounded-2xl border border-[var(--lework-primary-soft)] bg-white p-5 shadow-sm">
						<div className="flex items-start justify-between gap-4">
							<div>
								<h2 className="text-sm font-semibold text-[var(--lework-text-strong)]">
									在这台机器上执行
								</h2>
								<p className="mt-1 text-xs text-[var(--lework-text-muted)]">
									代码 {claim.expires_at ? `在 ${shortTime(claim.expires_at)} 前有效` : "短期有效"}
									； 节点连上后会自动出现在列表里。
								</p>
							</div>
							<Button variant="ghost" size="sm" onClick={() => setClaim(null)}>
								关闭
							</Button>
						</div>
						<div className="mt-3 flex items-center gap-2 rounded-xl bg-[var(--lework-surface-soft)] px-3 py-2.5">
							<code className="min-w-0 flex-1 overflow-x-auto font-mono text-sm text-[var(--lework-text)]">
								lework agent --code {claim.code}
							</code>
							<Button size="sm" variant="outline" onClick={() => void copyClaim()}>
								{copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
								{copied ? "已复制" : "复制"}
							</Button>
						</div>
					</section>
				) : null}

				{loading && nodes.length === 0 ? (
					<div className="flex items-center gap-2 text-sm text-[var(--lework-text-muted)]">
						<Loader2 className="size-4 animate-spin" /> 正在加载节点…
					</div>
				) : nodes.length === 0 ? (
					<div className="mx-auto max-w-lg rounded-2xl border border-dashed border-[var(--lework-control-border)] bg-white p-8 text-center">
						<Server className="mx-auto size-8 text-[var(--lework-text-subtle)]" />
						<h2 className="mt-3 text-sm font-medium text-[var(--lework-text-strong)]">
							还没有节点
						</h2>
						<p className="mt-1 text-sm text-[var(--lework-text-muted)]">
							点击「添加节点」生成认领码，然后在目标机器上执行命令。
						</p>
					</div>
				) : (
					<div className="grid gap-4 xl:grid-cols-2">
						{nodes.map((node) => {
							const state = health[node.id];
							const lifecycleBusy = busy?.startsWith(`${node.id}:`);
							return (
								<article
									key={node.id}
									className="rounded-2xl border border-[var(--lework-control-border)] bg-white p-4 shadow-sm"
								>
									<div className="flex items-start justify-between gap-3">
										<div className="min-w-0">
											<div className="flex items-center gap-2">
												<span
													className={cn(
														"size-2 rounded-full",
														node.connected ? "bg-emerald-500" : "bg-slate-400",
													)}
												/>
												<h2 className="truncate text-sm font-semibold text-[var(--lework-text-strong)]">
													{node.name}
												</h2>
												<Badge variant={node.connected ? "secondary" : "outline"}>
													{node.connected ? "在线" : "离线"}
												</Badge>
											</div>
											<p className="mt-1 text-xs text-[var(--lework-text-muted)]">
												{node.os}/{node.arch} · v{node.agent_version} · 最后在线{" "}
												{shortTime(node.last_seen)}
											</p>
										</div>
										<Badge variant={state?.running ? "secondary" : "outline"}>
											{state ? (state.running ? "OpenCode 运行中" : "OpenCode 已停止") : "状态未知"}
										</Badge>
									</div>

									{node.workspace_roots?.length ? (
										<div className="mt-3 flex flex-wrap gap-1.5">
											{node.workspace_roots.map((root) => (
												<code
													key={root}
													className="max-w-full truncate rounded bg-[var(--lework-surface-soft)] px-1.5 py-0.5 text-[11px] text-[var(--lework-text-muted)]"
												>
													{root}
												</code>
											))}
										</div>
									) : null}

									<div className="mt-4 flex flex-wrap items-center gap-2">
										<Button
											size="sm"
											variant="outline"
											disabled={!node.connected || lifecycleBusy || state?.running === true}
											onClick={() => void runLifecycle(node, "start")}
										>
											{busy === `${node.id}:start` ? (
												<Loader2 className="size-3.5 animate-spin" />
											) : (
												<Play className="size-3.5" />
											)}
											启动
										</Button>
										<Button
											size="sm"
											variant="outline"
											disabled={!node.connected || lifecycleBusy}
											onClick={() => void runLifecycle(node, "restart")}
										>
											{busy === `${node.id}:restart` ? (
												<Loader2 className="size-3.5 animate-spin" />
											) : (
												<RotateCcw className="size-3.5" />
											)}
											重启
										</Button>
										<Button
											size="sm"
											variant="outline"
											disabled={!node.connected || lifecycleBusy || state?.running === false}
											onClick={() => void runLifecycle(node, "stop")}
										>
											{busy === `${node.id}:stop` ? (
												<Loader2 className="size-3.5 animate-spin" />
											) : (
												<Square className="size-3.5" />
											)}
											停止
										</Button>
										<div className="ml-auto">
											{confirmDelete === node.id ? (
												<div className="flex items-center gap-1">
													<Button
														size="sm"
														variant="destructive"
														disabled={busy === `${node.id}:delete`}
														onClick={() => void removeNode(node)}
													>
														{busy === `${node.id}:delete` ? (
															<Loader2 className="size-3.5 animate-spin" />
														) : (
															<Check className="size-3.5" />
														)}
														确认移除
													</Button>
													<Button size="sm" variant="ghost" onClick={() => setConfirmDelete(null)}>
														取消
													</Button>
												</div>
											) : (
												<Button
													size="sm"
													variant="ghost"
													className="text-red-600"
													onClick={() => setConfirmDelete(node.id)}
												>
													<Trash2 className="size-3.5" /> 移除
												</Button>
											)}
										</div>
									</div>
								</article>
							);
						})}
					</div>
				)}
			</div>
		</div>
	);
}
