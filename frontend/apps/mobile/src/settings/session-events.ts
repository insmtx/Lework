/**
 * 会话过期通知器。
 *
 * API 客户端不依赖 React，拿不到 router 或设置存储；外壳（AppShell）持有两者，
 * 但在挂载前客户端就可能已经发出请求。模块级单个回调把两者解耦：客户端只管
 * 「喊一声」，外壳决定「清会话 + 回登录页」。只保留一个接收者——外壳是唯一
 * 需要处理 401 的地方，多播只会造成重复跳转。
 */
type SessionExpiredHandler = () => void;

let handler: SessionExpiredHandler | null = null;

/** setSessionExpiredHandler 注册接收者，返回取消函数（供 useEffect 清理）。 */
export function setSessionExpiredHandler(next: SessionExpiredHandler | null): () => void {
	handler = next;
	return () => {
		// 只在仍是自己时清空：卸载晚于新注册者的重挂载时，不能把新接收者清掉。
		if (handler === next) handler = null;
	};
}

/** notifySessionExpired 由 API 客户端在收到 401 时调用；无接收者时为空操作。 */
export function notifySessionExpired(): void {
	handler?.();
}

/** resetSessionExpiredHandlerForTests 清空接收者；生产代码不要调用。 */
export function resetSessionExpiredHandlerForTests(): void {
	handler = null;
}
