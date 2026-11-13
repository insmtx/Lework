import type {
	Device,
	DeviceRegistration,
	FileDiff,
	FileInfo,
	FileReadResult,
	InterruptResult,
	Message,
	MessagePage,
	NodeHealth,
	NodeView,
	OverviewNode,
	PendingPermission,
	PermissionRequest,
	PersistentTerminal,
	Session,
} from "./types";

/**
 * RequestContext 描述一次请求落在哪个控制面、带什么凭据。
 *
 * baseUrl 为空表示同源（浏览器内嵌场景）；移动端传绝对地址。
 * headers 允许传函数，因为设备令牌可能在运行期刷新，逐个请求求值
 * 才能保证拿到最新值。
 */
export interface RequestContext {
	baseUrl?: string;
	headers?: Record<string, string> | (() => Record<string, string>);
}

export class ApiError extends Error {
	readonly code: string;
	readonly status: number;

	constructor(status: number, code: string, message: string) {
		super(message);
		this.name = "ApiError";
		this.status = status;
		this.code = code;
	}
}

export interface ApiClient {
	listNodes(): Promise<NodeView[]>;
	getNodeHealth(node: string): Promise<NodeHealth>;
	startOpenCode(node: string): Promise<unknown>;
	restartOpenCode(node: string): Promise<unknown>;
	stopOpenCode(node: string): Promise<unknown>;
	listSessions(node: string, directory?: string): Promise<Session[]>;
	createSession(node: string, directory: string): Promise<Session>;
	renameSession(node: string, session: string, title: string): Promise<unknown>;
	deleteSession(node: string, session: string): Promise<unknown>;
	listMessages(
		node: string,
		session: string,
		cursor?: string,
		limit?: number,
	): Promise<MessagePage>;
	sendPrompt(node: string, session: string, text: string, idempotencyKey: string): Promise<string>;
	interruptSession(node: string, session: string): Promise<InterruptResult>;
	replyPermission(
		node: string,
		session: string,
		permissionID: string,
		decision: "once" | "always" | "reject",
		message?: string,
	): Promise<unknown>;
	listPermissions(node: string, session: string): Promise<PermissionRequest[]>;
	sessionDiff(node: string, session: string, directory: string): Promise<FileDiff[]>;
	listFiles(node: string, directory: string, path: string): Promise<FileInfo[]>;
	readFile(node: string, directory: string, path: string): Promise<FileReadResult>;
	createPTY(
		node: string,
		session: string,
		directory: string,
		cols: number,
		rows: number,
	): Promise<PersistentTerminal>;
	closePTY(node: string, session: string, ptyID: string): Promise<unknown>;
	getOverview(): Promise<OverviewNode[]>;
	listPendingPermissions(node: string, session: string): Promise<PendingPermission[]>;
	registerDevice(device: DeviceRegistration): Promise<Device>;
	listDevices(): Promise<Device[]>;
	deleteDevice(id: string): Promise<unknown>;
}

function query(values: Record<string, string | number | undefined>): string {
	const params = new URLSearchParams();
	for (const [key, value] of Object.entries(values)) {
		if (value !== undefined && value !== "") params.set(key, String(value));
	}
	const encoded = params.toString();
	return encoded ? `?${encoded}` : "";
}

