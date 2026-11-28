import { describe, expect, it } from "vitest";
import { baseName, isSafeRelative, joinPath, parentPath } from "./paths";

describe("isSafeRelative", () => {
	it("接受普通相对路径", () => {
		expect(isSafeRelative("src/app.ts")).toBe(true);
		expect(isSafeRelative("设计/首页 mock.png")).toBe(true);
	});
	it("拒绝空路径、绝对路径与上跳段", () => {
		expect(isSafeRelative("")).toBe(false);
		expect(isSafeRelative("/etc/passwd")).toBe(false);
		expect(isSafeRelative("C:\\Windows")).toBe(false);
		expect(isSafeRelative("../secret")).toBe(false);
		expect(isSafeRelative("src/../../secret")).toBe(false);
	});
});

describe("joinPath", () => {
	it("根目录直接返回名字", () => {
		expect(joinPath("", "src")).toBe("src");
	});
	it("去掉目录项尾部斜杠", () => {
		expect(joinPath("src", "app/")).toBe("src/app");
	});
	it("保留中文与空格", () => {
		expect(joinPath("", "设计/首页 mock.png")).toBe("设计/首页 mock.png");
	});
});

describe("parentPath / baseName", () => {
	it("逐级回退", () => {
		expect(parentPath("src/app/main.ts")).toBe("src/app");
		expect(parentPath("src")).toBe("");
		expect(parentPath("")).toBe("");
	});
	it("取最后一段并兼容反斜杠", () => {
		expect(baseName("src/app/main.ts")).toBe("main.ts");
		expect(baseName("src\\app\\main.ts")).toBe("main.ts");
	});
});
