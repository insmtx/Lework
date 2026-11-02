import { normalizeAPIBaseURL } from "./config";

export type SparkDevice = {
	type: "spark";
	protocol_version: 1;
	id: string;
	name: string;
	management: boolean;
};

function record(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}

/** Anonymous device category detection never grants management permission. */
export async function inspectSparkDevice(
	baseURL: string,
	signal?: AbortSignal,
): Promise<SparkDevice | null> {
	const normalized = normalizeAPIBaseURL(baseURL);
	const controller = new AbortController();
	const abort = () => controller.abort();
	if (signal?.aborted) controller.abort();
	signal?.addEventListener("abort", abort, { once: true });
	const timeout = setTimeout(abort, 8000);
	try {
		const response = await fetch(`${normalized}/GlobalConfig`, {
			method: "GET",
			credentials: "omit",
			redirect: "error",
			cache: "no-store",
			headers: { Accept: "application/json" },
			signal: controller.signal,
		});
		if (!response.ok) throw new Error("设备识别失败");
		const payload = await readDeclaration(response, controller.signal);
		if (
			!record(payload) ||
			payload.code !== 0 ||
			!record(payload.data) ||
			!["oss", "enterprise"].includes(String(payload.data.edition))
		)
			throw new Error("设备声明无效");
		if (!("device" in payload.data)) return null;
		const d = payload.data.device;
		if (
			!record(d) ||
			d.type !== "spark" ||
			d.protocol_version !== 1 ||
			typeof d.id !== "string" ||
			!/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(d.id) ||
			typeof d.name !== "string" ||
			!d.name ||
			d.name.trim() !== d.name ||
			new TextEncoder().encode(d.name).length > 256 ||
			// biome-ignore lint/suspicious/noControlCharactersInRegex: 设备名称必须拒绝 C0/C1 控制字符。
			/[\u0000-\u001f\u007f-\u009f]/.test(d.name) ||
			typeof d.management !== "boolean"
		)
			throw new Error("设备声明无效");
		return { type: "spark", protocol_version: 1, id: d.id, name: d.name, management: d.management };
	} catch {
		throw new Error(
			controller.signal.aborted ? "设备识别已取消或超时" : "无法确认设备，请检查连接后重试",
		);
	} finally {
		clearTimeout(timeout);
		signal?.removeEventListener("abort", abort);
	}
}

// 不能信任候选服务给出的 Content-Length；按实际收到的字节限制正文。
async function readDeclaration(response: Response, signal: AbortSignal): Promise<unknown> {
	const reader = response.body?.getReader();
	if (!reader) throw new Error("设备声明为空");
	const cancel = () => {
		void reader.cancel().catch(() => {
			// 取消后的读取错误由外层请求错误路径统一处理。
		});
	};
	signal.addEventListener("abort", cancel, { once: true });
	const decoder = new TextDecoder("utf-8", { fatal: true });
	let bytes = 0;
	let body = "";
	try {
		signal.throwIfAborted();
		while (true) {
			const part = await reader.read();
			signal.throwIfAborted();
			if (part.done) break;
			bytes += part.value.byteLength;
			if (bytes > 64 * 1024) throw new Error("设备声明过大");
			body += decoder.decode(part.value, { stream: true });
		}
		body += decoder.decode();
		return JSON.parse(body) as unknown;
	} finally {
		signal.removeEventListener("abort", cancel);
		cancel();
		reader.releaseLock();
	}
}
