import { cloneElement, isValidElement, type ReactElement, type ReactNode } from "react";
import { Text } from "react-native";
import type { RenderFunction, RenderRules } from "react-native-markdown-display";

// selectable 会吞掉 Text 的 onPress，链接所在节点必须保持可点击。
function hasPressHandler(node: ReactNode): boolean {
	if (Array.isArray(node)) return node.some(hasPressHandler);
	if (!isValidElement(node)) return false;
	const props = node.props as { children?: ReactNode; onPress?: unknown };
	return typeof props.onPress === "function" || hasPressHandler(props.children);
}

const wrap =
	(rule: RenderFunction): RenderFunction =>
	(node, children, parent, styles, styleObj, ...args) => {
		const rendered = rule(node, children, parent, styles, styleObj, ...args);
		// 只有 Text 接受 selectable；给 View 加会触发 RN 属性告警。
		if (!isValidElement(rendered) || rendered.type !== Text || hasPressHandler(rendered)) {
			return rendered;
		}
		return cloneElement(rendered as ReactElement<{ selectable?: boolean }>, { selectable: true });
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
