import {
	cloneElement,
	createElement,
	isValidElement,
	type ReactElement,
	type ReactNode,
} from "react";
import { Text, type TextStyle } from "react-native";
import type { RenderFunction, RenderRules } from "react-native-markdown-display";

import { findFilePathSpans } from "./file-paths";

type ResolvePath = (token: string) => string | null;
type OpenRoute = (route: string) => void;
type LinkStyles = { link?: TextStyle };

function linkParts(
	content: string,
	resolve: ResolvePath,
	open: OpenRoute,
	styles: LinkStyles,
): ReactNode[] | null {
	const spans = findFilePathSpans(content);
	if (spans.length === 0) {
		return null;
	}
	const parts: ReactNode[] = [];
	let cursor = 0;
	for (const [index, span] of spans.entries()) {
		const route = resolve(span.path);
		if (!route) continue;
		if (span.start > cursor) {
			parts.push(content.slice(cursor, span.start));
		}
		parts.push(
			createElement(
				Text,
				{ key: `file-path-${index}`, style: styles.link, onPress: () => open(route) },
				span.path,
			),
		);
		cursor = span.end;
	}
	if (parts.length === 0) {
		return null;
	}
	if (cursor < content.length) {
		parts.push(content.slice(cursor));
	}
	return parts;
}

const wrap =
	(rule: RenderFunction, resolve: ResolvePath, open: OpenRoute): RenderFunction =>
	(node, children, parentNodes, styles, styleObj, ...args) => {
		const rendered = rule(node, children, parentNodes, styles, styleObj, ...args);
		// 链接标签里的文字属于链接本身，不能抢掉它的跳转。
		if (
			!isValidElement(rendered) ||
			typeof node.content !== "string" ||
			parentNodes.some((parent) => parent.type === "link")
		) {
			return rendered;
		}
		const parts = linkParts(node.content, resolve, open, styles);
		if (!parts) {
			return rendered;
		}
		return cloneElement(rendered as ReactElement<{ children?: ReactNode }>, { children: parts });
	};

/**
 * withFilePathLinks 让 text / code_inline 里的裸文件路径可点。
 *
 * 只替换这两条规则：fence / code_block（代码块）和 link（链接标签）里的路径
 * 保持原样，避免改变既有跳转与复制行为。
 */
export function withFilePathLinks(
	rules: RenderRules,
	resolve: ResolvePath,
	open: OpenRoute,
): RenderRules {
	return {
		...rules,
		text: rules.text ? wrap(rules.text, resolve, open) : rules.text,
		code_inline: rules.code_inline ? wrap(rules.code_inline, resolve, open) : rules.code_inline,
	};
}
