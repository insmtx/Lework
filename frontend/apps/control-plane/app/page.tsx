"use client";

import { createApiClient } from "@lework/api";
import { ControlApiProvider, ControlAuthGate, ControlConsole } from "@lework/control-ui";
import { useMemo } from "react";

// 控制面与控制台同源：baseUrl 留空，鉴权靠 HttpOnly Cookie。
const client = createApiClient();

export default function ControlPlanePage() {
	const api = useMemo(() => ({ client }), []);
	return (
		<ControlApiProvider {...api}>
			<ControlAuthGate>
				<ControlConsole />
			</ControlAuthGate>
		</ControlApiProvider>
	);
}
