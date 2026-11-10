export interface NodeView {
	id: string;
	name: string;
	os: string;
	arch: string;
	agent_version: string;
	capabilities: string[];
	connected: boolean;
	last_seen: string;
}

export interface Session {
	id: string;
	directory?: string;
	title?: string;
	updatedAt?: string;
}

export interface MessageError {
	type: string;
	message: string;
	status?: number;
}

export interface ContentPart {
	type: string;
	text?: string;
	tool?: string;
	callID?: string;
	state?: unknown;
}

export interface Message {
	id: string;
	role: string;
	text?: string;
	parts?: ContentPart[];
	finish?: string;
	outcome?: string;
	error?: MessageError;
}

export interface InterruptResult {
	interrupted: boolean;
}

export interface FileDiff {
	file: string;
	patch: string;
	additions: number;
	deletions: number;
	status: "added" | "deleted" | "modified";
}

export interface FileInfo {
	path: string;
	type: "file" | "directory";
}

export interface FileReadResult {
	path: string;
	encoding: "utf-8" | "base64";
	content: string;
	size: number;
	truncated: boolean;
}

export interface TerminalSize {
	cols: number;
	rows: number;
}

export interface PersistentTerminal {
	id: string;
	title: string;
	command: string;
	cwd: string;
	status: string;
	pid?: number;
	size?: TerminalSize;
}

export interface PermissionRequest {
	id: string;
	sessionID: string;
	action: string;
	resources: string[];
	message?: string | null;
}

export interface ControlEvent {
	node_id: string;
	session_id?: string;
	seq: number;
	type: string;
	data?: unknown;
	time: string;
}
