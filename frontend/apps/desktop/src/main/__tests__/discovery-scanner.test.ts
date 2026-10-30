import { EventEmitter } from "node:events";
import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({ stop: vi.fn(), destroy: vi.fn(), find: vi.fn() }));
vi.mock("electron", () => ({ app: { on: vi.fn() }, ipcMain: { handle: vi.fn() } }));
const transport = new EventEmitter();
vi.mock("bonjour-service", () => ({
	Bonjour: class {
		server = { mdns: transport };
		find = mocks.find;
		destroy = mocks.destroy;
	},
}));

import { createDiscoveryScanner } from "../discovery";

describe("bounded discovery", () => {
	afterEach(() => {
		vi.useRealTimers();
		vi.clearAllMocks();
		transport.removeAllListeners();
	});
	it("shares concurrent scans, collects services, releases resources after 8 seconds", async () => {
		vi.useFakeTimers();
		const browser = Object.assign(new EventEmitter(), { stop: mocks.stop });
		mocks.find.mockReturnValue(browser);
		const scanner = createDiscoveryScanner();
		const first = scanner.scan();
		expect(scanner.scan()).toBe(first);
		browser.emit("up", {
			name: "Spark",
			host: "spark.local",
			port: 38081,
			txt: { v: "1", id: "x", scheme: "http", path: "/v1" },
		});
		await vi.advanceTimersByTimeAsync(8000);
		expect((await first).servers).toHaveLength(1);
		expect(mocks.stop).toHaveBeenCalledOnce();
		expect(mocks.destroy).toHaveBeenCalledOnce();
	});
	it("reports asynchronous UDP errors without crashing", async () => {
		vi.useFakeTimers();
		mocks.find.mockReturnValue(Object.assign(new EventEmitter(), { stop: mocks.stop }));
		const scanner = createDiscoveryScanner();
		const result = scanner.scan();
		transport.emit("error", new Error("EACCES"));
		expect((await result).error).toContain("权限");
		expect(mocks.destroy).toHaveBeenCalledOnce();
	});
	it("flags late conflicting identities even when the list is full", async () => {
		vi.useFakeTimers();
		const browser = Object.assign(new EventEmitter(), { stop: mocks.stop });
		mocks.find.mockReturnValue(browser);
		const scanner = createDiscoveryScanner();
		const result = scanner.scan();
		for (let i = 0; i < 32; i++)
			browser.emit("up", {
				name: "Spark",
				host: `spark${i}.local`,
				port: 38081,
				txt: { v: "1", id: `id-${i}`, scheme: "http", path: "/v1" },
			});
		browser.emit("up", {
			name: "Other",
			host: "other.local",
			port: 38081,
			txt: { v: "1", id: "id-0", scheme: "http", path: "/v1" },
		});
		scanner.stop();
		expect((await result).servers.find((s) => s.id === "id-0")?.conflict).toBe(true);
	});
	it("replaces changed SRV records and removes unsupported TXT updates", async () => {
		vi.useFakeTimers();
		const browser = Object.assign(new EventEmitter(), { stop: mocks.stop });
		mocks.find.mockReturnValue(browser);
		const scanner = createDiscoveryScanner();
		const result = scanner.scan();
		const old = {
			name: "Spark",
			host: "spark.local",
			port: 38081,
			txt: { v: "1", id: "id", scheme: "http", path: "/v1" },
		};
		const updated = { ...old, port: 38082 };
		browser.emit("up", old);
		browser.emit("srv-update", updated, old);
		scanner.stop();
		expect((await result).servers.map((s) => s.url)).toEqual(["http://spark.local:38082/v1"]);
		const next = scanner.scan();
		browser.emit("up", updated);
		browser.emit("txt-update", { ...updated, txt: { ...updated.txt, v: "2" } }, updated);
		scanner.stop();
		expect((await next).servers).toEqual([]);
	});
	it("cancels and permits a fresh search without stale results", async () => {
		vi.useFakeTimers();
		mocks.find.mockReturnValue(Object.assign(new EventEmitter(), { stop: mocks.stop }));
		const scanner = createDiscoveryScanner();
		const result = scanner.scan();
		scanner.stop();
		expect((await result).servers).toEqual([]);
		const next = scanner.scan();
		scanner.stop();
		await next;
		expect(mocks.destroy).toHaveBeenCalledTimes(2);
	});
});
