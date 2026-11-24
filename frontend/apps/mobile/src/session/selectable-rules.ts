import { cloneElement, isValidElement, type ReactElement } from "react";
import { Text } from "react-native";
import type { RenderFunction, RenderRules } from "react-native-markdown-display";

const wrap =
	(rule: RenderFunction): RenderFunction =>
	(node, children, parent, styles, styleObj, ...args) => {
		const rendered = rule(node, children, parent, styles, styleObj, ...args);
		// 只有 Text 接受 selectable；给 View 加会触发 RN 属性告警。
		return isValidElement(rendered) && rendered.type === Text
			? cloneElement(rendered as ReactElement<{ selectable?: boolean }>, { selectable: true })
			: rendered;
	};

/**
 * withSelectableText 给 Markdown 默认规则产出的 Text 补上 selectable。
 *
 * 移动端默认不能长按选中文本，而回复正文全是 Markdown 渲染的 Text；
 * 逐条重写规则容易漏掉列表、表格等分支，这里统一包一层。
 */
export function withSelectableText(rules: RenderRules): RenderRules {
	const wrapped: RenderRules = {};
	for (const [name, rule] of Object.entries(rules)) {
		if (rule) wrapped[name] = wrap(rule);
	}
	return wrapped;
}
