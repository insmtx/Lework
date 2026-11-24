import { describe, expect, it } from "vitest";
import { routeFromNotification } from "./route";

describe("routeFromNotification", () => {
	it("完整负载直达审批所在的会话", () => {
		expect(
			routeFromNotification({ node: "node_1", session: "ses_1", permission_id: "perm_1" }),
		).toBe("/session/node_1/ses_1?permission=perm_1");
	});

	it("缺少 permission_id 时落到会话页", () => {
		expect(routeFromNotification({ node: "node_1", session: "ses_1" })).toBe(
			"/session/node_1/ses_1",
		);
	});

	it("带 kind 的完成推送仍路由到会话页", () => {
		// kind 只用于文案与将来的行为区分，跳转只看 node + session。
		expect(routeFromNotification({ node: "node_1", session: "ses_1", kind: "done" })).toBe(
			"/session/node_1/ses_1",
		);
		expect(routeFromNotification({ node: "node_1", session: "ses_1", kind: "failed" })).toBe(
			"/session/node_1/ses_1",
		);
	});

	it("缺少 node 或 session 时不构造深链", () => {
		expect(routeFromNotification({ node: "node_1" })).toBeNull();
		expect(routeFromNotification({ session: "ses_1" })).toBeNull();
		expect(routeFromNotification({ node: 1, session: "ses_1" })).toBeNull();
		expect(routeFromNotification(null)).toBeNull();
		expect(routeFromNotification("node_1/ses_1")).toBeNull();
	});

	it("对路径段与查询参数编码", () => {
		expect(
			routeFromNotification({ node: "node/1", session: "ses 1", permission_id: "perm&1" }),
		).toBe("/session/node%2F1/ses%201?permission=perm%261");
	});

	it("非法 permission_id 视为缺失", () => {
		expect(routeFromNotification({ node: "node_1", session: "ses_1", permission_id: 7 })).toBe(
			"/session/node_1/ses_1",
		);
	});
});

describe("表单推送深链", () => {
	it("表单推送直达会话并带 form 参数", () => {
		expect(
			routeFromNotification({ node: "node_1", session: "ses_1", form_id: "frm_1" }),
		).toBe("/session/node_1/ses_1?form=frm_1");
	});

	it("global 表单落到待处理页", () => {
		expect(
			routeFromNotification({ node: "node_1", session: "global", form_id: "frm_1", global: true }),
		).toBe("/pending");
	});

	it("form_id 优先于 permission_id", () => {
		expect(
			routeFromNotification({
				node: "node_1",
				session: "ses_1",
				permission_id: "perm_1",
				form_id: "frm_1",
			}),
		).toBe("/session/node_1/ses_1?form=frm_1");
	});
});
