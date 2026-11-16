import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

import { brand, cssVariableByToken, leworkLight, radii } from "./index";

const css = readFileSync(new URL("../../styles/globals.css", import.meta.url), "utf8");

/** cssVariable 从根选择器里读出变量值，读不到时返回 null。 */
function cssVariable(name: string): string | null {
	const match = css.match(new RegExp(`${name}:\\s*([^;]+);`));
	return match?.[1]?.trim() ?? null;
}

describe("web token parity", () => {
	for (const [token, cssName] of Object.entries(cssVariableByToken)) {
		it(`${token} 与 ${cssName} 一致`, () => {
			const value = cssVariable(cssName);
			expect(value, `${cssName} 未在 packages/styles/globals.css 定义`).not.toBeNull();
			expect(value?.toLowerCase()).toBe(
				leworkLight[token as keyof typeof leworkLight].toLowerCase(),
			);
		});
	}

	it("圆角与 Web token 一致", () => {
		expect(cssVariable("--lework-radius-sm")).toBe(`${radii.sm}px`);
		expect(cssVariable("--lework-radius-md")).toBe(`${radii.md}px`);
		expect(cssVariable("--lework-radius-lg")).toBe(`${radii.lg}px`);
	});

	it("品牌主色引用唯一的 indigo", () => {
		expect(leworkLight.primary).toBe(brand.indigo500);
		expect(leworkLight.sidebarBg).toBe(brand.indigo50);
		expect(leworkLight.primarySoft).toBe(brand.indigo100);
		expect(leworkLight.primarySofter).toBe(brand.indigo150);
	});
});
