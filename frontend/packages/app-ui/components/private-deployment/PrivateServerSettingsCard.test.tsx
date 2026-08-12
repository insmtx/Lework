// @vitest-environment jsdom

import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const storeMocks = vi.hoisted(() => ({
	clearStoredAuthUser: vi.fn(),
	normalizeAPIBaseURL: vi.fn((value: string) => value),
	readPrivateServerBaseURL: vi.fn(),
	savePrivateServerBaseURL: vi.fn(),
	testServerConnection: vi.fn(),
}));

vi.mock("@leros/store", () => ({
	isPrivateDeployment: true,
	...storeMocks,
}));

import { PrivateServerSettingsCard } from "./PrivateServerSettingsCard";

describe("PrivateServerSettingsCard", () => {
	let container: HTMLDivElement;
	let root: Root;
	let onReload: ReturnType<typeof vi.fn<() => void>>;

	beforeEach(() => {
		container = document.createElement("div");
		document.body.appendChild(container);
		root = createRoot(container);
		onReload = vi.fn<() => void>();
		storeMocks.readPrivateServerBaseURL.mockReturnValue("https://old.example.com/v1");
		storeMocks.clearStoredAuthUser.mockReset();
		storeMocks.savePrivateServerBaseURL.mockReset();
		storeMocks.testServerConnection.mockReset();
		storeMocks.normalizeAPIBaseURL.mockImplementation((value: string) => value);
	});

	afterEach(() => {
		act(() => root.unmount());
		container.remove();
	});

	it("requires a successful test before saving a changed address", async () => {
		storeMocks.testServerConnection.mockResolvedValue("https://new.example.com/v1");
		await act(async () => root.render(<PrivateServerSettingsCard onReload={onReload} />));

		const input = container.querySelector("input");
		if (!input) throw new Error("server address input was not rendered");
		const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
		setter?.call(input, "https://new.example.com/v1");
		input.dispatchEvent(new Event("input", { bubbles: true }));

		const saveButton = getButton(container, "保存并重新加载");
		expect(saveButton.disabled).toBe(true);

		await act(async () => getButton(container, "测试连接").click());
		expect(storeMocks.testServerConnection).toHaveBeenCalledWith("https://new.example.com/v1");
		expect(saveButton.disabled).toBe(false);

		await act(async () => saveButton.click());
		expect(storeMocks.savePrivateServerBaseURL).toHaveBeenCalledWith("https://new.example.com/v1");
		expect(storeMocks.clearStoredAuthUser).toHaveBeenCalledOnce();
		expect(onReload).toHaveBeenCalledOnce();
	});
});

function getButton(container: HTMLElement, label: string): HTMLButtonElement {
	const button = [...container.querySelectorAll("button")].find((item) =>
		item.textContent?.includes(label),
	);
	if (!button) throw new Error(`button not found: ${label}`);
	return button;
}
