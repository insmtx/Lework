import { afterEach, describe, expect, it, vi } from "vitest";
import { isExternalHttpLink, openExternalLink } from "./open-external-link";

describe("openExternalLink", () => {
	afterEach(() => {
		vi.restoreAllMocks();
		delete (window as Window & { lerosDesktop?: unknown }).lerosDesktop;
	});

	it("识别 Markdown 外链", () => {
		expect(isExternalHttpLink("https://example.com")).toBe(true);
		expect(isExternalHttpLink("/workbench")).toBe(false);
		expect(isExternalHttpLink(undefined)).toBe(false);
	});

	it("桌面端通过 lerosDesktop.openExternal 打开链接", () => {
		const openExternal = vi.fn().mockResolvedValue(true);
		(window as Window & { lerosDesktop?: { openExternal: typeof openExternal } }).lerosDesktop = {
			openExternal,
		};

		openExternalLink("https://example.com/docs");

		expect(openExternal).toHaveBeenCalledWith("https://example.com/docs");
	});

	it("Web 端使用 window.open 打开链接", () => {
		const windowOpen = vi.spyOn(window, "open").mockImplementation(() => null);

		openExternalLink("https://example.com/docs");

		expect(windowOpen).toHaveBeenCalledWith(
			"https://example.com/docs",
			"_blank",
			"noopener,noreferrer",
		);
	});
});