export function createApiClient(ctx: RequestContext = {}): ApiClient {
	const baseUrl = ctx.baseUrl ?? "";
	const resolveHeaders = (): Record<string, string> =>
		typeof ctx.headers === "function" ? ctx.headers() : (ctx.headers ?? {});

	async function request<T>(path: string, init?: RequestInit): Promise<T> {
		const headers: Record<string, string> = { ...resolveHeaders() };
		// 只有带 body 的请求才声明 JSON，避免给 GET 附上没有意义的头。
		if (init?.body) headers["Content-Type"] = "application/json";

		const response = await fetch(`${baseUrl}${path}`, { ...init, headers });
		if (!response.ok) {
			let code = "http_error";
			let message = `request failed with status ${response.status}`;
			try {
				const body = (await response.json()) as { code?: string; message?: string };
				if (body.code) code = body.code;
				if (body.message) message = body.message;
			} catch {
				// Keep the status-derived message when the body is not JSON.
			}
			throw new ApiError(response.status, code, message);
		}
		if (response.status === 204) return null as T;
		const text = await response.text();
		return (text ? JSON.parse(text) : null) as T;
	}

	return {
		listNodes: () => request<NodeView[]>("/api/nodes"),

		getNodeHealth: async (node) => {
			// 节点返回 Go 结构体的导出字段名，这里统一转成前端的小写形状。
			const raw = await request<{ Running: boolean; Mode: string; BaseURL?: string }>(
				`/api/nodes/${encodeURIComponent(node)}/health`,
			);
			return { running: raw.Running, mode: raw.Mode, base_url: raw.BaseURL ?? "" };
		},

		startOpenCode: (node) =>
			request<unknown>(`/api/nodes/${encodeURIComponent(node)}/opencode/start`, {
				method: "POST",
			}),

		restartOpenCode: (node) =>
			request<unknown>(`/api/nodes/${encodeURIComponent(node)}/opencode/restart`, {
				method: "POST",
			}),

		stopOpenCode: (node) =>
			request<unknown>(`/api/nodes/${encodeURIComponent(node)}/opencode/stop`, {
				method: "POST",
			}),

		listSessions: (node, directory) =>
			request<Session[]>(`/api/sessions${query({ node, directory })}`),

		createSession: (node, directory) =>
			request<Session>("/api/sessions", {
				method: "POST",
				body: JSON.stringify({ node_id: node, directory }),
			}),

		renameSession: (node, session, title) =>
			request<unknown>(`/api/sessions/${encodeURIComponent(node)}/${encodeURIComponent(session)}`, {
				method: "PATCH",
				body: JSON.stringify({ title }),
			}),

		deleteSession: (node, session) =>
			request<unknown>(`/api/sessions/${encodeURIComponent(node)}/${encodeURIComponent(session)}`, {
				method: "DELETE",
			}),

		listMessages: async (node, session, cursor, limit) => {
			// 老控制面把 agent 的数组直接透传；统一成 MessagePage，调用方只认一种形状。
			const body = await request<MessagePage | Message[]>(
				`/api/sessions/${encodeURIComponent(node)}/${encodeURIComponent(session)}/messages${query({ cursor, limit })}`,
			);
			return Array.isArray(body) ? { messages: body } : body;
		},

		sendPrompt: (node, session, text, idempotencyKey) =>
			request<string>(
				`/api/sessions/${encodeURIComponent(node)}/${encodeURIComponent(session)}/prompt`,
				{
					method: "POST",
					body: JSON.stringify({ text, idempotency_key: idempotencyKey }),
				},
			),

		interruptSession: (node, session) =>
			request<InterruptResult>(
				`/api/sessions/${encodeURIComponent(node)}/${encodeURIComponent(session)}/interrupt`,
				{ method: "POST" },
			),

		replyPermission: (node, session, permissionID, decision, message) =>
			request<unknown>(
				`/api/sessions/${encodeURIComponent(node)}/${encodeURIComponent(session)}/permissions/${encodeURIComponent(permissionID)}/reply`,
				{ method: "POST", body: JSON.stringify({ decision, message }) },
			),

		listPermissions: (node, session) =>
			request<PermissionRequest[]>(
				`/api/sessions/${encodeURIComponent(node)}/${encodeURIComponent(session)}/permissions`,
			),

		sessionDiff: (node, session, directory) =>
			request<FileDiff[]>(
				`/api/sessions/${encodeURIComponent(node)}/${encodeURIComponent(session)}/diff${query({ directory })}`,
			),

		listFiles: (node, directory, path) =>
			request<FileInfo[]>(
				`/api/nodes/${encodeURIComponent(node)}/files${query({ directory, path })}`,
			),

		readFile: (node, directory, path) =>
			request<FileReadResult>(
				`/api/nodes/${encodeURIComponent(node)}/files/content${query({ directory, path })}`,
			),

		createPTY: (node, session, directory, cols, rows) =>
			request<PersistentTerminal>(
				`/api/sessions/${encodeURIComponent(node)}/${encodeURIComponent(session)}/pty`,
				{
					method: "POST",
					body: JSON.stringify({ directory, cols, rows }),
				},
			),

		closePTY: (node, session, ptyID) =>
			request<unknown>(
				`/api/sessions/${encodeURIComponent(node)}/${encodeURIComponent(session)}/pty/${encodeURIComponent(ptyID)}`,
				{ method: "DELETE" },
			),

		getOverview: async () => {
			const body = await request<{ nodes?: OverviewNode[] }>("/api/overview");
			return body.nodes ?? [];
		},

		listPendingPermissions: async (node, session) => {
			// 控制面把 agent 的快照原样透传，形状是数组；事件里的卡片用 session_id，
			// 这里做一次字段归一，调用方不必区分两种来源。
			const snapshot = await request<PermissionRequest[]>(
				`/api/sessions/${encodeURIComponent(node)}/${encodeURIComponent(session)}/permissions`,
			);
			return snapshot.map((permission) => ({
				id: permission.id,
				session_id: permission.sessionID || session,
				action: permission.action,
				resources: permission.resources ?? [],
				message: permission.message ?? undefined,
			}));
		},

		registerDevice: (device) =>
			request<Device>("/api/devices", {
				method: "POST",
				body: JSON.stringify(device),
			}),

		listDevices: () => request<Device[]>("/api/devices"),

		deleteDevice: (id) =>
			request<unknown>(`/api/devices/${encodeURIComponent(id)}`, { method: "DELETE" }),
	};
}
