import type { EventEmitter } from "node:events";
import { Bonjour, type Browser, type Service } from "bonjour-service";
import { app, type BrowserWindow, ipcMain } from "electron";
import {
	type DiscoveredServer,
	type DiscoveryResult,
	discoveryScanChannel,
} from "../shared/discovery";
import { mergeDiscoveryServices, parseDiscoveryService } from "./discovery-protocol";

// 有界快照搜索；所有调用共享一次搜索，结束时释放 UDP socket。
export function createDiscoveryScanner() {
	let pending: Promise<DiscoveryResult> | undefined;
	let cancel: (() => void) | undefined;
	return {
		scan(): Promise<DiscoveryResult> {
			if (pending) return pending;
			const work = new Promise<DiscoveryResult>((resolve) => {
				let bonjour: Bonjour | undefined;
				let browser: Browser | undefined;
				let ended = false;
				const candidates = new Map<string, DiscoveredServer>();
				const finish = (error?: string) => {
					if (ended) return;
					ended = true;
					clearTimeout(timer);
					browser?.stop();
					bonjour?.destroy();
					resolve({ servers: mergeDiscoveryServices([...candidates.values()]), error });
				};
				const timer = setTimeout(() => finish(), 8000);
				cancel = () => finish();
				try {
					bonjour = new Bonjour({}, () =>
						finish("无法搜索局域网，请检查本地网络权限；也可手动输入地址。"),
					);
					// bonjour-service 1.4.4 的回调不覆盖 socket error；固定版本的传输层必须显式监听。
					const transport = (bonjour as unknown as { server: { mdns: EventEmitter } }).server.mdns;
					transport.on("error", () =>
						finish("无法搜索局域网，请检查本地网络权限；也可手动输入地址。"),
					);
					if (ended) {
						bonjour.destroy();
						return;
					}
					browser = bonjour.find({ type: "lework", protocol: "tcp" });
					const add = (service: Service) => {
						const parsed = parseDiscoveryService(service);
						if (!parsed) return;
						const key = `${parsed.id}|${parsed.url}`;
						for (const existing of candidates.values()) {
							if (existing.id === parsed.id && existing.url !== parsed.url) {
								existing.conflict = true;
								parsed.conflict = true;
							}
						}
						parsed.conflict ||= candidates.get(key)?.conflict;
						if (candidates.has(key) || candidates.size < 32) candidates.set(key, parsed);
					};
					browser.on("up", add);
					const replace = (next: Service, previous: Service) => {
						const old = parseDiscoveryService(previous);
						if (old) candidates.delete(`${old.id}|${old.url}`);
						add(next);
					};
					browser.on("srv-update", replace);
					browser.on("txt-update", replace);
					browser.on("down", (service) => {
						const parsed = parseDiscoveryService(service);
						if (parsed) candidates.delete(`${parsed.id}|${parsed.url}`);
					});
				} catch {
					finish("发现服务启动失败，可继续手动连接。");
				}
			});
			pending = work.finally(() => {
				pending = undefined;
				cancel = undefined;
			});
			return pending;
		},
		stop() {
			cancel?.();
		},
	};
}

export function registerDiscovery(getWindow: () => BrowserWindow | null) {
	const scanner = createDiscoveryScanner();
	ipcMain.handle(discoveryScanChannel, (event) => {
		const window = getWindow();
		if (
			!window ||
			event.sender !== window.webContents ||
			event.senderFrame !== window.webContents.mainFrame
		) {
			throw new Error("不允许此窗口发起搜索");
		}
		if (process.env.LEROS_DEPLOYMENT_MODE !== "private") return { servers: [] };
		return scanner.scan();
	});
	app.on("before-quit", () => scanner.stop());
}
