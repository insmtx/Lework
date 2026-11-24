import { createReadStream, statSync } from "node:fs";
import {
	createServer,
	request as httpRequest,
	type IncomingHttpHeaders,
	type IncomingMessage,
	type OutgoingHttpHeaders,
	type ServerResponse,
} from "node:http";
import { request as httpsRequest } from "node:https";
import { connect as netConnect, type Socket } from "node:net";
import { extname, resolve, sep } from "node:path";
import { connect as tlsConnect } from "node:tls";
import type { Duplex } from "node:stream";

/**
 * 桌面渲染层从 file:// 改为固定的回环 http 源。
 *
 * 这样渲染进程与 Web 一样拥有同源 `/api`：HttpOnly cookie、SSE 与 PTY
 * WebSocket 都不需要给控制面加 CORS，也不需要关闭 Electron 的 web security。
 * 源只监听 127.0.0.1，代理目标由主进程决定，渲染层无法指定任意上游。
 */

export const LOCAL_ORIGIN_PORT = 47830;

type Upstreams = {
	apiBaseURL: string;
	controlPlaneBaseURL: string;
};

export type LocalOrigin = {
	origin: string;
	setUpstreams(next: Partial<Upstreams>): void;
	close(): Promise<void>;
};

const HOP_BY_HOP = new Set([
	"connection",
	"keep-alive",
	"proxy-authenticate",
	"proxy-authorization",
	"te",
	"trailer",
	"transfer-encoding",
	"upgrade",
]);

const MIME_TYPES: Record<string, string> = {
	".html": "text/html; charset=utf-8",
	".js": "text/javascript; charset=utf-8",
	".mjs": "text/javascript; charset=utf-8",
	".css": "text/css; charset=utf-8",
	".json": "application/json; charset=utf-8",
	".map": "application/json; charset=utf-8",
	".svg": "image/svg+xml",
	".png": "image/png",
	".jpg": "image/jpeg",
	".jpeg": "image/jpeg",
	".gif": "image/gif",
	".ico": "image/x-icon",
	".woff": "font/woff",
	".woff2": "font/woff2",
	".ttf": "font/ttf",
	".webp": "image/webp",
	".txt": "text/plain; charset=utf-8",
};

/**
 * proxyRequestHeaders 去掉会在上游改变鉴权语义的头。
 *
 * 控制面按 RemoteAddr 判断本机管理员，PTY 又用 Origin 与 Host 比较；本地源
 * 与上游不同源，这两类头必须就地剥离，不能让上游误判成浏览器跨域请求。
 */
export function proxyRequestHeaders(incoming: IncomingHttpHeaders): OutgoingHttpHeaders {
	const headers: OutgoingHttpHeaders = {};
	for (const [name, value] of Object.entries(incoming)) {
		const key = name.toLowerCase();
		if (key === "origin" || key.startsWith("x-forwarded-") || key === "forwarded") continue;
		if (key === "x-real-ip") continue;
		if (HOP_BY_HOP.has(key)) continue;
		if (value !== undefined) headers[key] = value;
	}
	return headers;
}

/**
 * rewriteSetCookie 让上游 cookie 能在当前源上保存。
 *
 * 本地源是 127.0.0.1 的 http，上游可能是带 Domain 的 https 控制面；不修
 * Domain/Secure 的话会话 cookie 会被浏览器直接丢弃。
 */
export function rewriteSetCookie(
	values: string[],
	originProtocol: "http:" | "https:",
): string[] {
	return values.map((value) => {
		const parts = value.split(";").map((part) => part.trim());
		const kept: string[] = [];
		let hasSecure = false;
		for (const part of parts) {
			const lower = part.toLowerCase();
			if (lower.startsWith("domain=")) continue;
			if (lower === "secure") {
				hasSecure = true;
				if (originProtocol === "https:") kept.push(part);
				continue;
			}
			if (part !== "") kept.push(part);
		}
		if (originProtocol === "https:" && !hasSecure) kept.push("Secure");
		return kept.join("; ");
	});
}

/**
 * upstreamForPath 只放行 `/api` 与 `/v1`，其余交给静态文件。
 *
 * `/v1` 的配置地址通常带结尾 `/v1`，拼接前去掉，避免出现 `/v1/v1`。
 */
export function upstreamForPath(path: string, upstreams: Upstreams): { base: string; path: string } | null {
	if (path === "/api" || path.startsWith("/api/") || path.startsWith("/api?")) {
		return { base: upstreams.controlPlaneBaseURL.replace(/\/+$/, ""), path };
	}
	if (path === "/v1" || path.startsWith("/v1/") || path.startsWith("/v1?")) {
		const base = upstreams.apiBaseURL.replace(/\/+$/, "").replace(/\/v1$/, "");
		return { base, path };
	}
	return null;
}

