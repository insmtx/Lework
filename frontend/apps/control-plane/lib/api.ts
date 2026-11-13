import { createApiClient } from "@lework/api";

export { ApiError } from "@lework/api";

// 浏览器与控制面同源，无需 baseUrl 与鉴权头；保留具名导出让既有调用点不变。
const client = createApiClient();

export const {
	listNodes,
	getNodeHealth,
	startOpenCode,
	restartOpenCode,
	stopOpenCode,
	listSessions,
	createSession,
	renameSession,
	deleteSession,
	listMessages,
	sendPrompt,
	interruptSession,
	replyPermission,
	listPermissions,
	sessionDiff,
	listFiles,
	readFile,
	createPTY,
	closePTY,
} = client;
