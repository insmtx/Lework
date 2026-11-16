"use client";

import { ControlConsole, type ProjectSummary, useControlApi } from "@lework/control-ui";
import { Button } from "@lework/ui/components/ui/button";
import { CloudOff, Loader2 } from "lucide-react";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";

/**
 * WorkspaceConsole 把某个 OpenCode workspace 交给共享控制台。
 *
 * 会话目录固定为 workspace 的 canonical 路径，节点优先用用户设置的主节点，
 * 否则回落到第一个在线节点；其余行为完全复用 ControlConsole。
 */
export function WorkspaceConsole({
	projectKey,
	initialSessionID,
}: {
	projectKey: string;
	initialSessionID?: string | null;
}) {
	const router = useRouter();
	const { client } = useControlApi();
	const [project, setProject] = useState<ProjectSummary | null>(null);
	const [loading, setLoading] = useState(true);
	const [error, setError] = useState<string | null>(null);

	const load = useCallback(async () => {
		try {
			const projects = await client.listProjects();
			const found = projects.find((item) => item.key === projectKey) ?? null;
			setProject(found);
			setError(found ? null : "没有找到这个工作区，它可能已从节点上移除。");
		} catch (caught) {
			setError(caught instanceof Error ? caught.message : "无法加载工作区");
		} finally {
			setLoading(false);
		}
	}, [client, projectKey]);

	useEffect(() => {
		void load();
	}, [load]);

	if (loading) {
		return (
			<div className="flex h-full items-center justify-center gap-2 bg-[var(--lework-app-bg)] text-sm text-[var(--lework-text-muted)]">
				<Loader2 className="size-4 animate-spin" /> 正在打开工作区…
			</div>
		);
	}

	if (!project) {
		return (
			<div className="flex h-full flex-col items-center justify-center gap-3 bg-[var(--lework-app-bg)] p-6 text-center">
				<CloudOff className="size-8 text-[var(--lework-text-subtle)]" />
				<p className="text-sm text-[var(--lework-text-muted)]">{error ?? "工作区不存在"}</p>
				<Button variant="outline" onClick={() => router.push("/workspaces")}>
					返回工作区列表
				</Button>
			</div>
		);
	}

	const nodeID =
		project.primary_node_id ??
		project.nodes.find((node) => node.online)?.node_id ??
		project.nodes[0]?.node_id ??
		null;

	return (
		<ControlConsole
			initialNodeID={nodeID}
			initialDirectory={project.canonical}
			initialSessionID={initialSessionID}
		/>
	);
}
