export interface NodeView {
	id: string;
	name: string;
	/** 每用户自己的节点颜色 token；空或缺省表示未设色。 */
	color?: string;
	os: string;
	arch: string;
	agent_version: string;
	capabilities: string[];
	/** 节点允许的工作目录；老 agent 不上报时为空数组或缺省。 */
	workspace_roots?: string[];
	/** 已归属某个账号；老控制面没有该字段时为 undefined。 */
	claimed?: boolean;
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
	/** 节点当前是否仍有未结束的执行（OpenCode time.busy > time.idle）。 */
	running?: boolean;
}

/** ProjectNode 描述项目在某个节点上的出现情况。 */
export interface ProjectNode {
	node_id: string;
	online: boolean;
	last_seen: string;
	/**
	 * 该项目在这台节点上的检出路径。同一 git 项目在不同机器上路径可能不同，
	 * 新建会话必须用它；老控制面缺省时退回项目顶层 canonical。
	 */
	canonical?: string;
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

/** ProjectCreateResult 是 agent 在 workspace root 下建好空目录后的返回值。 */
export interface ProjectCreateResult {
	canonical: string;
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
	/** 该会话待回答的表单数；旧控制面缺省时按 0 处理。 */
	pending_forms?: number;
	last_activity: string;
}

export interface OverviewNode {
	node_id: string;
	name: string;
	connected: boolean;
	/** 位置级（global）表单数量；旧控制面缺省时按 0 处理。 */
	pending_forms?: number;
	sessions: OverviewSession[];
}

export interface PendingPermission {
	id: string;
	session_id: string;
	action: string;
	resources: string[];
	message?: string;
}

export type FormAnswerValue = string | boolean | number | string[];

export interface FormOption {
	value: string;
	label: string;
	description?: string;
}

export interface FormWhen {
	key: string;
	op: "eq" | "neq";
	value: string | boolean | number;
}

export interface FormField {
	key: string;
	type: "string" | "number" | "integer" | "boolean" | "multiselect" | "external";
	title?: string;
	description?: string;
	required?: boolean;
	hidden?: boolean;
	when?: FormWhen[];
	format?: string;
	placeholder?: string;
	minLength?: number;
	maxLength?: number;
	minItems?: number;
	maxItems?: number;
	minimum?: number;
	maximum?: number;
	default?: FormAnswerValue;
	options?: FormOption[];
	custom?: boolean;
	url?: string;
}

/** PendingForm 同时用于会话表单与 global 表单；global 时 directory 必有值。 */
export interface PendingForm {
	id: string;
	session_id: string;
	title: string;
	metadata?: { kind?: string };
	fields: FormField[];
	directory?: string;
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

/** AuthMethods 是登录页在鉴权之前能拿到的全部信息。 */
export interface AuthMethods {
	initialized: boolean;
	registration_mode: "open" | "closed";
}

export interface AuthUser {
	id: string;
	email: string;
	role?: string;
	status?: string;
}

export interface AuthResponse {
	user: AuthUser;
	token: string;
}

/** ClaimPreview 是 5 位码对应的机器信息；确认前给用户看的就是它。 */
export interface ClaimPreview {
	code: string;
	node_name: string;
	os: string;
	arch: string;
	agent_version: string;
	expires_at: string;
}

/** ClaimCode 是网页为 `lework agent --code` 签出的短期认领码。 */
export interface ClaimCode {
	code: string;
	node_name: string;
	expires_at: string;
}

/** NodeProject 是单个节点上报的 OpenCode workspace 记录。 */
export interface NodeProject {
	id: string;
	canonical: string;
	vcs?: string;
}
