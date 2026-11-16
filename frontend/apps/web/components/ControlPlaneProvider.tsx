"use client";

import { createApiClient } from "@lework/api";
import { ControlApiProvider, ControlAuthGate } from "@lework/control-ui";
import { type ReactNode, useMemo } from "react";

/**
 * 控制面客户端与浏览器同源（Next rewrite 到 LEWORK_CONTROL_PLANE_URL），
 * 因此 baseUrl 留空、鉴权交给 HttpOnly Cookie。不要在客户端读取
 * LEWORK_CONTROL_PLANE_URL：它不是 NEXT_PUBLIC_，只能在 next.config 里用。
 *
 * PTY 走 WebSocket，rewrite 不保证代理 Upgrade；跨机/反代部署时设置
 * NEXT_PUBLIC_LEWORK_CONTROL_PLANE_WS_URL（如 wss://control.example.com）。
 */
const client = createApiClient();
const wsBaseUrl = process.env.NEXT_PUBLIC_LEWORK_CONTROL_PLANE_WS_URL?.replace(/\/$/, "");

const ptyUrl = wsBaseUrl
	? ({ nodeID, sessionID, ptyID }: { nodeID: string; sessionID: string; ptyID: string }) =>
			`${wsBaseUrl}/api/pty/${encodeURIComponent(nodeID)}/${encodeURIComponent(sessionID)}/${encodeURIComponent(ptyID)}/ws`
	: undefined;

export function ControlPlaneProvider({ children }: { children: ReactNode }) {
	const api = useMemo(() => ({ client, ptyUrl }), []);
	return (
		<ControlApiProvider {...api}>
			<ControlAuthGate>{children}</ControlAuthGate>
		</ControlApiProvider>
	);
}
