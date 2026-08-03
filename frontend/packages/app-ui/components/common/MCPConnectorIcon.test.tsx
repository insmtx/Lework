import "@testing-library/jest-dom/vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { MCPConnectorIcon } from "./MCPConnectorIcon";

describe("MCPConnectorIcon", () => {
	afterEach(cleanup);

	it.each([
		"baidu-netdisk",
		"baidu-netdisk-0123456789abcdef",
	])("uses the Baidu Netdisk logo for connector identity %s", (code) => {
		render(<MCPConnectorIcon code={code} name="百度网盘" />);

		expect(screen.getByRole("img", { name: "百度网盘 Logo" })).toHaveAttribute("src");
	});

	it("uses the CoreKG platform logo for CoreKG connector identities", () => {
		render(<MCPConnectorIcon code="corekg-0123456789abcdef" name="CoreKG" />);

		expect(screen.getByRole("img", { name: "CoreKG Logo" })).toHaveAttribute("src");
	});

	it.each([
		"netease-mail",
		"netease-mail-0123456789abcdef",
	])("uses the NetEase Mail logo for connector identity %s", (code) => {
		render(<MCPConnectorIcon code={code} name="邮箱" />);

		expect(screen.getByRole("img", { name: "邮箱 Logo" })).toHaveAttribute("src");
	});

	it("uses the default icon for custom connectors", () => {
		render(<MCPConnectorIcon code="mcp-0123456789abcdef" name="Custom MCP" />);

		expect(screen.queryByRole("img")).not.toBeInTheDocument();
	});
});
