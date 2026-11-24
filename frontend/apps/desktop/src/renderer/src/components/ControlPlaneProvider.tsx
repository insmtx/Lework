import { createApiClient } from "@lework/api";
import { ControlApiProvider, ControlAuthGate } from "@lework/control-ui";
import type { ReactNode } from "react";

// 桌面渲染层从本地回环源加载，`/api` 已由主进程反代到控制面，
// 所以地址留空、鉴权交给同源 Cookie；PTY 也由默认地址按当前 origin 推导。
const client = createApiClient();

export function ControlPlaneProvider({ children }: { children: ReactNode }) {
	return (
		<ControlApiProvider client={client}>
			<ControlAuthGate>{children}</ControlAuthGate>
		</ControlApiProvider>
	);
}
