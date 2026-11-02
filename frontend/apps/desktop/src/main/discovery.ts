import type { EventEmitter } from "node:events";
import { networkInterfaces } from "node:os";
import { Bonjour, type Browser, type Service } from "bonjour-service";
import { app, type BrowserWindow, ipcMain, powerMonitor } from "electron";
import {
	type DiscoveredServer,
	type DiscoveryResult,
	discoveryResetChannel,
	discoveryScanChannel,
} from "../shared/discovery";
import { mergeDiscoveryServices, parseDiscoveryService } from "./discovery-protocol";

// 有界快照搜索；所有调用共享一次搜索，结束时释放 UDP socket。
export function createDiscoveryScanner() {
	let pending: Promise<DiscoveryResult> | undefined;
	let cancel: ((discard?: boolean) => void) | undefined;
	return {
		scan(): Promise<DiscoveryResult> {
			if (pending) return pending;
			const work = new Promise<DiscoveryResult>((resolve) => {
				let bonjour: Bonjour | undefined;
				let browser: Browser | undefined;
				let ended = false;
				const initialNetwork = JSON.stringify(networkInterfaces());
				const candidates = new Map<string, DiscoveredServer>();
				const finish = (error?: string, discard = false) => {
					if (ended) return;
					ended = true;
					clearTimeout(timer);
					browser?.stop();
					bonjour?.destroy();
					resolve({
						servers:
							discard || initialNetwork !== JSON.stringify(networkInterfaces())
								? []
								: mergeDiscoveryServices([...candidates.values()]),
						error,
					});
				};
				const timer = setTimeout(() => finish(), 8000);
				cancel = (discard) => finish(undefined, discard);
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
						if (ended) return;
						const parsed = parseDiscoveryService(
							service,
							Object.values(networkInterfaces())
								.flatMap((n) => n ?? [])
								.filter((n) => !n.internal),
						);
						if (!parsed) return;
						const key = `${parsed.id}|${parsed.url}`;
						for (const existing of candidates.values()) {
							if (
								(existing.id === parsed.id && existing.url !== parsed.url) ||
								(existing.id !== parsed.id &&
									new URL(existing.url).hostname === new URL(parsed.url).hostname)
							) {
								existing.conflict = true;
								parsed.conflict = true;
							}
						}
						parsed.conflict ||= candidates.get(key)?.conflict;
						if (candidates.has(key) || candidates.size < 32) {
							const previous = candidates.get(key);
							candidates.set(
								key,
								previous ? mergeDiscoveryServices([previous, parsed])[0] : parsed,
							);
						}
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
			const result = work.finally(() => {
				if (pending === result) {
					pending = undefined;
					cancel = undefined;
				}
			});
			pending = result;
			return result;
		},
		invalidate() {
			cancel?.(true);
			pending = undefined;
			cancel = undefined;
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
	let signature = JSON.stringify(networkInterfaces());
	const reset = () => {
		scanner.invalidate();
		getWindow()?.webContents.send(discoveryResetChannel);
	};
	const timer = setInterval(() => {
		const next = JSON.stringify(networkInterfaces());
		if (next !== signature) {
			signature = next;
			reset();
		}
	}, 5000);
	timer.unref();
	powerMonitor.on("resume", reset);
	app.on("before-quit", () => {
		clearInterval(timer);
		powerMonitor.removeListener("resume", reset);
		scanner.stop();
	});
}
