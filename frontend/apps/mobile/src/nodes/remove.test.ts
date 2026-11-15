import { ApiError, type NodeView } from "@lework/api";
import { describe, expect, it } from "vitest";

import {
	describeRemoveNode,
	removeErrorMessage,
	visibleNodes,
	withoutNode,
} from "./remove";

describe("describeRemoveNode", () => {
	it("说清代价", () => {
		expect(describeRemoveNode("MacBook")).toContain("lework agent --reset");
		expect(describeRemoveNode("MacBook")).toContain("MacBook");
	});
});

describe("visibleNodes", () => {
	it("过滤掉未归属的历史节点", () => {
		const nodes = [
			{ id: "a", claimed: true, connected: true },
			{ id: "b", claimed: false, connected: true },
			// 老控制面没有 claimed 字段
			{ id: "c", connected: false },
		] as NodeView[];
		expect(visibleNodes(nodes).map((node) => node.id)).toEqual(["a", "c"]);
	});
});

describe("withoutNode", () => {
	it("移除后不留下该节点", () => {
		const nodes = [{ id: "a" }, { id: "b" }] as NodeView[];
		expect(withoutNode(nodes, "a").map((node) => node.id)).toEqual(["b"]);
	});
});

describe("removeErrorMessage", () => {
	it("区分 404 / 401 / 403 / 其他", () => {
		expect(removeErrorMessage(new ApiError(404, "not_found", ""))).toBeNull();
		expect(removeErrorMessage(new ApiError(401, "unauthorized", ""))).toBe("登录已过期，请重新登录");
		expect(removeErrorMessage(new ApiError(403, "forbidden", ""))).toBe(
			"只有节点的所有者可以移除它",
		);
		expect(removeErrorMessage(new TypeError("Network request failed"))).toBe(
			"网络不通，请检查服务器地址",
		);
		expect(removeErrorMessage(new ApiError(500, "internal_error", ""))).toBe("移除失败，请稍后重试");
	});
});
