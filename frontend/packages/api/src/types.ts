export interface NodeView {
	id: string;
	name: string;
	os: string;
	arch: string;
	agent_version: string;
	capabilities: string[];
	/** 节点允许的工作目录；老 agent 不上报时为空数组或缺省。 */
	workspace_roots?: string[];
	connected: boolean;
	last_seen: string;
}

export interface NodeHealth {
	running: boolean;
	mode: string;
	base_url?: string;
}

export interface Session {
	id: string;
	directory?: string;
	title?: string;
	updatedAt?: string;
	/** OpenCode 的项目 id；老控制面可能不返回。 */
	projectID?: string;
	/** 会话在项目根之下的相对路径；在项目根创建时为空。 */
	subpath?: string;
}

/** ProjectNode 描述项目在某个节点上的出现情况。 */
export interface ProjectNode {
	node_id: string;
	online: boolean;
	last_seen: string;
}

/**
 * ProjectSummary 是控制面聚合后的项目视图。
 *
 * key 是聚合键（通常等于 id，无提交仓库为 "global:<canonical>"）；
 * primary_node_id / alias 来自用户偏好，未设置时缺省。
 */
export interface ProjectSummary {
	key: string;
	id: string;
	canonical: string;
	vcs?: string;
	primary_node_id?: string;
	alias?: string;
	nodes: ProjectNode[];
	last_seen: string;
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

/** MessagePage 是一页按时间正序排列的消息；next 指向更早的一页。 */
export interface MessagePage {
	messages: Message[];
	previous?: string;
	next?: string;
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

export type SessionState = "running" | "waiting_permission" | "idle" | "error" | "offline";

export interface OverviewSession {
	session_id: string;
	title?: string;
	state: SessionState;
	pending_permissions: number;
	last_activity: string;
}

export interface OverviewNode {
	node_id: string;
	name: string;
	connected: boolean;
	sessions: OverviewSession[];
}

export interface PendingPermission {
	id: string;
	session_id: string;
	action: string;
	resources: string[];
	message?: string;
}

export interface DeviceRegistration {
	name: string;
	platform: "android" | "ios";
	push_token: string;
}

export interface Device {
	id: string;
	name: string;
	platform: "android" | "ios";
	push_token: string;
	last_seen: string;
	created_at: string;
}
