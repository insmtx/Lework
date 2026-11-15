import type { ToolTone } from "@lework/tool-view";

/** ToolCardVisibility 是决定工具卡片展开/折叠所需的全部输入。 */
export interface ToolCardVisibility {
	tone: ToolTone;
	/** 这张卡片之后是否还有内容（同一消息的后续工具或后续消息）。 */
	hasFollowingContent: boolean;
	/** 用户偏好「工具卡片默认展开」。 */
	preferExpanded: boolean;
	/** 用户是否手动点过这张卡片；手动选择优先于自动行为。 */
	manual: boolean;
	/** 当前实际展开状态，手动模式下原样返回。 */
	expanded: boolean;
	/** 本次挂载期间是否见过它处于运行中，用来区分历史消息与新执行。 */
	ranDuringMount: boolean;
}

/**
 * resolveExpanded 决定工具卡片下一帧是否展开。
 *
 * 运行中的工具强制展开，让流式输出和 loading 可见；刚执行完且仍处在会话
 * 末尾时保持展开，等下一段内容出现再自动折叠。打开历史会话时，早就完成的
 * 卡片仍按用户偏好保持折叠，不会一屏全是展开的工具输出。
 */
export function resolveExpanded(state: ToolCardVisibility): boolean {
	if (state.manual) return state.expanded;
	if (state.tone === "running") return true;
	if (state.preferExpanded) return true;
	if (!state.ranDuringMount) return false;
	return !state.hasFollowingContent;
}
