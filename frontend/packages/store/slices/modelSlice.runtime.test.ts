import { beforeEach, describe, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({ list: vi.fn(), test: vi.fn() }));
vi.mock("../api/modelApi", () => ({ modelApi: api }));

import { ModelSliceImpl, type ModelStore } from "./modelSlice";

const model = {
	id: 1,
	org_id: 1,
	model: "Qwen",
	base_url: "http://local",
	updated_at: "2026-11-02T00:00:00Z",
	created_at: "2026-11-02T00:00:00Z",
	runtime: { source: "configuration", state: "unobserved", observed_at: 0, valid_until: 0 },
};
const response = (value = model) => ({ data: { data: { items: [value], total: 1 } } });
describe("model status refresh isolation", () => {
	let state: ModelStore;
	let slice: ModelSliceImpl;
	beforeEach(() => {
		vi.resetAllMocks();
		state = { models: [], loaded: false, loading: false, error: null } as unknown as ModelStore;
		slice = new ModelSliceImpl((update) => {
			state = { ...state, ...(typeof update === "function" ? update(state) : update) };
		});
	});
	it("discards pending old-organization responses after reset", async () => {
		let resolve!: (value: unknown) => void;
		api.list.mockImplementationOnce(
			() =>
				new Promise((r) => {
					resolve = r;
				}),
		);
		const pending = slice.fetchModels();
		slice.resetAuthScopedData();
		resolve(response());
		await pending;
		expect(state.models).toEqual([]);
	});
	it("keeps silent refresh visually stable and keeps test results only for unchanged configurations", async () => {
		api.list.mockResolvedValue(response());
		await slice.fetchModels();
		api.test.mockResolvedValue({ data: { data: { success: true } } });
		await slice.testModel({ id: 1 });
		expect(state.models[0]).toMatchObject({ id: 1, connectionTest: { state: "available" } });
		const pending = slice.fetchModels({ silent: true });
		expect(state.loading).toBe(false);
		await pending;
		expect(state.models[0]).toMatchObject({ id: 1, connectionTest: { state: "available" } });
		api.list.mockResolvedValue(response({ ...model, base_url: "https://other" }));
		await slice.fetchModels({ silent: true });
		expect(state.models[0]).toMatchObject({ id: 1, baseUrl: "https://other" });
		expect(state.models[0]?.connectionTest).toBeUndefined();
	});
	it("does not apply a test response after logout", async () => {
		api.list.mockResolvedValue(response());
		await slice.fetchModels();
		let resolve!: (value: unknown) => void;
		api.test.mockImplementationOnce(
			() =>
				new Promise((r) => {
					resolve = r;
				}),
		);
		const pending = slice.testModel({ id: 1 });
		slice.resetAuthScopedData();
		resolve({ data: { data: { success: true } } });
		await pending;
		expect(state.models).toEqual([]);
	});
});
