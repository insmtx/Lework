import { mkdtempSync, writeFileSync } from "node:fs";
import { type RequestListener, createServer as createHttpServer, type Server } from "node:http";
import { connect } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { Duplex } from "node:stream";
import { afterEach, describe, expect, it } from "vitest";
import {
	proxyRequestHeaders,
	rewriteSetCookie,
	startLocalOrigin,
	upstreamForPath,
} from "../local-origin";

const cleanups: Array<() => Promise<void> | void> = [];

afterEach(async () => {
	while (cleanups.length > 0) {
		await cleanups.pop()?.();
	}
});

async function startUpstream(handler: RequestListener): Promise<{ server: Server; base: string }> {
	const server = createHttpServer(handler);
	await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
	cleanups.push(
		() =>
			new Promise<void>((resolve) => {
				server.closeAllConnections();
				server.close(() => resolve());
			}),
	);
	const address = server.address();
	if (address === null || typeof address === "string") throw new Error("no upstream address");
	return { server, base: `http://127.0.0.1:${address.port}` };
}

async function startProxy(options: { rootDir: string; apiBaseURL: string; controlPlaneBaseURL: string }) {
	const proxy = await startLocalOrigin({ ...options, port: 0 });
	cleanups.push(() => proxy.close());
	return proxy;
}

describe("local origin helpers", () => {
	it("strips Origin and forwarding headers", () => {
		expect(
			proxyRequestHeaders({
				origin: "http://127.0.0.1:47830",
				"x-forwarded-for": "1.2.3.4",
				"x-forwarded-proto": "https",
				cookie: "a=b",
			}),
		).toEqual({ cookie: "a=b" });
	});

	it("strips Domain and Secure from cookies on the http loopback origin", () => {
		expect(
			rewriteSetCookie(
				["lework_session=x; Path=/; Domain=example.com; Secure; HttpOnly; SameSite=Strict"],
				"http:",
			),
		).toEqual(["lework_session=x; Path=/; HttpOnly; SameSite=Strict"]);
	});

	it("keeps Secure cookies on an https origin", () => {
		expect(rewriteSetCookie(["a=b; Path=/; HttpOnly"], "https:")).toEqual([
			"a=b; Path=/; HttpOnly; Secure",
		]);
	});

	it("maps /api to the control plane and /v1 to the legacy api", () => {
		const upstreams = {
			apiBaseURL: "https://api.example.com/v1",
			controlPlaneBaseURL: "http://127.0.0.1:8787",
		};
		expect(upstreamForPath("/api/me", upstreams)).toEqual({
			base: "http://127.0.0.1:8787",
			path: "/api/me",
		});
		expect(upstreamForPath("/v1/GlobalConfig", upstreams)).toEqual({
			base: "https://api.example.com",
			path: "/v1/GlobalConfig",
		});
		expect(upstreamForPath("/index.html", upstreams)).toBeNull();
	});
});

describe("local origin server", () => {
	it("serves the renderer entry and proxies /api without forwarding Origin", async () => {
		const rootDir = mkdtempSync(join(tmpdir(), "lework-origin-"));
		writeFileSync(join(rootDir, "index.html"), "<h1>renderer</h1>");
		const { base } = await startUpstream((req, res) => {
			res.setHeader("set-cookie", "lework_session=x; Path=/; Domain=127.0.0.1; Secure; HttpOnly");
			res.end(`upstream:${req.url}:${req.headers.origin ?? "none"}`);
		});
		const proxy = await startProxy({
			rootDir,
			apiBaseURL: `${base}/v1`,
			controlPlaneBaseURL: base,
		});

		const page = await fetch(proxy.origin);
		expect(await page.text()).toContain("renderer");

		const response = await fetch(`${proxy.origin}/api/hello`, {
			headers: { origin: "http://127.0.0.1:47830" },
		});
		expect(await response.text()).toBe("upstream:/api/hello:none");
		expect(response.headers.get("set-cookie")).toBe("lework_session=x; Path=/; HttpOnly");
	});

	it("forwards websocket upgrades", async () => {
		const rootDir = mkdtempSync(join(tmpdir(), "lework-origin-"));
		const { server, base } = await startUpstream(() => {});
		const upgraded = new Set<Duplex>();
		cleanups.push(() => {
			for (const socket of upgraded) socket.destroy();
		});
		server.on("upgrade", (_req, socket) => {
			upgraded.add(socket);
			socket.write("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n");
			socket.write("hello");
		});
		const proxy = await startProxy({
			rootDir,
			apiBaseURL: `${base}/v1`,
			controlPlaneBaseURL: base,
		});

		const port = Number(new URL(proxy.origin).port);
		const received = await new Promise<string>((resolve, reject) => {
			const socket = connect(port, "127.0.0.1", () => {
				socket.write(
					"GET /api/pty/x HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n",
				);
			});
			let data = "";
			socket.on("data", (chunk) => {
				data += chunk.toString();
				if (data.includes("hello")) {
					socket.destroy();
					resolve(data);
				}
			});
			socket.on("error", reject);
		});

		expect(received).toContain("101 Switching Protocols");
		expect(received).toContain("hello");
	});
});
