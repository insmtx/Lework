import { describe, expect, it } from "vitest";

import { darkTheme, lightTheme, resolveScheme, themeFor } from "./theme";

/** relativeLuminance 按 WCAG 计算相对亮度，用于检查两套主题的可读性。 */
function relativeLuminance(hex: string): number {
	const value = hex.replace("#", "");
	const normalized =
		value.length === 3
			? value
					.split("")
					.map((char) => char + char)
					.join("")
			: value;
	const [r, g, b] = [0, 2, 4].map((start) => {
		const channel = Number.parseInt(normalized.slice(start, start + 2), 16) / 255;
		return channel <= 0.03928 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4;
	});
	return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

function contrastRatio(foreground: string, background: string): number {
	const lighter = Math.max(relativeLuminance(foreground), relativeLuminance(background));
	const darker = Math.min(relativeLuminance(foreground), relativeLuminance(background));
	return (lighter + 0.05) / (darker + 0.05);
}

describe("themeFor", () => {
	it("系统是暗色时返回暗色主题", () => {
		expect(themeFor("dark")).toBe(darkTheme);
	});

	it("系统是亮色或未知时返回亮色主题", () => {
		expect(themeFor("light")).toBe(lightTheme);
		expect(themeFor(null)).toBe(lightTheme);
		expect(themeFor(undefined)).toBe(lightTheme);
	});

	it("两套主题的 scheme 与实际主题一致", () => {
		expect(lightTheme.scheme).toBe("light");
		expect(darkTheme.scheme).toBe("dark");
	});
});

describe("resolveScheme", () => {
	it("跟随系统时采用系统配色，未知按亮色", () => {
		expect(resolveScheme("system", "dark")).toBe("dark");
		expect(resolveScheme("system", "light")).toBe("light");
		expect(resolveScheme("system", null)).toBe("light");
	});

	it("手动选择优先于系统", () => {
		expect(resolveScheme("light", "dark")).toBe("light");
		expect(resolveScheme("dark", "light")).toBe("dark");
	});
});

describe("主题可读性", () => {
	for (const theme of [lightTheme, darkTheme]) {
		describe(theme.scheme, () => {
			it("正文与页面背景达到 4.5:1", () => {
				expect(contrastRatio(theme.colors.text, theme.colors.background)).toBeGreaterThanOrEqual(
					4.5,
				);
			});

			it("次要文字与页面背景达到 4.5:1", () => {
				expect(
					contrastRatio(theme.colors.textSecondary, theme.colors.background),
				).toBeGreaterThanOrEqual(4.5);
			});

			it("时间戳等弱化文字仍达到 3:1", () => {
				expect(
					contrastRatio(theme.colors.textMuted, theme.colors.background),
				).toBeGreaterThanOrEqual(3);
			});

			it("用户气泡文字与气泡背景达到 4.5:1", () => {
				expect(
					contrastRatio(theme.colors.userBubbleText, theme.colors.userBubble),
				).toBeGreaterThanOrEqual(4.5);
			});

			it("助手气泡在页面里可辨认", () => {
				expect(contrastRatio(theme.colors.text, theme.colors.messageBubble)).toBeGreaterThanOrEqual(
					4.5,
				);
			});

			it("代码文字与代码块背景达到 4.5:1", () => {
				expect(
					contrastRatio(theme.colors.codeText, theme.colors.codeBackground),
				).toBeGreaterThanOrEqual(4.5);
			});

			it("行内代码文字与背景达到 4.5:1", () => {
				expect(
					contrastRatio(theme.colors.inlineCodeText, theme.colors.inlineCodeBackground),
				).toBeGreaterThanOrEqual(4.5);
			});

			it("主按钮文字与按钮背景达到 4.5:1", () => {
				expect(contrastRatio(theme.colors.actionText, theme.colors.action)).toBeGreaterThanOrEqual(
					4.5,
				);
			});

			it("链接色与页面背景达到 4.5:1", () => {
				expect(contrastRatio(theme.colors.accent, theme.colors.background)).toBeGreaterThanOrEqual(
					4.5,
				);
			});

			it("错误与成功状态文字达到 4.5:1", () => {
				expect(contrastRatio(theme.colors.danger, theme.colors.background)).toBeGreaterThanOrEqual(
					4.5,
				);
				expect(contrastRatio(theme.colors.success, theme.colors.background)).toBeGreaterThanOrEqual(
					4.5,
				);
			});
		});
	}
});
