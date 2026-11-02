import {
	API_BASE_URL,
	inspectSparkDevice,
	type SparkDevice,
	sparkApi,
	useAuthStore,
} from "@leros/store";
import { createContext, type ReactNode, useContext, useEffect, useState } from "react";

export type SparkConnection = {
	device: SparkDevice;
	status: "online" | "offline";
	baseURL: string;
};
const SparkConnectionContext = createContext<SparkConnection | null>(null);
export function useSparkConnection() {
	return useContext(SparkConnectionContext);
}

export function SparkConnectionGate({
	baseURL = API_BASE_URL,
	children,
}: {
	baseURL?: string;
	children: ReactNode;
}) {
	const user = useAuthStore((s) => s.authUser);
	const org = user?.organizations?.find((o) => o.id === user.currentOrg?.id) ?? user?.currentOrg;
	// 匿名识别不代表授权；身份改变时清空当前设备与子组件中的旧管理状态。
	const identity = JSON.stringify([baseURL, user?.publicId, org?.id, org?.isAdmin, user?.jwtToken]);
	return (
		<ConnectionSession
			key={identity}
			baseURL={baseURL}
			allowLegacy={Boolean(user && org?.isAdmin && baseURL === API_BASE_URL)}
		>
			{children}
		</ConnectionSession>
	);
}

function ConnectionSession({
	baseURL,
	children,
	allowLegacy,
}: {
	baseURL: string;
	children: ReactNode;
	allowLegacy: boolean;
}) {
	const [connection, setConnection] = useState<SparkConnection | null>(null);
	useEffect(() => {
		let disposed = false;
		let sequence = 0;
		let inFlight = false;
		let controller: AbortController | undefined;
		const inspect = async () => {
			// 旧版状态接口耗时可能超过发现周期，避免重复请求 Worker 状态。
			if (inFlight) return;
			inFlight = true;
			const current = ++sequence;
			controller?.abort();
			controller = new AbortController();
			try {
				let device = await inspectSparkDevice(baseURL, controller.signal);
				if (disposed || current !== sequence) return;
				// 仅当前已登录服务的管理员可走兼容识别；绝不向发现候选发送凭据。
				// status() 已严格验证 Spark 协议和快照，接口存在或主机名相似都不足以识别。
				if (!device && allowLegacy) {
					const status = await sparkApi.status();
					const id = status.snapshot.host_id;
					if (!/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(id)) throw new Error("设备标识无效");
					device = { type: "spark", protocol_version: 1, id, name: "Spark", management: true };
				}
				if (disposed || current !== sequence) return;
				setConnection(device ? { device, status: "online", baseURL } : null);
			} catch {
				if (disposed || current !== sequence) return;
				setConnection((known) => (known ? { ...known, status: "offline" } : null));
			} finally {
				inFlight = false;
			}
		};
		const offline = () => {
			++sequence;
			controller?.abort();
			setConnection((known) => (known ? { ...known, status: "offline" } : null));
		};
		void inspect();
		const timer = window.setInterval(() => void inspect(), 15000);
		window.addEventListener("online", inspect);
		window.addEventListener("offline", offline);
		return () => {
			disposed = true;
			controller?.abort();
			window.clearInterval(timer);
			window.removeEventListener("online", inspect);
			window.removeEventListener("offline", offline);
		};
	}, [baseURL, allowLegacy]);
	if (!connection) return null;
	return (
		<SparkConnectionContext.Provider key={connection.device.id} value={connection}>
			{children}
		</SparkConnectionContext.Provider>
	);
}
