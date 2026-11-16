"use client";

import type { ApiClient } from "@lework/api";
import { createContext, type ReactNode, useContext } from "react";

/**
 * ControlApiContextValue 是控制面 UI 与宿主应用之间的唯一接口。
 *
 * 组件不读环境变量、不拼 `window.location`，也不直接 import 某个 app 的
 * `lib/api`：控制面（同源 + Cookie）与 PC 工作台（Next rewrite + Cookie）
 * 的差异全部收敛到注入值，组件才能被两端共用。
 */
export interface ControlApiContextValue {
	/** 已按宿主鉴权方式构造好的 API 客户端。 */
	client: ApiClient;
	/** 控制面 SSE 地址；缺省为同源 `/api/events`。 */
	eventsUrl?: string;
	/** 构造 PTY WebSocket 地址；缺省按当前页面 origin 推导。 */
	ptyUrl?: (parts: { nodeID: string; sessionID: string; ptyID: string }) => string;
}

const ControlApiContext = createContext<ControlApiContextValue | null>(null);

export function ControlApiProvider({
	client,
	eventsUrl,
	ptyUrl,
	children,
}: ControlApiContextValue & { children: ReactNode }) {
	return (
		<ControlApiContext.Provider value={{ client, eventsUrl, ptyUrl }}>
			{children}
		</ControlApiContext.Provider>
	);
}

/** useControlApi 读取注入的客户端；在 Provider 之外使用属于接线错误。 */
export function useControlApi(): ControlApiContextValue {
	const value = useContext(ControlApiContext);
	if (!value) throw new Error("useControlApi must be used inside ControlApiProvider");
	return value;
}

/**
 * defaultPtyUrl 按当前页面 origin 推导 PTY 地址。
 *
 * 只适用于「页面与 /api 同源」的部署；跨源或反代改写路径时宿主应显式传
 * `ptyUrl`，否则 WebSocket 会连到错误的主机。
 */
export function defaultPtyUrl({
	nodeID,
	sessionID,
	ptyID,
}: {
	nodeID: string;
	sessionID: string;
	ptyID: string;
}): string {
	const scheme = window.location.protocol === "https:" ? "wss" : "ws";
	const path = `/api/pty/${encodeURIComponent(nodeID)}/${encodeURIComponent(sessionID)}/${encodeURIComponent(ptyID)}/ws`;
	return `${scheme}://${window.location.host}${path}`;
}
