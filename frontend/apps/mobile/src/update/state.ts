// 更新流程的纯状态机：UI 与下载/安装代码只 dispatch UpdateAction，不直接改状态。
// reduceUpdateState 对非法迁移一律原样返回（不抛错）。error 保留出错前的版本与
// 文件信息：version 与 downloadUrl 齐全时 download-started 视为原地重试，dismissed 则是
// 取消下载（下载层负责 abort 真实任务）。
export type UpdateState =
	| { phase: "idle" }
	| { phase: "checking" }
	| { phase: "available"; version: string; downloadUrl: string }
	| { phase: "downloading"; version: string; downloadUrl: string; progress: number }
	| { phase: "downloaded"; version: string; downloadUrl: string; fileUri: string }
	| { phase: "installing"; version: string; fileUri: string }
	| { phase: "error"; message: string; version?: string; downloadUrl?: string; fileUri?: string };

export type UpdateAction =
	| { type: "check-started" }
	| { type: "update-available"; version: string; downloadUrl: string }
	| { type: "no-update" }
	| { type: "download-started" }
	| { type: "download-progress"; progress: number }
	| { type: "download-finished"; fileUri: string }
	| { type: "install-started" }
	| { type: "failed"; message: string }
	| { type: "dismissed" };

export function reduceUpdateState(state: UpdateState, action: UpdateAction): UpdateState {
	switch (action.type) {
		case "check-started":
			// 检查只允许从非 busy 阶段发起；重新检查会丢弃旧的 available 信息。
			return isBusy(state) ? state : { phase: "checking" };
		case "update-available":
			// busy 阶段不允许中途替换目标版本，其余阶段（含 idle 直推）接受新版本。
			return isBusy(state)
				? state
				: { phase: "available", version: action.version, downloadUrl: action.downloadUrl };
		case "no-update":
			return state.phase === "checking" ? { phase: "idle" } : state;
		case "download-started":
			// 首次下载只从 available；error 若仍持有完整版本信息则走原地重试，
			// 转入 downloading 并清零进度（message 随状态替换自然消失）。
			if (state.phase === "available") {
				return {
					phase: "downloading",
					version: state.version,
					downloadUrl: state.downloadUrl,
					progress: 0,
				};
			}
			if (state.phase === "error" && state.version && state.downloadUrl) {
				return {
					phase: "downloading",
					version: state.version,
					downloadUrl: state.downloadUrl,
					progress: 0,
				};
			}
			return state;
		case "download-progress":
			if (state.phase !== "downloading") return state;
			return { ...state, progress: clampProgress(action.progress) };
		case "download-finished":
			return state.phase === "downloading"
				? {
						phase: "downloaded",
						version: state.version,
						downloadUrl: state.downloadUrl,
						fileUri: action.fileUri,
					}
				: state;
		case "install-started":
			// 安装只需文件本身，不再携带 downloadUrl；弹窗关闭（isUpdateVisible=false）。
			return state.phase === "downloaded"
				? { phase: "installing", version: state.version, fileUri: state.fileUri }
				: state;
		case "failed":
			return toError(state, action.message);
		case "dismissed":
			// 关闭弹窗回到 idle：available 时是用户跳过（持久化在 update-context，
			// 之后同一版本不再提示）；downloading 时是「取消下载」入口，调用方需同时
			// abort 真实下载任务；installing 由系统安装器接管，此处不可中断。
			return state.phase === "available" ||
				state.phase === "downloading" ||
				state.phase === "downloaded" ||
				state.phase === "error"
				? { phase: "idle" }
				: state;
	}
}

export function isUpdateVisible(state: UpdateState): boolean {
	switch (state.phase) {
		case "available":
		case "downloading":
		case "downloaded":
		case "error":
			return true;
		case "idle":
		case "checking":
		case "installing":
			return false;
	}
}

function isBusy(state: UpdateState): boolean {
	return state.phase === "downloading" || state.phase === "installing";
}

// 进度按整数百分比展示：四舍五入后夹在 0–100，防止异常上报穿透界面。
function clampProgress(progress: number): number {
	return Math.min(100, Math.max(0, Math.round(progress)));
}

function toError(state: UpdateState, message: string): UpdateState {
	switch (state.phase) {
		case "idle":
		case "error":
			return state;
		case "checking":
			return { phase: "error", message };
		case "available":
		case "downloading":
			return { phase: "error", message, version: state.version, downloadUrl: state.downloadUrl };
		case "downloaded":
			return {
				phase: "error",
				message,
				version: state.version,
				downloadUrl: state.downloadUrl,
				fileUri: state.fileUri,
			};
		case "installing":
			return { phase: "error", message, version: state.version, fileUri: state.fileUri };
	}
}
