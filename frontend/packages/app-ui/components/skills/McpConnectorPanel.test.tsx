import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { McpConnectorPanel } from "./McpConnectorPanel";

const {
	mockPluginAddMCP,
	mockPluginConnectMCPPlatform,
	mockPluginDelete,
	mockPluginGet,
	mockPluginList,
	mockPluginListMCPPlatforms,
	mockPluginGetMCPPlatformOAuthStatus,
	mockPluginStartMCPPlatformOAuth,
	mockPluginTestMCP,
	mockPluginUpdateMCP,
	mockOpenExternalLink,
} = vi.hoisted(() => ({
	mockPluginAddMCP: vi.fn(),
	mockPluginConnectMCPPlatform: vi.fn(),
	mockPluginDelete: vi.fn(),
	mockPluginGet: vi.fn(),
	mockPluginList: vi.fn(),
	mockPluginListMCPPlatforms: vi.fn(),
	mockPluginGetMCPPlatformOAuthStatus: vi.fn(),
	mockPluginStartMCPPlatformOAuth: vi.fn(),
	mockPluginTestMCP: vi.fn(),
	mockPluginUpdateMCP: vi.fn(),
	mockOpenExternalLink: vi.fn(),
}));

vi.mock("@leros/store", () => ({
	pluginApi: {
		addMCP: mockPluginAddMCP,
		connectMCPPlatform: mockPluginConnectMCPPlatform,
		delete: mockPluginDelete,
		get: mockPluginGet,
		list: mockPluginList,
		listMCPPlatforms: mockPluginListMCPPlatforms,
		getMCPPlatformOAuthStatus: mockPluginGetMCPPlatformOAuthStatus,
		startMCPPlatformOAuth: mockPluginStartMCPPlatformOAuth,
		testMCP: mockPluginTestMCP,
		updateMCP: mockPluginUpdateMCP,
	},
}));

vi.mock("../../utils/open-external-link", () => ({
	openExternalLink: mockOpenExternalLink,
}));