export async function startLocalOrigin(options: {
	rootDir: string;
	apiBaseURL: string;
	controlPlaneBaseURL: string;
	port?: number;
}): Promise<LocalOrigin> {
	let upstreams: Upstreams = {
		apiBaseURL: options.apiBaseURL,
		controlPlaneBaseURL: options.controlPlaneBaseURL,
	};
	const rootDir = resolve(options.rootDir);
	const sockets = new Set<Duplex>();

	const server = createServer((req, res) => {
		const path = req.url ?? "/";
		const target = upstreamForPath(path, upstreams);
		if (target) {
			proxyRequest(req, res, target, "http:");
			return;
		}
		serveStatic(rootDir, req, res);
	});

	server.on("connection", (socket) => {
		sockets.add(socket);
		socket.on("close", () => sockets.delete(socket));
	});

	server.on("upgrade", (req, socket, head) => {
		sockets.add(socket);
		socket.on("close", () => sockets.delete(socket));
		const target = upstreamForPath(req.url ?? "/", upstreams);
		if (!target) {
			socket.destroy();
			return;
		}
		proxyUpgrade(req, socket, head, target, sockets);
	});

	server.keepAliveTimeout = 65_000;

	await new Promise<void>((resolvePromise, reject) => {
		server.once("error", reject);
		server.listen(options.port ?? LOCAL_ORIGIN_PORT, "127.0.0.1", () => {
			server.off("error", reject);
			resolvePromise();
		});
	});

	const address = server.address();
	if (address === null || typeof address === "string") {
		throw new Error("本地渲染源没有拿到监听地址");
	}

	return {
		origin: `http://127.0.0.1:${address.port}`,
		setUpstreams(next) {
			upstreams = { ...upstreams, ...next };
		},
		close() {
			for (const socket of sockets) socket.destroy();
			return new Promise<void>((resolvePromise) => server.close(() => resolvePromise()));
		},
	};
}

function proxyRequest(
	req: IncomingMessage,
	res: ServerResponse,
	target: { base: string; path: string },
	originProtocol: "http:" | "https:",
): void {
	const url = new URL(target.base);
	if (url.protocol !== "http:" && url.protocol !== "https:") {
		res.statusCode = 502;
		res.end("invalid upstream");
		return;
	}

	const headers = proxyRequestHeaders(req.headers);
	headers.host = url.host;
	const request = url.protocol === "https:" ? httpsRequest : httpRequest;
	const upstream = request(
		{
			protocol: url.protocol,
			hostname: url.hostname,
			port: url.port || undefined,
			method: req.method,
			path: target.path,
			headers,
		},
		(upstreamRes) => {
			res.statusCode = upstreamRes.statusCode ?? 502;
			for (const [name, value] of Object.entries(upstreamRes.headers)) {
				if (name.toLowerCase() === "set-cookie" || value === undefined) continue;
				res.setHeader(name, value);
			}
			const cookies = upstreamRes.headers["set-cookie"];
			if (cookies?.length) {
				res.setHeader("set-cookie", rewriteSetCookie(cookies, originProtocol));
			}
			// 逐块 pipe：SSE 与长响应不能被缓冲。
			upstreamRes.pipe(res);
		},
	);
	upstream.on("error", () => {
		if (!res.headersSent) {
			res.statusCode = 502;
			res.end("upstream unavailable");
		} else {
			res.destroy();
		}
	});
	req.pipe(upstream);
}

function proxyUpgrade(
	req: IncomingMessage,
	socket: Duplex,
	head: Buffer,
	target: { base: string; path: string },
	sockets: Set<Duplex>,
): void {
	const url = new URL(target.base);
	const secure = url.protocol === "https:";
	const port = Number(url.port || (secure ? 443 : 80));
	if (url.protocol !== "http:" && !secure) {
		socket.destroy();
		return;
	}

	const upstream = (secure
		? tlsConnect({ host: url.hostname, port, servername: url.hostname })
		: netConnect(port, url.hostname)) as Socket;

	sockets.add(upstream);
	upstream.on("close", () => sockets.delete(upstream));

	const handshake = () => {
		const headers = proxyRequestHeaders(req.headers);
		// 升级握手必须保留 Connection / Upgrade，否则上游会当成普通请求。
		if (req.headers.connection) headers.connection = req.headers.connection;
		if (req.headers.upgrade) headers.upgrade = req.headers.upgrade;
		headers.host = url.host;
		const lines = [`${req.method ?? "GET"} ${target.path} HTTP/1.1`];
		for (const [name, value] of Object.entries(headers)) {
			if (value === undefined) continue;
			lines.push(`${name}: ${Array.isArray(value) ? value.join(", ") : value}`);
		}
		upstream.write(`${lines.join("\r\n")}\r\n\r\n`);
		if (head.length > 0) upstream.write(head);
		upstream.pipe(socket);
		socket.pipe(upstream);
	};

	if (secure) upstream.once("secureConnect", handshake);
	else upstream.once("connect", handshake);
	upstream.on("error", () => socket.destroy());
	socket.on("error", () => upstream.destroy());
	socket.on("close", () => upstream.destroy());
}

function serveStatic(rootDir: string, req: IncomingMessage, res: ServerResponse): void {
	if (req.method !== "GET" && req.method !== "HEAD") {
		res.statusCode = 405;
		res.end("method not allowed");
		return;
	}

	let pathname: string;
	try {
		pathname = decodeURIComponent(new URL(req.url ?? "/", "http://127.0.0.1").pathname);
	} catch {
		res.statusCode = 400;
		res.end("bad request");
		return;
	}

	const relative = pathname === "/" ? "/index.html" : pathname;
	const filePath = resolve(rootDir, `.${relative}`);
	if (filePath !== rootDir && !filePath.startsWith(`${rootDir}${sep}`)) {
		res.statusCode = 404;
		res.end("not found");
		return;
	}

	let size: number;
	try {
		const stat = statSync(filePath);
		if (!stat.isFile()) throw new Error("not a file");
		size = stat.size;
	} catch {
		res.statusCode = 404;
		res.end("not found");
		return;
	}

	res.statusCode = 200;
	res.setHeader("content-type", MIME_TYPES[extname(filePath).toLowerCase()] ?? "application/octet-stream");
	res.setHeader("content-length", size);
	if (relative.startsWith("/assets/")) {
		res.setHeader("cache-control", "public, max-age=31536000, immutable");
	}
	if (req.method === "HEAD") {
		res.end();
		return;
	}
	createReadStream(filePath).on("error", () => res.destroy()).pipe(res);
}
