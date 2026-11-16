import { normalizeBaseUrl } from "../settings/settings";

/** PairPayload 是配对链接里的控制面地址与短码。 */
export interface PairPayload {
	baseUrl: string;
	code: string;
}

// CODE_PATTERN 与服务端短码一致：字母数字，长度 4..8（默认 5）。
const CODE_PATTERN = /^[0-9A-Z]{4,8}$/;

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

/**
 * normalizeCode 把用户/二维码里的短码收敛成服务端比较的形式。
 *
 * 服务端只做大写与去首尾空格；这里额外丢弃分隔符（扫码枪与手抄常带空格、
 * 连字符），因为那些字符不可能出现在合法短码里。
 */
export function normalizeCode(raw: string): string {
	return raw.toUpperCase().replace(/[^0-9A-Z]/g, "");
}

// buildPairPayload 校验链接里的 url 与短码；两者缺一不可。
export function buildPairPayload(rawUrl: string, rawCode: string): PairPayload {
	const code = normalizeCode(rawCode);
	if (!CODE_PATTERN.test(code)) {
		throw new Error("配对码应为 4–8 位字母数字");
	}
	if (rawUrl.trim() === "") {
		throw new Error("配对链接缺少控制面地址");
	}
	return { baseUrl: normalizeBaseUrl(rawUrl), code };
}

/**
 * parsePairLink 解析 `lework://node/pair?url=…&code=…`（Expo Go 为
 * `exp://…/--/node/pair?…`）。
 *
 * 只按第一个 ? 之后取 query，不依赖 Hermes 对自定义 scheme 的 URL 解析：
 * lework:// 这种非标准协议在部分运行时里解析结果并不可靠。扫到 Metro 的
 * 二维码时给出专属提示——它和配对码长得几乎一样。
 */
export function parsePairLink(raw: string): PairPayload {
	const trimmed = raw.trim();
	const mark = trimmed.indexOf("?");
	if (mark < 0) {
		if (/^exps?:\/\//i.test(trimmed)) {
			throw new Error("这是 Expo 开发服务器的二维码；请扫 lework agent 打印的那一个");
		}
		throw new Error("配对链接缺少参数");
	}
	const params = readParams(trimmed.slice(mark + 1));
	return buildPairPayload(params.get("url") ?? "", params.get("code") ?? "");
}
