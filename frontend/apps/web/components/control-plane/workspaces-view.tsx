"use client";

import { type NodeView, type ProjectSummary, useControlApi } from "@lework/control-ui";
import { Badge } from "@lework/ui/components/ui/badge";
import { Button } from "@lework/ui/components/ui/button";
import { cn } from "@lework/ui/lib/utils";
import { CloudOff, FolderGit2, Loader2, RefreshCw, Server } from "lucide-react";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useState } from "react";

function baseName(path: string): string {
	const parts = path.split("/").filter(Boolean);
	return parts[parts.length - 1] ?? path;
}

function workspaceName(project: ProjectSummary): string {
	return project.alias?.trim() || baseName(project.canonical) || project.id;
}

function nodeName(nodes: NodeView[], nodeID: string): string {
	return nodes.find((node) => node.id === nodeID)?.name ?? nodeID.slice(0, 8);
}

/**
 * WorkspacesView 是 PC 的 OpenCode 工作区入口。
 *
 * 「项目」在这里就是 OpenCode workspace：展示 canonical 路径、出现过的节点
 * 和主节点。进入详情复用 ControlConsole，因此会话/审批/Diff/文件/终端不需要
 * 在 PC 侧再实现一套。
 */
export function WorkspacesView() {
	const router = useRouter();
	const { client } = useControlApi();
	const [projects, setProjects] = useState<ProjectSummary[]>([]);
	const [nodes, setNodes] = useState<NodeView[]>([]);
	const [loading, setLoading] = useState(true);
	const [error, setError] = useState<string | null>(null);
	const [savingKey, setSavingKey] = useState<string | null>(null);

	const load = useCallback(async () => {
		try {
			const [projectList, nodeList] = await Promise.all([
				client.listProjects(),
				client.listNodes(),
			]);
			setProjects(projectList);
			setNodes(nodeList);
			setError(null);
		} catch (caught) {
			setError(caught instanceof Error ? caught.message : "无法加载工作区");
		} finally {
			setLoading(false);
		}
	}, [client]);

	useEffect(() => {
		void load();
	}, [load]);

	useEffect(() => {
		const timer = setInterval(() => void load(), 20_000);
		return () => clearInterval(timer);
	}, [load]);

	const onlineCount = useMemo(() => nodes.filter((node) => node.connected).length, [nodes]);

	const setPrimaryNode = async (project: ProjectSummary, nodeID: string) => {
		setSavingKey(project.key);
		setError(null);
		try {
			await client.setProjectPrimaryNode(project.key, nodeID);
			setProjects((current) =>
				current.map((item) =>
					item.key === project.key ? { ...item, primary_node_id: nodeID } : item,
				),
			);
		} catch (caught) {
			setError(caught instanceof Error ? caught.message : "无法设置主节点");
		} finally {
			setSavingKey(null);
		}
	};

	return (
		<div className="flex h-full min-h-0 flex-col overflow-y-auto bg-[var(--lework-app-bg)]">
			<header className="flex shrink-0 items-start justify-between gap-4 border-b border-[var(--lework-control-border)] bg-white px-6 py-5">
				<div>
					<div className="flex items-center gap-2">
						<FolderGit2 className="size-5 text-[var(--lework-primary)]" />
						<h1 className="text-lg font-semibold text-[var(--lework-text-strong)]">工作区</h1>
					</div>
					<p className="mt-1 text-sm text-[var(--lework-text-muted)]">
						OpenCode workspace 一览；进入后用会话、Diff、文件与终端处理实际工作。
					</p>
					<p className="mt-1 text-xs text-[var(--lework-text-subtle)]">
						{projects.length} 个工作区 · {onlineCount}/{nodes.length} 个节点在线
					</p>
				</div>
				<Button variant="outline" size="sm" onClick={() => void load()} disabled={loading}>
					<RefreshCw className={cn("size-3.5", loading && "animate-spin")} /> 刷新
				</Button>
			</header>

			{error ? (
				<p className="border-b border-red-200 bg-red-50 px-6 py-2 text-sm text-red-700">{error}</p>
			) : null}

			<div className="min-h-0 flex-1 p-6">
				{loading && projects.length === 0 ? (
					<div className="flex items-center gap-2 text-sm text-[var(--lework-text-muted)]">
						<Loader2 className="size-4 animate-spin" /> 正在加载工作区…
					</div>
				) : projects.length === 0 ? (
					<div className="mx-auto max-w-lg rounded-2xl border border-dashed border-[var(--lework-control-border)] bg-white p-8 text-center">
						<CloudOff className="mx-auto size-8 text-[var(--lework-text-subtle)]" />
						<h2 className="mt-3 text-sm font-medium text-[var(--lework-text-strong)]">
							还没有工作区
						</h2>
						<p className="mt-1 text-sm text-[var(--lework-text-muted)]">
							先启动 <code className="rounded bg-slate-100 px-1">lework agent</code>，并在节点上打开
							过至少一个 Git 仓库；OpenCode 上报后工作区会出现在这里。
						</p>
						<Button className="mt-4" variant="outline" onClick={() => router.push("/nodes")}>
							<Server className="size-4" /> 管理节点
						</Button>
					</div>
				) : (
					<div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
						{projects.map((project) => {
							const primary =
								project.primary_node_id ??
								project.nodes.find((node) => node.online)?.node_id ??
								project.nodes[0]?.node_id ??
								"";
							const onlineNodes = project.nodes.filter((node) => node.online);
							return (
								<article
									key={project.key}
									className="flex flex-col gap-3 rounded-2xl border border-[var(--lework-control-border)] bg-white p-4 shadow-sm"
								>
									<div className="flex items-start justify-between gap-3">
										<div className="min-w-0">
											<h2 className="truncate text-sm font-semibold text-[var(--lework-text-strong)]">
												{workspaceName(project)}
											</h2>
											<code className="mt-1 block truncate text-xs text-[var(--lework-text-muted)]">
												{project.canonical}
											</code>
										</div>
										{project.vcs ? (
											<Badge variant="secondary" className="shrink-0">
												{project.vcs}
											</Badge>
										) : null}
									</div>

									<div className="flex flex-wrap items-center gap-1.5">
										{project.nodes.length === 0 ? (
											<Badge variant="outline">暂无节点</Badge>
										) : (
											project.nodes.map((node) => (
												<Badge
													key={node.node_id}
													variant={node.online ? "secondary" : "outline"}
													title={node.online ? "在线" : `最后在线 ${node.last_seen}`}
												>
													<span
														className={cn(
															"mr-1 inline-block size-1.5 rounded-full",
															node.online ? "bg-emerald-500" : "bg-slate-400",
														)}
													/>
													{nodeName(nodes, node.node_id)}
												</Badge>
											))
										)}
									</div>

									<div className="mt-auto flex items-center gap-2">
										<select
											className="h-8 min-w-0 flex-1 rounded-md border border-[var(--lework-control-border)] bg-white px-2 text-xs text-[var(--lework-text)]"
											value={primary}
											disabled={savingKey === project.key || project.nodes.length === 0}
											onChange={(event) => void setPrimaryNode(project, event.target.value)}
											aria-label={`${workspaceName(project)} 的主节点`}
										>
											{project.nodes.length === 0 ? <option value="">无节点</option> : null}
											{project.nodes.map((node) => (
												<option key={node.node_id} value={node.node_id}>
													{nodeName(nodes, node.node_id)}
													{node.online ? "" : "（离线）"}
												</option>
											))}
										</select>
										<Button
											size="sm"
											onClick={() => router.push(`/workspaces/${encodeURIComponent(project.key)}`)}
											disabled={onlineNodes.length === 0}
										>
											打开
										</Button>
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
