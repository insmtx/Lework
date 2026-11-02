import { isIPv4 } from "node:net";
import type { DiscoveredServer, DiscoveryCandidate } from "../shared/discovery";

export function parseDiscoveryService(
	value: unknown,
	networks: { address: string; netmask: string }[] = [],
): DiscoveredServer | null {
	if (!value || typeof value !== "object") return null;
	const s = value as {
		host?: unknown;
		port?: unknown;
		name?: unknown;
		txt?: unknown;
		addresses?: unknown;
	};
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
	const url = `${txt.scheme}://${host.toLowerCase()}:${s.port}/v1`;
	const candidates: DiscoveryCandidate[] = [];
	if (Array.isArray(s.addresses)) {
		for (const address of s.addresses.slice(0, 32)) {
			if (typeof address !== "string" || !isIPv4(address)) continue;
			const [a, b, c] = address.split(".").map(Number);
			const linkLocal = a === 169 && b === 254 && c > 0 && c < 255;
			const privateIP = a === 10 || (a === 172 && b >= 16 && b <= 31) || (a === 192 && b === 168);
			if ((!linkLocal && !privateIP) || !networks.some((n) => sameNetwork(address, n))) continue;
			if (!candidates.some((c) => c.url === `${txt.scheme}://${address}:${s.port}/v1`))
				candidates.push({
					url: `${txt.scheme}://${address}:${s.port}/v1`,
					transport: linkLocal ? "link-local" : "local-network",
				});
		}
	}
	candidates.sort(
		(a, b) => Number(b.transport === "link-local") - Number(a.transport === "link-local"),
	);
	candidates.splice(3);
	candidates.push({ url, transport: "hostname" });
	return {
		candidates,
		id: txt.id,
		name: typeof s.name === "string" ? s.name.slice(0, 128) : host,
		url,
	};
}

function sameNetwork(address: string, network: { address: string; netmask: string }): boolean {
	if (!isIPv4(network.address) || !isIPv4(network.netmask)) return false;
	const number = (ip: string) => ip.split(".").reduce((v, b) => (v << 8) | Number(b), 0) >>> 0;
	const mask = number(network.netmask);
	if (!mask) return false;
	const ip = number(address);
	const host = (ip & ~mask) >>> 0;
	return (ip & mask) === (number(network.address) & mask) && host !== 0 && host !== ~mask >>> 0;
}

export function mergeDiscoveryServices(servers: DiscoveredServer[]): DiscoveredServer[] {
	const unique = new Map<string, DiscoveredServer>();
	for (const server of servers) {
		const key = `${server.id}|${server.url}`;
		const previous = unique.get(key);
		const paths = [
			...(previous?.candidates ?? []),
			...(server.candidates ?? [{ url: server.url, transport: "hostname" as const }]),
		];
		const candidates = [...new Map(paths.map((p) => [p.url, p])).values()];
		const rank = { "link-local": 0, "local-network": 1, hostname: 2 };
		candidates.sort((a, b) => rank[a.transport] - rank[b.transport]);
		const direct = candidates.filter((p) => p.transport !== "hostname").slice(0, 3);
		unique.set(key, {
			...server,
			conflict: previous?.conflict || server.conflict,
			candidates: [...direct, { url: server.url, transport: "hostname" }],
		});
	}
	const rows = [...unique.values()];
	return rows.map((s) => ({
		...s,
		conflict:
			Boolean(s.conflict) ||
			rows.some(
				(other) =>
					(other.id === s.id && other.url !== s.url) ||
					(other.id !== s.id && new URL(other.url).hostname === new URL(s.url).hostname),
			),
	}));
}
