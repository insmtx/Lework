import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SkillMarketView } from "./SkillMarketView";

const { layoutState } = vi.hoisted(() => ({
	layoutState: {
		setWorkbenchComposerPrefill: vi.fn(),
		selectWorkbenchProject: vi.fn(),
		selectWorkbenchTask: vi.fn(),
		switchView: vi.fn(),
	},
}));

vi.mock("@leros/store", () => ({
	BRANDING_CHANGED_EVENT: "leros:branding-changed",
	readBrandLogo: () => null,
	readBrandName: () => "Lework",
	useLayoutStore: (selector: (state: typeof layoutState) => unknown) => selector(layoutState),
}));

vi.mock("../auth", () => ({
	useAuth: () => ({
		isAuthenticated: true,
		requireAuth: (action: () => void) => action(),
	}),
}));

vi.mock("./MarketplacePanel", () => ({
	MarketplacePanel: () => <div>技能市场内容</div>,
}));

vi.mock("./MySkillsPanel", () => ({
	MySkillsPanel: () => <div>我的技能内容</div>,
}));

vi.mock("./McpConnectorPanel", () => ({
	McpConnectorPanel: () => <div>MCP 页面内容</div>,
}));

vi.mock("./SkillDetailView", () => ({
	SkillDetailView: () => <div>技能详情</div>,
}));

vi.mock("./SkillImportDialog", () => ({
	SkillImportDialog: () => null,
}));

describe("SkillMarketView plugin layout", () => {
	afterEach(cleanup);

	it("renames the page to plugins and provides MCP and skill tabs", () => {
		render(<SkillMarketView />);

		expect(screen.getByRole("heading", { name: "插件" })).toBeInTheDocument();
		expect(screen.getByText("统一管理连接器和技能")).toBeInTheDocument();
		expect(screen.getByRole("tab", { name: "MCP 连接器" })).toBeInTheDocument();
		expect(screen.getByRole("tab", { name: "技能库" })).toHaveAttribute("data-active");
		expect(screen.getByRole("searchbox", { name: "搜索技能" })).toBeInTheDocument();
		expect(screen.getByRole("tab", { name: "技能市场 0" })).toBeInTheDocument();
		expect(screen.getByRole("tab", { name: "组织共享 0" })).toBeInTheDocument();
		expect(screen.getByRole("tab", { name: "我的 0" })).toBeInTheDocument();
		expect(screen.queryByRole("button", { name: "全部分类" })).not.toBeInTheDocument();
		expect(screen.getByRole("button", { name: "导入技能" })).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "创建技能" })).toBeInTheDocument();

		fireEvent.click(screen.getByRole("tab", { name: "MCP 连接器" }));
		expect(screen.getByText("MCP 页面内容")).toBeInTheDocument();
	});
});
