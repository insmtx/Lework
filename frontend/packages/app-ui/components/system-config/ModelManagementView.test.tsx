// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
	store: {
		models: [] as unknown[],
		loading: false,
		loaded: true,
		error: null,
		fetchModels: vi.fn(),
		testModel: vi.fn(),
	},
}));
vi.mock("@leros/store", () => ({
	useModelStore: (selector: (s: unknown) => unknown) => selector(mocks.store),
}));
vi.mock("./ModelFormDialog", () => ({ ModelFormDialog: () => null }));
vi.mock("@leros/ui/components/ui/scroll-area", () => ({
	ScrollArea: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));

import { ModelManagementView } from "./ModelManagementView";

describe("model list observations", () => {
	let container: HTMLDivElement;
	let root: Root;
	beforeEach(() => {
		vi.useFakeTimers();
		vi.setSystemTime(new Date("2026-11-02T00:00:00Z"));
		mocks.store.fetchModels.mockReset().mockResolvedValue(undefined);
		const now = Date.now() / 1000;
		mocks.store.models = [
			{
				id: 1,
				name: "Qwen",
				model: "Qwen",
				provider: "openai",
				status: "active",
				runtime: { source: "spark", state: "ready", observed_at: now, valid_until: now + 30 },
			},
		];
		container = document.createElement("div");
		document.body.append(container);
		root = createRoot(container);
	});
	afterEach(() => {
		act(() => root.unmount());
		container.remove();
		vi.useRealTimers();
	});
	it("shows separate columns and silently polls every 15 seconds without removing rows", async () => {
		await act(async () => root.render(<ModelManagementView />));
		expect(container.textContent).toContain("来源");
		expect(container.textContent).toContain("实际状态");
		expect(container.textContent).toContain("启用状态");
		expect(container.textContent).toContain("Spark");
		expect(container.textContent).toContain("已就绪");
		mocks.store.fetchModels.mockImplementationOnce(
			() =>
				new Promise(() => {
					// 保持请求未完成，验证后台刷新期间行内容不消失。
				}),
		);
		await act(async () => vi.advanceTimersByTimeAsync(15000));
		expect(mocks.store.fetchModels).toHaveBeenLastCalledWith({ silent: true });
		expect(container.textContent).toContain("Qwen");
		expect(container.textContent).not.toContain("加载模型列表");
		await act(async () => vi.advanceTimersByTimeAsync(16000));
		expect(container.textContent).toContain("未知");
	});
	it("stops polling after leaving the view", async () => {
		await act(async () => root.render(<ModelManagementView />));
		await act(async () => root.render(null));
		await act(async () => vi.advanceTimersByTimeAsync(45000));
		expect(mocks.store.fetchModels).toHaveBeenCalledTimes(1);
	});
});
