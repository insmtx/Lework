import type { DiscoveredServer } from "../shared/discovery";

export function parseDiscoveryService(value: unknown): DiscoveredServer | null {
	if (!value || typeof value !== "object") return null;
	const s = value as { host?: unknown; port?: unknown; name?: unknown; txt?: unknown };
	if (!s.txt || typeof s.txt !== "object") return null;
	const txt = s.txt as { v?: unknown; id?: unknown; scheme?: unknown; path?: unknown };
	const host = typeof s.host === "string" ? s.host.replace(/\.$/, "") : "";
	if (txt.v !== "1" || typeof txt.id !== "string" || !/^[a-zA-Z0-9_-]{1,128}$/.test(txt.id))
		return null;
	if (
		host.length > 253 ||
		!/^(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+local$/i.test(host)
	)
		return null;
	if (typeof s.port !== "number" || !Number.isInteger(s.port) || s.port < 1 || s.port > 65535)
		return null;
	if ((txt.scheme !== "http" && txt.scheme !== "https") || txt.path !== "/v1") return null;
	return {
		id: txt.id,
		name: typeof s.name === "string" ? s.name.slice(0, 128) : host,
		url: `${txt.scheme}://${host.toLowerCase()}:${s.port}/v1`,
	};
}

export function mergeDiscoveryServices(servers: DiscoveredServer[]): DiscoveredServer[] {
	const unique = [...new Map(servers.map((s) => [`${s.id}|${s.url}`, s])).values()];
	return unique.map((s) => ({
		...s,
		conflict:
			Boolean(s.conflict) || unique.some((other) => other.id === s.id && other.url !== s.url),
	}));
}
