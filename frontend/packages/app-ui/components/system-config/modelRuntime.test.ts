import type { ModelItem } from "@leros/store";
import { describe, expect, it } from "vitest";
import { modelRuntimePresentation } from "./modelRuntime";

const item = { id: 1, orgId: 1, status: "active" } as ModelItem;
describe("model runtime presentation", () => {
	it("never treats enabled or an old backend as proof of availability or source", () => {
		expect(modelRuntimePresentation(item, 100)).toMatchObject({ source: "未知", label: "未知" });
	});
	it("shows a fresh Spark observation and expires it without polling success", () => {
		const m = {
			...item,
			runtime: { source: "spark", state: "ready", observed_at: 90, valid_until: 120 },
		};
		expect(modelRuntimePresentation(m, 100)).toMatchObject({ source: "Spark", label: "已就绪" });
		expect(modelRuntimePresentation(m, 121)).toMatchObject({ source: "Spark", label: "未知" });
		expect(modelRuntimePresentation(m, 80).label).toBe("未知");
	});
	it("keeps disabled configuration independent from running state", () => {
		expect(
			modelRuntimePresentation(
				{
					...item,
					status: "inactive",
					runtime: { source: "spark", state: "ready", observed_at: 90, valid_until: 120 },
				},
				100,
			).label,
		).toBe("已就绪");
	});
	it("shows bounded connection-test results only for configured models", () => {
		const m = {
			...item,
			runtime: { source: "configuration", state: "unobserved", observed_at: 0, valid_until: 0 },
		};
		expect(modelRuntimePresentation(m, 100).label).toBe("未检测");
		const tested = {
			...m,
			connectionTest: { state: "available" as const, observedAt: 90, validUntil: 150 },
		};
		expect(modelRuntimePresentation(tested, 100).label).toBe("可用");
		expect(modelRuntimePresentation(tested, 151).label).toBe("未知");
		expect(
			modelRuntimePresentation(
				{
					...tested,
					runtime: { source: "spark", state: "stopped", observed_at: 90, valid_until: 120 },
				},
				100,
			).label,
		).toBe("已停止");
	});
	it("does not turn a failed list refresh into offline", () => {
		const m = {
			...item,
			runtime: { source: "spark", state: "ready", observed_at: 90, valid_until: 120 },
		};
		expect(modelRuntimePresentation(m, 100, true)).toMatchObject({
			source: "Spark",
			label: "未知",
		});
	});
});
