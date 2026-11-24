import { createElement, type ReactElement } from "react";
import { describe, expect, it, vi } from "vitest";

// node 环境不加载 RN 运行时；Text 用字符串即可比对 element.type。
vi.mock("react-native", () => ({ Text: "Text" }));

import type { ASTNode } from "react-native-markdown-display";
import { withSelectableText } from "./selectable-rules";

const node = {
	type: "text",
	sourceType: "text",
	key: "k",
	content: "hi",
	markup: "",
	tokenIndex: 0,
	index: 0,
	attributes: {},
	children: [],
} as ASTNode;

describe("withSelectableText", () => {
	it("给 Text 补 selectable，View 保持原样", () => {
		const rules = withSelectableText({
			text: () => createElement("Text", { style: {} }, "hi"),
			body: () => createElement("View", null, null),
		});

		const text = rules.text!(node, [], [], {}, undefined) as ReactElement<{
			selectable?: boolean;
		}>;
		const body = rules.body!(node, [], [], {}, undefined) as ReactElement<{
			selectable?: boolean;
		}>;

		expect(text.props.selectable).toBe(true);
		expect(body.props.selectable).toBeUndefined();
	});

	it("含链接的 Text 不加 selectable，保留点击", () => {
		const rules = withSelectableText({
			paragraph: () =>
				createElement("Text", { style: {} }, [
					createElement("Text", { key: "link", onPress: vi.fn(), style: {} }, "docs"),
				]),
		});

		const paragraph = rules.paragraph!(node, [], [], {}, undefined) as ReactElement<{
			selectable?: boolean;
		}>;

		expect(paragraph.props.selectable).toBeUndefined();
	});
});