describe("McpConnectorPanel", () => {
	afterEach(() => {
		vi.useRealTimers();
		cleanup();
	});

	beforeEach(() => {
		vi.clearAllMocks();
		mockPluginList.mockResolvedValue({
			data: {
				data: {
					plugins: [
						{
							public_id: "plugin_mcp",
							code: "browser",
							kind: "mcp",
							name: "浏览器连接器",
							description: "连接浏览器服务",
							status: "active",
							origin: "manual",
							current_revision: 2,
						},
						{
							public_id: "plugin_mcp_inactive",
							code: "documents",
							kind: "mcp",
							name: "文档连接器",
							description: "连接文档服务",
							status: "inactive",
							origin: "marketplace",
							current_revision: 1,
						},
					],
				},
			},
		});
		mockPluginGet.mockResolvedValue({
			data: {
				data: {
					definition: {
						schema: "mcp/v1",
						transport: "http",
						name: "browser",
						url: "https://example.com/mcp",
						bearer_token: "browser-secret",
						headers: { Authorization: "Bearer token" },
					},
				},
			},
		});
		mockPluginTestMCP.mockResolvedValue({
			data: { data: { ok: true, tool_count: 3 } },
		});
		mockPluginListMCPPlatforms.mockResolvedValue({
			data: {
				data: {
					platforms: [
						{
							code: "corekg",
							name: "CoreKG",
							description: "CoreKG 连接知识库、知识图谱与智能问答能力",
							auto_connect_supported: true,
							connected: true,
							plugin_id: "plugin_corekg",
						},
					],
				},
			},
		});
		mockPluginConnectMCPPlatform.mockResolvedValue({
			data: {
				data: {
					platform: { code: "corekg", connected: true },
					plugin: { public_id: "plugin_corekg" },
					tool_count: 21,
				},
			},
		});
		mockPluginStartMCPPlatformOAuth.mockResolvedValue({
			data: {
				data: {
					attempt_id: "oauth_attempt",
					authorization_url: "https://openapi.baidu.com/oauth/2.0/authorize?state=opaque",
					expires_at: "2026-08-03T02:10:00Z",
				},
			},
		});
		mockPluginGetMCPPlatformOAuthStatus.mockResolvedValue({
			data: {
				data: { attempt_id: "oauth_attempt", status: "active", connected: true },
			},
		});
		mockPluginDelete.mockResolvedValue({ data: { data: { operation: "deleted" } } });
		mockPluginAddMCP.mockResolvedValue({ data: { data: {} } });
		mockPluginUpdateMCP.mockResolvedValue({ data: { data: {} } });
	});

	it("loads and renders organization MCP plugins", async () => {
		render(<McpConnectorPanel />);

		expect(await screen.findByText("浏览器连接器")).toBeInTheDocument();
		expect(screen.getByText("文档连接器")).toBeInTheDocument();
		expect(screen.getByText("平台连接器")).toBeInTheDocument();
		expect(screen.getByText("自定义连接器")).toBeInTheDocument();
		expect(screen.getByText("知识库")).toBeInTheDocument();
		expect(screen.getByText("连接知识库、知识图谱与智能问答能力")).toBeInTheDocument();
		expect(screen.queryByText(/CoreKG/)).not.toBeInTheDocument();
		expect(screen.getByText("已连接")).toBeInTheDocument();
		expect(screen.queryByRole("button", { name: "管理 知识库" })).not.toBeInTheDocument();
		expect(screen.queryByText("自定义 MCP 服务")).not.toBeInTheDocument();
		expect(screen.getByRole("button", { name: "管理 浏览器连接器" })).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "管理 文档连接器" })).toBeInTheDocument();
		expect(screen.getAllByText("工具")).toHaveLength(2);
		expect(screen.queryByText("未连接")).not.toBeInTheDocument();
		await waitFor(() =>
			expect(mockPluginList).toHaveBeenCalledWith({
				kind: "mcp",
				limit: 90,
				status: "active",
			}),
		);
	});

	it("relies on the MCP list API to ensure CoreKG server-side", async () => {
		render(<McpConnectorPanel />);

		expect(await screen.findByText("已连接")).toBeInTheDocument();
		expect(mockPluginConnectMCPPlatform).not.toHaveBeenCalled();
		expect(mockPluginAddMCP).not.toHaveBeenCalled();
	});

	it("does not repeat a connected platform in the MCP connector list", async () => {
		mockPluginList.mockResolvedValueOnce({
			data: {
				data: {
					plugins: [
						{
							public_id: "plugin_mcp",
							code: "browser",
							kind: "mcp",
							name: "浏览器连接器",
							status: "active",
							origin: "manual",
							current_revision: 2,
						},
						{
							public_id: "plugin_corekg",
							code: "corekg-user",
							kind: "mcp",
							name: "CoreKG",
							status: "active",
							origin: "manual",
							current_revision: 1,
						},
					],
				},
			},
		});
		mockPluginListMCPPlatforms.mockResolvedValueOnce({
			data: {
				data: {
					platforms: [
						{
							code: "corekg",
							name: "CoreKG",
							description: "连接知识库",
							auto_connect_supported: true,
							connected: true,
							plugin_id: "plugin_corekg",
						},
					],
				},
			},
		});

		render(<McpConnectorPanel />);

		expect(await screen.findByText("浏览器连接器")).toBeInTheDocument();
		expect(screen.getAllByText("知识库")).toHaveLength(1);
		expect(screen.getByText("共 1 个")).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "管理 浏览器连接器" })).toBeInTheDocument();
	});

	it("deletes a custom MCP connector from its action menu", async () => {
		render(<McpConnectorPanel />);
		await screen.findByText("浏览器连接器");

		fireEvent.click(screen.getByRole("button", { name: "管理 浏览器连接器" }));
		fireEvent.click(await screen.findByRole("menuitem", { name: "删除连接" }));

		expect(screen.getByText("删除 浏览器连接器？")).toBeInTheDocument();
		expect(screen.getByText(/会从所有关联项目中移除/)).toBeInTheDocument();
		fireEvent.click(screen.getByRole("button", { name: "确认删除" }));

		await waitFor(() => expect(mockPluginDelete).toHaveBeenCalledWith("plugin_mcp"));
		await waitFor(() => expect(mockPluginList).toHaveBeenCalledTimes(2));
	});

	it("shows unsupported CoreKG automatic authorization as disabled", async () => {
		mockPluginListMCPPlatforms.mockResolvedValueOnce({
			data: {
				data: {
					platforms: [
						{
							code: "corekg",
							name: "CoreKG",
							description: "连接知识库",
							auto_connect_supported: false,
							connected: false,
						},
					],
				},
			},
		});
		render(<McpConnectorPanel />);

		expect(await screen.findByText("当前版本暂不支持自动授权")).toBeInTheDocument();
		expect(mockPluginConnectMCPPlatform).not.toHaveBeenCalled();
	});

	it("does not expose test or disconnect actions for CoreKG", async () => {
		render(<McpConnectorPanel />);

		expect(await screen.findByText("已连接")).toBeInTheDocument();
		expect(screen.queryByRole("button", { name: "管理 知识库" })).not.toBeInTheDocument();
		expect(screen.queryByText("测试连接")).not.toBeInTheDocument();
		expect(screen.queryByText("断开连接")).not.toBeInTheDocument();
	});

	it("keeps manual actions for non-CoreKG platforms", async () => {
		mockPluginListMCPPlatforms.mockResolvedValueOnce({
			data: {
				data: {
					platforms: [
						{
							code: "future-platform",
							name: "未来平台",
							description: "未来平台连接器",
							auto_connect_supported: true,
							connected: true,
							plugin_id: "plugin_future",
						},
					],
				},
			},
		});
		render(<McpConnectorPanel />);

		fireEvent.click(await screen.findByRole("button", { name: "管理 未来平台" }));
		expect(await screen.findByRole("menuitem", { name: "测试连接" })).toBeInTheDocument();
		expect(screen.getByRole("menuitem", { name: "断开连接" })).toBeInTheDocument();
	});

	it("connects a Skill-only email platform with schema-driven authorization", async () => {
		mockPluginListMCPPlatforms.mockResolvedValueOnce({
			data: {
				data: {
					platforms: [
						{
							code: "netease-mail",
							name: "邮箱",
							description: "通过 IMAP/SMTP 收发邮件",
							mode: "skill_only",
							auth_type: "form",
							auth_fields: [
								{
									key: "email",
									label: "邮箱地址",
									type: "text",
									required: true,
									placeholder: "yourname@163.com",
								},
								{
									key: "authorization_code",
									label: "IMAP/SMTP 授权码",
									type: "password",
									required: true,
								},
							],
							auto_connect_supported: true,
							connected: false,
						},
					],
				},
			},
		});
		mockPluginConnectMCPPlatform.mockResolvedValueOnce({
			data: {
				data: {
					platform: { code: "netease-mail", connected: true },
					plugin: { public_id: "plugin_mail" },
					tool_count: 0,
				},
			},
		});
		render(<McpConnectorPanel />);

		fireEvent.click(await screen.findByRole("button", { name: "连接 邮箱" }));
		expect(screen.getByRole("heading", { name: "连接 邮箱" })).toBeInTheDocument();
		fireEvent.change(screen.getByLabelText("邮箱地址"), {
			target: { value: "user@example.com" },
		});
		fireEvent.change(screen.getByLabelText("IMAP/SMTP 授权码"), {
			target: { value: "client-authorization-code" },
		});
		fireEvent.click(screen.getByRole("button", { name: "连接" }));

		await waitFor(() =>
			expect(mockPluginConnectMCPPlatform).toHaveBeenCalledWith("netease-mail", {
				auth_values: {
					email: "user@example.com",
					authorization_code: "client-authorization-code",
				},
			}),
		);
	});

	it("opens Baidu OAuth externally and polls until the connector is active", async () => {
		mockPluginListMCPPlatforms.mockResolvedValueOnce({
			data: {
				data: {
					platforms: [
						{
							code: "baidu-netdisk",
							name: "百度网盘",
							description: "通过百度网盘 MCP 管理文件",
							mode: "hybrid",
							auth_type: "oauth",
							auto_connect_supported: true,
							connected: false,
							authorization_status: "disconnected",
						},
					],
				},
			},
		});
		render(<McpConnectorPanel />);
		const connectButton = await screen.findByRole("button", { name: "连接 百度网盘" });

		vi.useFakeTimers();
		fireEvent.click(connectButton);
		await vi.advanceTimersByTimeAsync(0);
		expect(mockPluginStartMCPPlatformOAuth).toHaveBeenCalledWith("baidu-netdisk");
		expect(mockOpenExternalLink).toHaveBeenCalledWith(
			"https://openapi.baidu.com/oauth/2.0/authorize?state=opaque",
		);
		await vi.advanceTimersByTimeAsync(2_000);
		vi.useRealTimers();

		await waitFor(() =>
			expect(mockPluginGetMCPPlatformOAuthStatus).toHaveBeenCalledWith(
				"baidu-netdisk",
				"oauth_attempt",
			),
		);
	});

	it("keeps an unfinished OAuth connector reconnectable without showing pending authorization", async () => {
		mockPluginListMCPPlatforms.mockResolvedValueOnce({
			data: {
				data: {
					platforms: [
						{
							code: "baidu-netdisk",
							name: "百度网盘",
							description: "通过百度网盘 MCP 管理文件",
							mode: "hybrid",
							auth_type: "oauth",
							auto_connect_supported: true,
							connected: false,
							authorization_status: "pending",
						},
					],
				},
			},
		});

		render(<McpConnectorPanel />);

		expect(await screen.findByRole("button", { name: "连接 百度网盘" })).toBeEnabled();
		expect(screen.queryByText("待完成授权")).not.toBeInTheDocument();
	});

	it("filters connectors by search keyword without connection-state controls", async () => {
		render(<McpConnectorPanel />);

		expect(await screen.findByText("浏览器连接器")).toBeInTheDocument();
		fireEvent.change(screen.getByRole("searchbox", { name: "搜索 MCP 连接器" }), {
			target: { value: "不存在" },
		});
		expect(screen.getByText("暂无符合条件的连接器")).toBeInTheDocument();
	});

	it.each([
		"corekg",
		"CoreKG",
		"COREKG",
	])("hides the knowledge base platform from %s search", async (keyword) => {
		mockPluginListMCPPlatforms.mockResolvedValueOnce({
			data: {
				data: {
					platforms: [
						{
							code: "corekg",
							name: "CoreKG",
							description: "COREKG 连接知识库",
							auto_connect_supported: true,
							connected: false,
						},
					],
				},
			},
		});
		render(<McpConnectorPanel />);

		expect(await screen.findByText("知识库")).toBeInTheDocument();
		expect(screen.getByText("连接知识库")).toBeInTheDocument();
		expect(screen.queryByText(/CoreKG|COREKG/i)).not.toBeInTheDocument();

		fireEvent.change(screen.getByRole("searchbox", { name: "搜索 MCP 连接器" }), {
			target: { value: keyword },
		});
		expect(screen.queryByText("知识库")).not.toBeInTheDocument();
		expect(screen.getByText("暂无符合条件的连接器")).toBeInTheDocument();
	});

	it("keeps the knowledge base platform searchable by 知识库", async () => {
		mockPluginListMCPPlatforms.mockResolvedValueOnce({
			data: {
				data: {
					platforms: [
						{
							code: "corekg",
							name: "CoreKG",
							description: "连接知识库",
							auto_connect_supported: true,
							connected: false,
						},
					],
				},
			},
		});
		render(<McpConnectorPanel />);

		expect(await screen.findByText("知识库")).toBeInTheDocument();
		fireEvent.change(screen.getByRole("searchbox", { name: "搜索 MCP 连接器" }), {
			target: { value: "知识库" },
		});
		expect(screen.getByText("知识库")).toBeInTheDocument();
	});

	it("creates an HTTP MCP without exposing or sending code", async () => {
		render(<McpConnectorPanel />);
		await screen.findByText("浏览器连接器");
		fireEvent.click(screen.getByRole("button", { name: "配置自定义 MCP" }));

		expect(screen.queryByLabelText("Code")).not.toBeInTheDocument();
		expect(screen.queryByLabelText("说明")).not.toBeInTheDocument();
		expect(screen.queryByLabelText("Headers JSON")).not.toBeInTheDocument();
		expect(screen.getByRole("button", { name: "STDIO" })).toBeDisabled();
		expect(screen.getByRole("button", { name: "STDIO" })).toHaveAttribute("aria-pressed", "false");
		expect(screen.getByRole("button", { name: "流式 HTTP" })).toHaveAttribute(
			"aria-pressed",
			"true",
		);
		expect(screen.getByLabelText("Bearer 令牌")).toBeEnabled();
		expect(screen.queryByText("来自环境变量的标头")).not.toBeInTheDocument();
		expect(screen.queryByRole("button", { name: "添加变量" })).not.toBeInTheDocument();

		fireEvent.change(screen.getByLabelText("名称"), { target: { value: "知识库" } });
		fireEvent.change(screen.getByLabelText("URL"), {
			target: { value: "https://example.com/knowledge/mcp" },
		});
		fireEvent.change(screen.getByLabelText("Bearer 令牌"), {
			target: { value: "knowledge-secret" },
		});

		fireEvent.click(screen.getByRole("button", { name: "保存" }));
		await waitFor(() =>
			expect(mockPluginAddMCP).toHaveBeenCalledWith({
				name: "知识库",
				transport: "http",
				url: "https://example.com/knowledge/mcp",
				bearer_token: "knowledge-secret",
				headers: {},
			}),
		);
		await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
		expect(mockPluginList).toHaveBeenCalledTimes(2);
		expect(mockPluginTestMCP).not.toHaveBeenCalled();
	});

	it("adds, removes, and validates header rows", async () => {
		render(<McpConnectorPanel />);
		await screen.findByText("浏览器连接器");
		fireEvent.click(screen.getByRole("button", { name: "配置自定义 MCP" }));
		fireEvent.change(screen.getByLabelText("名称"), { target: { value: "知识库" } });
		fireEvent.change(screen.getByLabelText("URL"), {
			target: { value: "https://example.com/knowledge/mcp" },
		});

		fireEvent.change(screen.getByLabelText("标头键 1"), {
			target: { value: "Authorization" },
		});
		fireEvent.click(screen.getByRole("button", { name: "保存" }));
		await waitFor(() => expect(mockPluginAddMCP).not.toHaveBeenCalled());

		fireEvent.change(screen.getByLabelText("标头值 1"), {
			target: { value: "Bearer token" },
		});
		fireEvent.click(screen.getByRole("button", { name: "添加标头" }));
		fireEvent.change(screen.getByLabelText("标头键 2"), {
			target: { value: "authorization" },
		});
		fireEvent.change(screen.getByLabelText("标头值 2"), {
			target: { value: "duplicate" },
		});
		fireEvent.click(screen.getByRole("button", { name: "保存" }));
		await waitFor(() => expect(mockPluginAddMCP).not.toHaveBeenCalled());

		fireEvent.click(screen.getByRole("button", { name: "删除标头 2" }));
		fireEvent.click(screen.getByRole("button", { name: "保存" }));
		await waitFor(() =>
			expect(mockPluginAddMCP).toHaveBeenCalledWith({
				name: "知识库",
				transport: "http",
				url: "https://example.com/knowledge/mcp",
				bearer_token: "",
				headers: { Authorization: "Bearer token" },
			}),
		);
	});

	it("hydrates and updates saved headers without sending code or description", async () => {
		render(<McpConnectorPanel />);
		await screen.findByText("浏览器连接器");
		fireEvent.click(screen.getByRole("button", { name: "管理 浏览器连接器" }));
		fireEvent.click(await screen.findByRole("menuitem", { name: "管理连接" }));

		expect(await screen.findByDisplayValue("Bearer token")).toBeInTheDocument();
		expect(screen.getByDisplayValue("browser-secret")).toBeInTheDocument();
		fireEvent.change(screen.getByLabelText("名称"), { target: { value: "浏览器 v2" } });
		fireEvent.click(screen.getByRole("button", { name: "保存" }));
		await waitFor(() =>
			expect(mockPluginUpdateMCP).toHaveBeenCalledWith("plugin_mcp", {
				name: "浏览器 v2",
				transport: "http",
				url: "https://example.com/mcp",
				bearer_token: "browser-secret",
				headers: { Authorization: "Bearer token" },
			}),
		);
	});

	it("disables stdio creation while preserving existing stdio configuration hydration", async () => {
		render(<McpConnectorPanel />);
		await screen.findByText("浏览器连接器");
		fireEvent.click(screen.getByRole("button", { name: "配置自定义 MCP" }));
		expect(screen.getByRole("button", { name: "STDIO" })).toBeDisabled();
		expect(screen.getByRole("button", { name: "STDIO" })).toHaveAttribute("aria-pressed", "false");
		fireEvent.click(screen.getByRole("button", { name: "关闭" }));
		await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());

		mockPluginGet.mockResolvedValueOnce({
			data: {
				data: {
					definition: {
						schema: "mcp/v1",
						transport: "stdio",
						name: "browser",
						command: "npx",
						args: ["-y", "@example/mcp"],
						env: { LOG_LEVEL: "debug" },
					},
				},
			},
		});
		fireEvent.click(screen.getByRole("button", { name: "管理 浏览器连接器" }));
		fireEvent.click(await screen.findByRole("menuitem", { name: "管理连接" }));

		expect(await screen.findByDisplayValue("@example/mcp")).toBeInTheDocument();
		expect(screen.getByDisplayValue("LOG_LEVEL")).toBeInTheDocument();
		expect(screen.queryByText("环境变量传递")).not.toBeInTheDocument();
		expect(screen.queryByLabelText("工作目录")).not.toBeInTheDocument();
	});

	it("does not execute a stdio command through the server-side connection test", async () => {
		mockPluginGet.mockResolvedValueOnce({
			data: {
				data: {
					definition: {
						schema: "mcp/v1",
						transport: "stdio",
						name: "browser",
						command: "npx",
						args: ["-y", "@example/mcp"],
					},
				},
			},
		});
		render(<McpConnectorPanel />);
		await screen.findByText("浏览器连接器");
		fireEvent.click(screen.getByRole("button", { name: "管理 浏览器连接器" }));
		fireEvent.click(await screen.findByRole("menuitem", { name: "测试连接" }));

		await waitFor(() => expect(mockPluginGet).toHaveBeenCalledWith("plugin_mcp"));
		expect(mockPluginTestMCP).not.toHaveBeenCalled();
	});

	it("does not request organization connectors before login", async () => {
		render(<McpConnectorPanel isAuthenticated={false} />);

		expect(await screen.findByText("登录后查看你的连接器")).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "配置自定义 MCP" })).toBeDisabled();
		expect(mockPluginList).not.toHaveBeenCalled();
	});
});
