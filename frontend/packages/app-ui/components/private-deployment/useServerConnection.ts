"use client";
import { API_BASE_URL, saveServerConnection, testServerConnection } from "@leros/store";
import { useEffect, useRef, useState } from "react";

export function useServerConnection(onReload: () => void, confirmSwitch = false) {
	const [testing, setTesting] = useState(false);
	const [errorMessage, setErrorMessage] = useState("");
	const [pendingURL, setPendingURL] = useState<string | null>(null);
	const busy = useRef(false);
	const pending = useRef<string | null>(null);
	const generation = useRef(0);
	const active = useRef(true);
	const expires = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
	const invalidate = () => {
		clearTimeout(expires.current);
		generation.current++;
		pending.current = null;
		busy.current = false;
		setTesting(false);
		setPendingURL(null);
	};
	const cancel = () => {
		invalidate();
		window.dispatchEvent(new Event("leros:connection-selection-cancelled"));
	};
	useEffect(() => {
		active.current = true;

		window.addEventListener("offline", invalidate);
		window.addEventListener("leros:connection-invalidated", invalidate);
		window.addEventListener("storage", invalidate);
		return () => {
			active.current = false;
			clearTimeout(expires.current);
			generation.current++;
			window.removeEventListener("offline", invalidate);
			window.removeEventListener("leros:connection-invalidated", invalidate);
			window.removeEventListener("storage", invalidate);
		};
	}, []);
	const save = (url: string) => {
		try {
			saveServerConnection(url, API_BASE_URL);
			onReload();
		} catch {
			setErrorMessage("无法保存服务连接，请检查本地存储后重试。登录状态可能已清除。");
		}
	};
	const connect = async (url: string, discovered = false) => {
		if (busy.current || !active.current) return;
		if (!discovered) window.dispatchEvent(new Event("leros:connection-selection-cancelled"));
		clearTimeout(expires.current);
		busy.current = true;
		const revision = ++generation.current;
		pending.current = null;
		setPendingURL(null);
		setTesting(true);
		setErrorMessage("");
		try {
			const normalized = await testServerConnection(url);
			if (!active.current || revision !== generation.current) return;
			if (confirmSwitch && normalized !== API_BASE_URL) {
				pending.current = normalized;
				setPendingURL(normalized);
				expires.current = setTimeout(() => {
					invalidate();
					setErrorMessage("连接确认已过期，请重新连接。");
				}, 30000);
			} else save(normalized);
		} catch {
			if (active.current && revision === generation.current)
				setErrorMessage("无法验证服务，请检查地址和网络后重试。");
		} finally {
			if (active.current && revision === generation.current) {
				busy.current = false;
				setTesting(false);
			}
		}
	};
	const confirm = async () => {
		if (!pending.current || busy.current || !active.current) return;
		busy.current = true;
		const url = pending.current;
		pending.current = null;
		const revision = generation.current;
		setTesting(true);
		setPendingURL(null);
		try {
			const normalized = await testServerConnection(url);
			if (!active.current || revision !== generation.current) return;
			if (normalized !== url) throw new Error("服务地址已改变");
			clearTimeout(expires.current);
			save(normalized);
		} catch {
			if (active.current && revision === generation.current)
				setErrorMessage("服务连接已失效，请检查网络后重新连接。");
		} finally {
			if (active.current && revision === generation.current) {
				busy.current = false;
				setTesting(false);
			}
		}
	};

	return {
		testing,
		errorMessage,
		pendingURL,
		connect,
		confirm,
		cancel,
		clearError: () => setErrorMessage(""),
	};
}
