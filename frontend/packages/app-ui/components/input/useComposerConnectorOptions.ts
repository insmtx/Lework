"use client";

import {
	type PluginComposerOption,
	type PluginListItem,
	pluginApi,
	pluginToComposerOption,
} from "@leros/store";
import { useCallback, useEffect, useState } from "react";

const CONNECTOR_PICKER_LIMIT = 100;

type UseComposerConnectorOptionsParams = {
	projectId?: string | null;
	enabled?: boolean;
};

export type ComposerConnectorOptions = {
	connectorOptions: PluginComposerOption[] | undefined;
	connectorsLoading: boolean;
	connectorError: string | null;
	reloadConnectorOptions: () => Promise<void>;
};

type LoadConnectorOptionsResult = {
	options: PluginComposerOption[];
	error: string | null;
};

/**
 * 加载「添加连接器」候选的真实数据源：当前组织 active 的 MCP 连接器，
 * 并在有项目上下文时同时读取项目已绑定 MCP 连接器（标记「已关联」）。
 * 导出的纯函数便于独立测试与潜在复用。
 */
export async function loadComposerConnectorOptions({
	projectId,
}: {
	projectId?: string | null;
}): Promise<LoadConnectorOptionsResult> {
	const [orgResult, projectResult] = await Promise.allSettled([
		pluginApi
			.list({ kind: "mcp", status: "active", limit: CONNECTOR_PICKER_LIMIT })
			.then((response) =>
				response.data.code === 0 ? (response.data.data.plugins ?? []) : ([] as PluginListItem[]),
			),
		projectId
			? pluginApi
					.listProject({ public_id: projectId, kind: "mcp" })
					.then((response) =>
						response.data.code === 0 ? (response.data.data ?? []) : ([] as PluginListItem[]),
					)
			: Promise.resolve([] as PluginListItem[]),
	]);

	const orgPlugins = orgResult.status === "fulfilled" ? orgResult.value : ([] as PluginListItem[]);
	const projectPlugins =
		projectResult.status === "fulfilled" ? projectResult.value : ([] as PluginListItem[]);

	const projectBoundIds = new Set<string>();
	for (const plugin of projectPlugins) {
		projectBoundIds.add(plugin.public_id);
	}

	const options = orgPlugins.map((plugin) => {
		const option = pluginToComposerOption(plugin, "organization");
		return {
			...option,
			projectAssociated: projectBoundIds.has(plugin.public_id),
		};
	});

	let error: string | null = null;
	if (orgResult.status === "rejected") {
		error = "连接器加载失败";
	} else if (projectId && projectResult.status === "rejected") {
		error = "项目绑定加载失败";
	}
	return { options, error };
}

/**
 * 输入框「添加连接器」候选 Hook。包装 loadComposerConnectorOptions，
 * 暴露加载状态与重新加载能力，供 ChatInput / WorkbenchPanel 消费。
 */
export function useComposerConnectorOptions({
	projectId,
	enabled = true,
}: UseComposerConnectorOptionsParams): ComposerConnectorOptions {
	const [connectorOptions, setConnectorOptions] = useState<PluginComposerOption[] | undefined>(
		undefined,
	);
	const [connectorsLoading, setConnectorsLoading] = useState(enabled);
	const [connectorError, setConnectorError] = useState<string | null>(null);

	const loadConnectorOptions = useCallback(async () => {
		if (!enabled) {
			setConnectorOptions(undefined);
			setConnectorsLoading(false);
			return;
		}
		setConnectorsLoading(true);
		let result: LoadConnectorOptionsResult;
		try {
			result = await loadComposerConnectorOptions({ projectId });
		} catch {
			result = { options: [], error: "连接器加载失败" };
		}
		setConnectorOptions(result.options);
		setConnectorError(result.error);
		setConnectorsLoading(false);
	}, [enabled, projectId]);

	const reloadConnectorOptions = useCallback(() => loadConnectorOptions(), [loadConnectorOptions]);

	useEffect(() => {
		void loadConnectorOptions();
	}, [loadConnectorOptions]);

	return { connectorOptions, connectorsLoading, connectorError, reloadConnectorOptions };
}
