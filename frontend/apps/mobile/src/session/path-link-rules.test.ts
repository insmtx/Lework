import { createElement, type ReactElement, type ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

// node 环境不加载 RN 运行时；Text 用字符串即可比对 element.type。
vi.mock("react-native", () => ({ Text: "Text" }));

import type { ASTNode, RenderRules } from "react-native-markdown-display";
import { withFilePathLinks } from "./path-link-rules";
import { withSelectableText } from "./selectable-rules";

function node(content: string, type = "text"): ASTNode {
	return {
		type,
		sourceType: type,
		key: `k-${type}`,
		content,
		markup: "",
		tokenIndex: 0,
		index: 0,
		attributes: {},
		children: [],
	} as ASTNode;
}

const styles = { link: { color: "accent" } };

function plainRules(): RenderRules {
	return {
		text: (n) => createElement("Text", { style: {} }, n.content),
		code_inline: (n) => createElement("Text", { style: {}, key: n.key }, n.content),
		fence: () => createElement("Text", null, "fence"),
		code_block: () => createElement("Text", null, "block"),
	};
}

function pressable(
	node: ReactNode,
): ReactElement<{ children?: ReactNode; onPress?: () => void }> | null {
	if (Array.isArray(node)) {
		for (const child of node) {
			const found = pressable(child);
			if (found) return found;
		}
		return null;
	}
	if (!node || typeof node !== "object" || !("props" in node)) return null;
	const element = node as ReactElement<{ children?: ReactNode; onPress?: () => void }>;
	if (typeof element.props.onPress === "function") return element;
	return pressable(element.props.children);
}

describe("withFilePathLinks", () => {
	it("text 节点命中路径时渲染可点片段", () => {
		const open = vi.fn();
		const resolve = vi.fn(() => "/file/n1?path=docs%2Fa.md");
		const rules = withFilePathLinks(plainRules(), resolve, open);

		const rendered = rules.text!(
			node("看 docs/a.md 吧"),
			[],
			[],
			styles,
			undefined,
		) as ReactElement<{ children?: ReactNode }>;

		expect(resolve).toHaveBeenCalledWith("docs/a.md");
		const link = pressable(rendered.props.children);
		expect(link).not.toBeNull();
		expect(link!.props.children).toBe("docs/a.md");
		link!.props.onPress!();
		expect(open).toHaveBeenCalledWith("/file/n1?path=docs%2Fa.md");
	});

	it("code_inline 节点命中路径时可点", () => {
		const open = vi.fn();
		const rules = withFilePathLinks(plainRules(), () => "/file/n1", open);

		const rendered = rules.code_inline!(
			node("src/b.go", "code_inline"),
			[],
			[],
			styles,
			undefined,
		) as ReactElement<{ children?: ReactNode }>;

		const link = pressable(rendered.props.children);
		expect(link).not.toBeNull();
		expect(link!.props.children).toBe("src/b.go");
	});

	it("链接标签里的路径不抢跳转", () => {
		const open = vi.fn();
		const rules = withFilePathLinks(plainRules(), () => "/file/n1", open);

		const rendered = rules.text!(
			node("docs/a.md"),
			[],
			[node("link", "link")],
			styles,
			undefined,
		) as ReactElement<{ children?: ReactNode }>;

		expect(rendered.props.children).toBe("docs/a.md");
		expect(pressable(rendered.props.children)).toBeNull();
		expect(open).not.toHaveBeenCalled();
	});

	it("不含路径的文本保持原样", () => {
		const rules = withFilePathLinks(plainRules(), () => "/file/n1", vi.fn());

		const rendered = rules.text!(node("普通一句话"), [], [], styles, undefined) as ReactElement<{
			children?: ReactNode;
		}>;

		expect(rendered.props.children).toBe("普通一句话");
	});

	it("resolve 返回 null 的路径保留普通文本", () => {
		const open = vi.fn();
		const rules = withFilePathLinks(plainRules(), () => null, open);

		const rendered = rules.text!(node("看 docs/a.md"), [], [], styles, undefined) as ReactElement<{
			children?: ReactNode;
		}>;

		expect(rendered.props.children).toBe("看 docs/a.md");
		expect(open).not.toHaveBeenCalled();
	});

	it("fence 与 code_block 不包装", () => {
		const base = plainRules();
		const rules = withFilePathLinks(base, () => "/file/n1", vi.fn());

		expect(rules.fence).toBe(base.fence);
		expect(rules.code_block).toBe(base.code_block);
	});

	it("多个路径与标点拆分后原文完整保留", () => {
		const content = "（docs/a.md），另见 src/b.go。";
		const rules = withFilePathLinks(plainRules(), (token) => `/file/${token}`, vi.fn());

		const rendered = rules.text!(node(content), [], [], styles, undefined) as ReactElement<{
			children?: ReactNode;
		}>;

		const parts = rendered.props.children as ReactNode[];
		const text = parts
			.map((part) =>
				typeof part === "string"
					? part
					: (part as ReactElement<{ children?: ReactNode }>).props.children,
			)
			.join("");
		expect(text).toBe(content);
	});

	it("先包路径再套 selectable：含路径节点不加 selectable", () => {
		const rules = withSelectableText(withFilePathLinks(plainRules(), () => "/file/n1", vi.fn()));

		const withPath = rules.text!(node("见 docs/a.md"), [], [], styles, undefined) as ReactElement<{
			selectable?: boolean;
		}>;
		const plain = rules.text!(node("普通一句话"), [], [], styles, undefined) as ReactElement<{
			selectable?: boolean;
		}>;

		expect(withPath.props.selectable).toBeUndefined();
		expect(plain.props.selectable).toBe(true);
	});
});
