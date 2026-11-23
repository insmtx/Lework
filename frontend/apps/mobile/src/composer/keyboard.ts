/** KeyboardOverlapInput 描述计算输入栏避让高度所需的现场信息。 */
export interface KeyboardOverlapInput {
	/** 当前键盘高度；键盘不可见时为 undefined。 */
	keyboardHeight: number | undefined;
	/** 输入栏当前已经留出的底部高度（安全区或最小留白）。 */
	baseBottom: number;
	/** 平台：iOS 的键盘高度已包含底部安全区，Android 的已扣掉系统导航栏。 */
	platform: string;
}

/**
 * keyboardOverlap 计算输入栏为了不被输入法盖住，还需要补的底部高度。
 *
 * 只依赖键盘高度，不依赖页面坐标或键盘事件是否刚好在挂载后到达：这样从
 * 键盘已经打开的页面跳进来（例如首页发送后直接打开会话页）也能正确避让。
 */
export function keyboardOverlap(input: KeyboardOverlapInput): number {
	const { keyboardHeight, baseBottom, platform } = input;
	if (keyboardHeight === undefined || keyboardHeight <= 0) return 0;
	// iOS 的键盘高度从屏幕底部算起，已含 Home Indicator；输入栏本身已经垫了
	// baseBottom，这里只补差额，否则会多出一段空白。Android 的键盘高度不含
	// 系统导航栏，需要整段补上。
	const extra = platform === "ios" ? keyboardHeight - baseBottom : keyboardHeight;
	return Math.max(0, extra);
}
