import { describe, expect, it } from "vitest";
import { LatestRequestMap } from "./latest-request";

describe("LatestRequestMap", () => {
	it("新请求让同一节点的旧请求过期", () => {
		const requests = new LatestRequestMap();
		const first = requests.begin("node-a");
		const second = requests.begin("node-a");

		expect(requests.isCurrent("node-a", first)).toBe(false);
		expect(requests.isCurrent("node-a", second)).toBe(true);
	});

	it("不同节点互不影响", () => {
		const requests = new LatestRequestMap();
		const nodeA = requests.begin("node-a");
		const nodeB = requests.begin("node-b");

		expect(requests.isCurrent("node-a", nodeA)).toBe(true);
		expect(requests.isCurrent("node-b", nodeB)).toBe(true);
	});

	it("没有开始过的节点不接受任何 token", () => {
		const requests = new LatestRequestMap();
		expect(requests.isCurrent("node-a", 1)).toBe(false);
	});
});
