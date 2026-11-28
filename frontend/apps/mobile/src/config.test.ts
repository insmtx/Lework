import { describe, expect, it } from "vitest";
import { APP_SCHEME, filesRoute, sessionRoute } from "./config";

describe("sessionRoute", () => {
	it("构造会话路径", () => {
		expect(sessionRoute("node_1", "ses_1")).toBe("/session/node_1/ses_1");
	});

	it("对路径段做编码", () => {
		expect(sessionRoute("node/1", "ses 1")).toBe("/session/node%2F1/ses%201");
	});

	it("深链 scheme 固定", () => {
		expect(APP_SCHEME).toBe("lework");
	});
});

describe("filesRoute", () => {
	it("把整个项目 key 编码成一个路径段", () => {
		expect(filesRoute("global:/home/me/repo")).toBe("/files/global%3A%2Fhome%2Fme%2Frepo");
	});
});
