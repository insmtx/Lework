import { type PluginListItem, pluginApi } from "@leros/store";
import { afterEach, describe, expect, it, vi } from "vitest";
import { loadComposerConnectorOptions } from "./useComposerConnectorOptions";

function mcpPlugin(code: string, name: string): PluginListItem {
	return {
		public_id: `connector_${code}`,
		code,
		kind: "mcp",
		name,
		description: `${name} description`,
		status: "active",
		origin: "org",
		current_revision: 1,
	};
}

const ORG_LIST_RESPONSE = {
	data: { code: 0, data: { plugins: [mcpPlugin("github", "GitHub")] } },
} as Awaited<ReturnType<typeof pluginApi.list>>;

const EMPTY_PROJECT_RESPONSE = {
	data: { code: 0, data: [] },
} as unknown as Awaited<ReturnType<typeof pluginApi.listProject>>;

afterEach(() => {
	vi.restoreAllMocks();
});

describe("loadComposerConnectorOptions", () => {
	it("加载组织 active MCP 连接器并映射为候选", async () => {
		vi.spyOn(pluginApi, "list").mockResolvedValue(ORG_LIST_RESPONSE);
		vi.spyOn(pluginApi, "listProject").mockResolvedValue(EMPTY_PROJECT_RESPONSE);

		const result = await loadComposerConnectorOptions({ projectId: "prj_1" });

		expect(result.error).toBeNull();
		expect(result.options).toHaveLength(1);
		expect(result.options[0]?.pluginId).toBe("connector_github");
		expect(result.options[0]?.label).toBe("GitHub");
		expect(result.options[0]?.source).toBe("organization");
		expect(result.options[0]?.projectAssociated).toBe(false);
	});

	it("把项目已绑定连接器标记为已关联", async () => {
		vi.spyOn(pluginApi, "list").mockResolvedValue(ORG_LIST_RESPONSE);
		vi.spyOn(pluginApi, "listProject").mockResolvedValue({
			data: { code: 0, data: [mcpPlugin("github", "GitHub")] },
		} as unknown as Awaited<ReturnType<typeof pluginApi.listProject>>);

		const result = await loadComposerConnectorOptions({ projectId: "prj_1" });

		expect(result.options[0]?.projectAssociated).toBe(true);
	});

	it("项目绑定加载失败时仍展示组织连接器并报告错误", async () => {
		vi.spyOn(pluginApi, "list").mockResolvedValue(ORG_LIST_RESPONSE);
		vi.spyOn(pluginApi, "listProject").mockRejectedValue(new Error("project unavailable"));

		const result = await loadComposerConnectorOptions({ projectId: "prj_1" });

		expect(result.options).toHaveLength(1);
		expect(result.error).toBe("项目绑定加载失败");
	});

	it("无项目上下文时不请求项目绑定", async () => {
		const list = vi.spyOn(pluginApi, "list").mockResolvedValue(ORG_LIST_RESPONSE);
		const listProject = vi.spyOn(pluginApi, "listProject");

		const result = await loadComposerConnectorOptions({ projectId: null });

		expect(list).toHaveBeenCalledWith({ kind: "mcp", status: "active", limit: 100 });
		expect(listProject).not.toHaveBeenCalled();
		expect(result.options).toHaveLength(1);
		expect(result.error).toBeNull();
	});

	it("组织加载失败时报错且返回空候选", async () => {
		vi.spyOn(pluginApi, "list").mockRejectedValue(new Error("org unavailable"));
		vi.spyOn(pluginApi, "listProject").mockResolvedValue(EMPTY_PROJECT_RESPONSE);

		const result = await loadComposerConnectorOptions({ projectId: "prj_1" });

		expect(result.options).toHaveLength(0);
		expect(result.error).toBe("连接器加载失败");
	});
});
