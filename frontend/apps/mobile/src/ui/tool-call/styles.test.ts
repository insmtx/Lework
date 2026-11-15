import { describe, expect, it, vi } from "vitest";

// 只用到 StyleSheet.create 的恒等行为，避免在 node 环境里加载整个 RN 运行时。
vi.mock("react-native", () => ({
	StyleSheet: { create: <T>(styles: T) => styles },
}));

import { FONT_SCALE } from "../../settings/preferences";
import { scaleFontStyles } from "../../theme/font-scale";
import { lightTheme } from "../../theme/theme";
import { createStyles } from "./styles";

describe("tool call styles", () => {
	it("基线倍率 1 保持原始尺寸", () => {
		const styles = createStyles(lightTheme);

		expect(styles.code.fontSize).toBe(12);
		expect(styles.diffLine.lineHeight).toBe(17);
		expect(styles.text.fontSize).toBe(13);
	});

	it("标准档已整体调大，工具输出不再停留在原始小字", () => {
		const base = createStyles(lightTheme);
		const standard = scaleFontStyles(base, FONT_SCALE.standard);

		expect(standard.code.fontSize).toBeGreaterThan(base.code.fontSize);
		expect(standard.text.fontSize).toBeGreaterThan(base.text.fontSize);
	});

	it("放大字号时正文、代码和 diff 一起变大", () => {
		const base = createStyles(lightTheme);
		const standard = scaleFontStyles(base, FONT_SCALE.standard);
		const large = scaleFontStyles(base, FONT_SCALE.large);

		expect(large.code.fontSize).toBeGreaterThan(standard.code.fontSize);
		expect(large.diffLine.fontSize).toBeGreaterThan(standard.diffLine.fontSize);
		expect(large.text.fontSize).toBeGreaterThan(standard.text.fontSize);
		expect(large.listItem.fontSize).toBeGreaterThan(standard.listItem.fontSize);
	});

	it("缩小字号时行高同步缩小，避免行距失衡", () => {
		const base = createStyles(lightTheme);
		const standard = scaleFontStyles(base, FONT_SCALE.standard);
		const small = scaleFontStyles(base, FONT_SCALE.small);

		expect(small.terminalOutput.lineHeight).toBeLessThan(standard.terminalOutput.lineHeight);
	});
});
