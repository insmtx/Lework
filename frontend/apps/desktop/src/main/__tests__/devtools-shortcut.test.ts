import type { Input } from "electron";
import { describe, expect, it } from "vitest";
import { isProductionDevToolsShortcut, productionDevToolsAccelerator } from "../devtools-shortcut";

function createInput(overrides: Partial<Input> = {}): Input {
	return {
		alt: true,
		code: "KeyI",
		control: false,
		isAutoRepeat: false,
		isComposing: false,
		key: "i",
		location: 0,
		meta: true,
		modifiers: [],
		shift: true,
		type: "keyDown",
		...overrides,
	};
}

describe("isProductionDevToolsShortcut", () => {
	it("macOS 仅匹配 Command+Option+Shift+I 的单次按下", () => {
		expect(productionDevToolsAccelerator).toBe("CommandOrControl+Alt+Shift+I");
		expect(isProductionDevToolsShortcut(createInput(), "darwin")).toBe(true);
		expect(isProductionDevToolsShortcut(createInput({ control: true }), "darwin")).toBe(false);
		expect(isProductionDevToolsShortcut(createInput({ shift: false }), "darwin")).toBe(false);
		expect(isProductionDevToolsShortcut(createInput({ isAutoRepeat: true }), "darwin")).toBe(false);
		expect(isProductionDevToolsShortcut(createInput({ type: "keyUp" }), "darwin")).toBe(false);
	});

	it("非 macOS 使用 Ctrl+Alt+Shift+I", () => {
		const input = createInput({ control: true, meta: false });

		expect(isProductionDevToolsShortcut(input, "win32")).toBe(true);
		expect(isProductionDevToolsShortcut(input, "linux")).toBe(true);
		expect(isProductionDevToolsShortcut({ ...input, control: false }, "win32")).toBe(false);
		expect(isProductionDevToolsShortcut({ ...input, alt: false }, "win32")).toBe(false);
		expect(isProductionDevToolsShortcut({ ...input, shift: false }, "linux")).toBe(false);
		expect(isProductionDevToolsShortcut({ ...input, meta: true }, "linux")).toBe(false);
		expect(isProductionDevToolsShortcut({ ...input, code: "KeyJ" }, "win32")).toBe(false);
		expect(isProductionDevToolsShortcut({ ...input, type: "keyUp" }, "linux")).toBe(false);
		expect(isProductionDevToolsShortcut({ ...input, isAutoRepeat: true }, "win32")).toBe(false);
	});
});
