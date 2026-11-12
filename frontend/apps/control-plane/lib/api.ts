import type {
	FileDiff,
	FileInfo,
	FileReadResult,
	InterruptResult,
	Message,
	NodeHealth,
	NodeView,
	PermissionRequest,
	PersistentTerminal,
	Session,
} from "./types";

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

async function request<T>(path: string, init?: RequestInit): Promise<T> {
	const response = await fetch(path, {
		...init,
		headers: init?.body ? { "Content-Type": "application/json" } : undefined,
	});
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

function query(values: Record<string, string | number | undefined>): string {
	const params = new URLSearchParams();
	for (const [key, value] of Object.entries(values)) {
		if (value !== undefined && value !== "") params.set(key, String(value));
	}
	const encoded = params.toString();
	return encoded ? `?${encoded}` : "";
}

export function listNodes(): Promise<NodeView[]> {
	return request<NodeView[]>("/api/nodes");
}

interface RawNodeHealth {
	Running: boolean;
	Mode: string;
	BaseURL: string;
}

export async function getNodeHealth(node: string): Promise<NodeHealth> {
	const raw = await request<RawNodeHealth>(`/api/nodes/${encodeURIComponent(node)}/health`);
	return { running: raw.Running, mode: raw.Mode, base_url: raw.BaseURL };
}

export function startOpenCode(node: string): Promise<unknown> {
	return request<unknown>(`/api/nodes/${encodeURIComponent(node)}/opencode/start`, {
		method: "POST",
	});
}

export function restartOpenCode(node: string): Promise<unknown> {
	return request<unknown>(`/api/nodes/${encodeURIComponent(node)}/opencode/restart`, {
		method: "POST",
	});
}

export function stopOpenCode(node: string): Promise<unknown> {
	return request<unknown>(`/api/nodes/${encodeURIComponent(node)}/opencode/stop`, {
		method: "POST",
	});
}

export function listSessions(node: string, directory?: string): Promise<Session[]> {
	return request<Session[]>(`/api/sessions${query({ node, directory })}`);
}

export function createSession(node: string, directory: string): Promise<Session> {
	return request<Session>("/api/sessions", {
		method: "POST",
		body: JSON.stringify({ node_id: node, directory }),
	});
}

export function renameSession(node: string, session: string, title: string): Promise<unknown> {
	return request<unknown>(
		`/api/sessions/${encodeURIComponent(node)}/${encodeURIComponent(session)}`,
		{ method: "PATCH", body: JSON.stringify({ title }) },
	);
}

export function deleteSession(node: string, session: string): Promise<unknown> {
	return request<unknown>(
		`/api/sessions/${encodeURIComponent(node)}/${encodeURIComponent(session)}`,
		{ method: "DELETE" },
	);
}

export function listMessages(node: string, session: string): Promise<Message[]> {
	return request<Message[]>(
		`/api/sessions/${encodeURIComponent(node)}/${encodeURIComponent(session)}/messages`,
	);
}

export function sendPrompt(
	node: string,
	session: string,
	text: string,
	idempotencyKey: string,
): Promise<string> {
	return request<string>(
		`/api/sessions/${encodeURIComponent(node)}/${encodeURIComponent(session)}/prompt`,
		{
			method: "POST",
			body: JSON.stringify({ text, idempotency_key: idempotencyKey }),
		},
	);
}

export function interruptSession(node: string, session: string): Promise<InterruptResult> {
	return request<InterruptResult>(
		`/api/sessions/${encodeURIComponent(node)}/${encodeURIComponent(session)}/interrupt`,
		{ method: "POST" },
	);
}

export function replyPermission(
	node: string,
	session: string,
	permissionID: string,
	decision: "once" | "always" | "reject",
	message?: string,
): Promise<unknown> {
	return request<unknown>(
		`/api/sessions/${encodeURIComponent(node)}/${encodeURIComponent(session)}/permissions/${encodeURIComponent(permissionID)}/reply`,
		{ method: "POST", body: JSON.stringify({ decision, message }) },
	);
}

export function listPermissions(node: string, session: string): Promise<PermissionRequest[]> {
	return request<PermissionRequest[]>(
		`/api/sessions/${encodeURIComponent(node)}/${encodeURIComponent(session)}/permissions`,
	);
}

export function sessionDiff(node: string, session: string, directory: string): Promise<FileDiff[]> {
	return request<FileDiff[]>(
		`/api/sessions/${encodeURIComponent(node)}/${encodeURIComponent(session)}/diff${query({ directory })}`,
	);
}

export function listFiles(node: string, directory: string, path: string): Promise<FileInfo[]> {
	return request<FileInfo[]>(
		`/api/nodes/${encodeURIComponent(node)}/files${query({ directory, path })}`,
	);
}

export function readFile(node: string, directory: string, path: string): Promise<FileReadResult> {
	return request<FileReadResult>(
		`/api/nodes/${encodeURIComponent(node)}/files/content${query({ directory, path })}`,
	);
}

export function createPTY(
	node: string,
	session: string,
	directory: string,
	cols: number,
	rows: number,
): Promise<PersistentTerminal> {
	return request<PersistentTerminal>(
		`/api/sessions/${encodeURIComponent(node)}/${encodeURIComponent(session)}/pty`,
		{
			method: "POST",
			body: JSON.stringify({ directory, cols, rows }),
		},
	);
}

export function closePTY(node: string, session: string, ptyID: string): Promise<unknown> {
	return request<unknown>(
		`/api/sessions/${encodeURIComponent(node)}/${encodeURIComponent(session)}/pty/${encodeURIComponent(ptyID)}`,
		{ method: "DELETE" },
	);
}
