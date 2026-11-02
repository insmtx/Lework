// @vitest-environment jsdom

import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const storeMocks = vi.hoisted(() => ({
	API_BASE_URL: "https://old.example.com/v1",
	normalizeAPIBaseURL: vi.fn((value: string) => value),
	readServerBaseURL: vi.fn(),
	saveServerConnection: vi.fn(),
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
		storeMocks.readServerBaseURL.mockReturnValue("https://old.example.com/v1");

		storeMocks.saveServerConnection.mockReset();
		storeMocks.testServerConnection.mockReset();
		storeMocks.normalizeAPIBaseURL.mockImplementation((value: string) => value);
	});

	afterEach(() => {
		act(() => root.unmount());
		vi.useRealTimers();
		container.remove();
	});

	const render = async () =>
		act(async () =>
			root.render(
				<PrivateServerSettingsCard
					onReload={onReload}
					renderDiscovery={(select) => (
						<button type="button" onClick={() => void select("https://new.example.com/v1")}>
							选择设备
						</button>
					)}
				/>,
			),
		);
	it("validates once and requires an explicit warning confirmation before replacing auth", async () => {
		storeMocks.testServerConnection.mockResolvedValue("https://new.example.com/v1");
		await render();
		await act(async () => getButton(container, "选择设备").click());
		expect(storeMocks.testServerConnection).toHaveBeenCalledOnce();
		expect(storeMocks.saveServerConnection).not.toHaveBeenCalled();
		expect(container.querySelector('[role="alertdialog"]')?.textContent).toContain("重新登录");
		await act(async () => {
			getButton(container, "确认切换").click();
			getButton(container, "确认切换").click();
		});
		expect(storeMocks.saveServerConnection).toHaveBeenCalledWith(
			"https://new.example.com/v1",
			"https://old.example.com/v1",
		);
		expect(onReload).toHaveBeenCalledOnce();
	});
	it("same endpoint saves without a switch warning", async () => {
		storeMocks.testServerConnection.mockResolvedValue("https://old.example.com/v1");
		await render();
		await act(async () => getButton(container, "连接并保存").click());
		expect(container.querySelector('[role="alertdialog"]')).toBeNull();
		expect(storeMocks.saveServerConnection).toHaveBeenCalledWith(
			"https://old.example.com/v1",
			"https://old.example.com/v1",
		);
		expect(onReload).toHaveBeenCalledOnce();
	});
	it("failed persistence never reloads", async () => {
		storeMocks.testServerConnection.mockResolvedValue("https://old.example.com/v1");
		storeMocks.saveServerConnection.mockImplementation(() => {
			throw new Error("quota");
		});
		await render();
		await act(async () => getButton(container, "连接并保存").click());
		expect(onReload).not.toHaveBeenCalled();
		expect(container.querySelector('[role="alert"]')).not.toBeNull();
	});
	it("suppresses duplicate selections and ignores completion after unmount", async () => {
		let finish!: (s: string) => void;
		storeMocks.testServerConnection.mockImplementation(
			() =>
				new Promise((resolve) => {
					finish = resolve;
				}),
		);
		await render();
		await act(async () => {
			getButton(container, "选择设备").click();
			getButton(container, "选择设备").click();
		});
		expect(storeMocks.testServerConnection).toHaveBeenCalledOnce();
		await act(async () => root.render(<div />));
		await act(async () => finish("https://new.example.com/v1"));
		expect(storeMocks.saveServerConnection).not.toHaveBeenCalled();
		expect(onReload).not.toHaveBeenCalled();
	});

	it("network invalidation prevents an older validation from offering a save", async () => {
		let finish!: (s: string) => void;
		storeMocks.testServerConnection.mockImplementation(
			() =>
				new Promise((resolve) => {
					finish = resolve;
				}),
		);
		await render();
		await act(async () => getButton(container, "选择设备").click());
		await act(async () => window.dispatchEvent(new Event("leros:connection-invalidated")));
		await act(async () => finish("https://new.example.com/v1"));
		expect(container.querySelector('[role="alertdialog"]')).toBeNull();
		expect(storeMocks.saveServerConnection).not.toHaveBeenCalled();
	});
	it("expires a pending switch and revalidates before saving a fresh confirmation", async () => {
		vi.useFakeTimers();
		storeMocks.testServerConnection.mockResolvedValue("https://new.example.com/v1");
		await render();
		await act(async () => getButton(container, "选择设备").click());
		await act(async () => vi.advanceTimersByTimeAsync(30000));
		expect(container.querySelector('[role="alertdialog"]')).toBeNull();
		await act(async () => getButton(container, "选择设备").click());
		storeMocks.testServerConnection.mockRejectedValue(new Error("offline"));
		await act(async () => getButton(container, "确认切换").click());
		expect(storeMocks.saveServerConnection).not.toHaveBeenCalled();
		expect(container.querySelector('[role="alert"]')).not.toBeNull();
		vi.useRealTimers();
	});
	it("manual edit and manual connection invalidate discovery selection", async () => {
		const cancelled = vi.fn();
		window.addEventListener("leros:connection-selection-cancelled", cancelled);
		storeMocks.testServerConnection.mockResolvedValue("https://new.example.com/v1");
		await render();
		const input = container.querySelector("input");
		if (!input) throw new Error("input missing");
		await act(async () => {
			Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set?.call(
				input,
				"https://manual.example.com/v1",
			);
			input.dispatchEvent(new Event("input", { bubbles: true }));
		});
		expect(cancelled).toHaveBeenCalled();
		cancelled.mockClear();
		await act(async () => getButton(container, "连接并保存").click());
		expect(cancelled).toHaveBeenCalledOnce();
		window.removeEventListener("leros:connection-selection-cancelled", cancelled);
	});
});

function getButton(container: HTMLElement, label: string): HTMLButtonElement {
	const button = [...container.querySelectorAll("button")].find((item) =>
		item.textContent?.includes(label),
	);
	if (!button) throw new Error(`button not found: ${label}`);
	return button;
}
