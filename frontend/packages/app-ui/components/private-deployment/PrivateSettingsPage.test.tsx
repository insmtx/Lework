// @vitest-environment jsdom

import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const storeMocks = vi.hoisted(() => ({
	isBrandingSettingsEnabled: vi.fn(),
	isPrivateDeployment: true,
}));

vi.mock("@leros/store", () => ({
	isPrivateDeployment: storeMocks.isPrivateDeployment,
	isBrandingSettingsEnabled: storeMocks.isBrandingSettingsEnabled,
}));

vi.mock("./PrivateServerSettingsCard", () => ({
	PrivateServerSettingsCard: () => <div data-testid="server-settings">server</div>,
}));

vi.mock("./BrandingSettingsCard", () => ({
	BrandingSettingsCard: () => <div data-testid="branding-settings">branding</div>,
}));

import { PrivateSettingsPage } from "./PrivateSettingsPage";

describe("PrivateSettingsPage", () => {
	let container: HTMLDivElement;
	let root: Root;

	beforeEach(() => {
		container = document.createElement("div");
		document.body.appendChild(container);
		root = createRoot(container);
		storeMocks.isBrandingSettingsEnabled.mockReset();
	});

	afterEach(() => {
		act(() => root.unmount());
		container.remove();
	});

	it("only shows server settings when branding flag is absent", async () => {
		storeMocks.isBrandingSettingsEnabled.mockReturnValue(false);
		await act(async () => root.render(<PrivateSettingsPage />));

		expect(container.querySelector('[data-testid="server-settings"]')).not.toBeNull();
		expect(container.querySelector('[data-testid="branding-settings"]')).toBeNull();
		expect(container.textContent).toContain("管理私有化后端服务连接。");
	});

	it("shows branding settings when branding flag is present", async () => {
		storeMocks.isBrandingSettingsEnabled.mockReturnValue(true);
		await act(async () => root.render(<PrivateSettingsPage />));

		expect(container.querySelector('[data-testid="server-settings"]')).not.toBeNull();
		expect(container.querySelector('[data-testid="branding-settings"]')).not.toBeNull();
		expect(container.textContent).toContain("管理私有化后端服务连接与系统品牌展示。");
	});
});
