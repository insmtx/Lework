import type {
	FileDiff,
	FileInfo,
	FileReadResult,
	InterruptResult,
	Message,
	NodeView,
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
	listSessions(node: string, directory?: string): Promise<Session[]>;
	createSession(node: string, directory: string): Promise<Session>;
	listMessages(node: string, session: string): Promise<Message[]>;
	sendPrompt(node: string, session: string, text: string, idempotencyKey: string): Promise<string>;
	interruptSession(node: string, session: string): Promise<InterruptResult>;
	replyPermission(
		node: string,
		session: string,
		permissionID: string,
		decision: "once" | "always" | "reject",
		message?: string,
	): Promise<unknown>;
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

		listSessions: (node, directory) =>
			request<Session[]>(`/api/sessions${query({ node, directory })}`),

		createSession: (node, directory) =>
			request<Session>("/api/sessions", {
				method: "POST",
				body: JSON.stringify({ node_id: node, directory }),
			}),

		listMessages: (node, session) =>
			request<Message[]>(
				`/api/sessions/${encodeURIComponent(node)}/${encodeURIComponent(session)}/messages`,
			),

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
	};
}
