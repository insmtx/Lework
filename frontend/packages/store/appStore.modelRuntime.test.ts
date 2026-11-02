import { describe, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({ list: vi.fn(), test: vi.fn() }));
vi.mock("./api/modelApi", () => ({ modelApi: api }));

import { useAppStore } from "./appStore";

describe("real app store model isolation", () => {
	it("invalidates an old model list request when organization data is reset", async () => {
		let resolve!: (value: unknown) => void;
		api.list.mockImplementationOnce(
			() =>
				new Promise((r) => {
					resolve = r;
				}),
		);
		const pending = useAppStore.getState().fetchModels();
		useAppStore.getState().resetAuthScopedData();
		resolve({ data: { data: { items: [{ id: 1, org_id: 1, model: "old-org" }], total: 1 } } });
		await pending;
		expect(useAppStore.getState().models).toEqual([]);
	});
});
