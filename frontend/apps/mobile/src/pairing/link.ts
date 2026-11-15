import { normalizeBaseUrl } from "../settings/settings";

// PairPayload 是从配对链接里解析出的控制面连接信息。
export interface PairPayload {
	baseUrl: string;
	token: string;
}

// decode 容忍非法的 percent 编码：配对链接来自终端输出，
// 手抄时经常被截断或转义坏掉，这里宁可原样返回也不要直接崩。
function decode(raw: string): string {
	try {
		return decodeURIComponent(raw.replace(/\+/g, " "));
	} catch {
		return raw;
	}
}

function readParams(query: string): Map<string, string> {
	const params = new Map<string, string>();
	for (const pair of query.split("&")) {
		if (pair === "") continue;
		const index = pair.indexOf("=");
		const key = decode(index < 0 ? pair : pair.slice(0, index));
		const value = index < 0 ? "" : decode(pair.slice(index + 1));
		// 同名参数以第一次出现为准，避免被后续参数覆盖。
		if (!params.has(key)) params.set(key, value);
	}
	return params;
}

// buildPairPayload 校验链接里的 url 与 token；两者缺一不可。
export function buildPairPayload(rawUrl: string, rawToken: string): PairPayload {
	const token = rawToken.trim();
	if (token === "") {
		throw new Error("配对链接缺少令牌");
	}
	return { baseUrl: normalizeBaseUrl(rawUrl), token };
}

// parsePairLink 解析控制面启动时打印的配对链接：
//
//	lework://connect?url=…&token=…             安装版 APK
//	exp://host:port/--/connect?url=…&token=…   Expo Go 调试
//
// 只按第一个 ? 之后的 query 取值，不依赖 Hermes 对自定义 scheme 的 URL
// 解析：lework:// 这种非标准协议在部分运行时里解析结果并不可靠。
export function parsePairLink(raw: string): PairPayload {
	const trimmed = raw.trim();
	const mark = trimmed.indexOf("?");
	if (mark < 0) {
		throw new Error("配对链接缺少参数");
	}
	const params = readParams(trimmed.slice(mark + 1));
	return buildPairPayload(params.get("url") ?? "", params.get("token") ?? "");
}
