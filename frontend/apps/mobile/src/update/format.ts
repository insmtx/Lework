import type { UpdateState } from "./state";

/**
 * describeVersion 生成设置页「版本」行右侧的状态文案。
 *
 * idle 时只显示本机版本；其余阶段优先显示更新进展，让用户不进 Modal
 * 也能看出「检查中 / 下载中 / 待安装」。下载中的百分比只属于弹窗，
 * 这里压缩成一句状态，避免菜单行过长。
 */
export function describeVersion(currentVersion: string | null, state: UpdateState): string {
	switch (state.phase) {
		case "checking":
			return "检查中";
		case "available":
			return `${state.version} 可更新`;
		case "downloading":
			return `${state.version} 下载中`;
		case "downloaded":
			return `${state.version} 待安装`;
		case "installing":
			return "安装中";
		case "error":
			return "更新失败";
		case "idle":
			return currentVersion ?? "未知";
	}
}
