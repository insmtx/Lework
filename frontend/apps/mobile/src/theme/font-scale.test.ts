import { describe, expect, it } from "vitest";

import { FONT_SCALE } from "../settings/preferences";
import { scaleFontStyles } from "./font-scale";

const styles = {
	title: { fontSize: 16, lineHeight: 22, fontWeight: "600" as const },
	card: { padding: 10, width: 40, color: "#fff" },
	icon: { fontSize: 12 },
};

describe("scaleFontStyles", () => {
	it("倍率为 1 时原样返回，避免无意义的重建", () => {
		expect(scaleFontStyles(styles, 1)).toBe(styles);
	});

	it("字号与行高一起缩放，忽略布局尺寸", () => {
		const scaled = scaleFontStyles(styles, FONT_SCALE.large);

		expect(scaled.title.fontSize).toBe(Math.round(16 * FONT_SCALE.large));
		expect(scaled.title.lineHeight).toBe(Math.round(22 * FONT_SCALE.large));
		expect(scaled.icon.fontSize).toBe(Math.round(12 * FONT_SCALE.large));
		// 间距和颜色不跟着字体倍数变化，否则「字号」会变成整体缩放。
		expect(scaled.card).toEqual({ padding: 10, width: 40, color: "#fff" });
	});

	it("不修改原始样式对象", () => {
		scaleFontStyles(styles, FONT_SCALE.standard);

		expect(styles.title.fontSize).toBe(16);
		expect(styles.title.lineHeight).toBe(22);
	});

	it("四舍五入到整数像素，避免小数字号造成的模糊", () => {
		const scaled = scaleFontStyles({ text: { fontSize: 13, lineHeight: 19 } }, 1.15);

		expect(scaled.text.fontSize).toBe(15);
		expect(scaled.text.lineHeight).toBe(22);
	});
});
