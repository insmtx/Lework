import { describe, expect, it } from "vitest";
import { mergeDiscoveryServices, parseDiscoveryService } from "../discovery-protocol";

const service = {
	name: "Spark",
	host: "spark.local.",
	port: 38081,
	txt: { v: "1", id: "unit-1", scheme: "http", path: "/v1" },
};
describe("discovery protocol", () => {
	it("builds the advertised address without hardcoded IP or port", () => {
		expect(parseDiscoveryService(service)?.url).toBe("http://spark.local:38081/v1");
	});
	it("rejects malformed endpoints and unsupported protocols", () => {
		for (const invalid of [
			{ ...service, port: 0 },
			{ ...service, host: "evil.local/path" },
			{ ...service, txt: { ...service.txt, v: "2" } },
			{ ...service, txt: { ...service.txt, path: "//evil.test" } },
		]) {
			expect(parseDiscoveryService(invalid)).toBeNull();
		}
	});
	it("deduplicates interfaces and flags identity conflicts", () => {
		const first = parseDiscoveryService(service);
		if (!first) throw new Error("fixture not parsed");
		expect(mergeDiscoveryServices([first, first])).toHaveLength(1);
		const other = { ...first, url: "http://other.local:38081/v1" };
		expect(mergeDiscoveryServices([first, other]).every((row) => row.conflict)).toBe(true);
		expect(mergeDiscoveryServices([first, { ...first, id: "unit-2" }])).toHaveLength(2);
	});
});

it("merges local interface candidates without conflating different hosts", () => {
	const lan = { address: "192.168.1.5", netmask: "255.255.255.0" };
	const first = parseDiscoveryService(
		{
			...service,
			addresses: ["192.168.1.20", "8.8.8.8", "127.0.0.1", "192.168.2.3", "169.254.0.1"],
		},
		[lan],
	);
	const second = parseDiscoveryService({ ...service, addresses: ["169.254.22.8"] }, [
		{ address: "169.254.22.2", netmask: "255.255.0.0" },
	]);
	if (!first || !second) throw new Error("fixture parse failed");
	const rows = mergeDiscoveryServices([first, second]);
	expect(rows).toHaveLength(1);
	expect(rows[0].conflict).toBe(false);
	expect(rows[0].candidates).toEqual([
		{ url: "http://169.254.22.8:38081/v1", transport: "link-local" },
		{ url: "http://192.168.1.20:38081/v1", transport: "local-network" },
		{ url: "http://spark.local:38081/v1", transport: "hostname" },
	]);
});
it("bounds broadcast address lists", () => {
	const row = parseDiscoveryService(
		{
			...service,
			addresses: Array.from({ length: 1000 }, (_, i) => `192.168.1.${(i % 200) + 10}`),
		},
		[{ address: "192.168.1.2", netmask: "255.255.255.0" }],
	);
	expect(row?.candidates?.length).toBeLessThanOrEqual(4);
});

it("prefers link-local within bounded multi-address broadcasts and rejects reused endpoints", () => {
	const row = parseDiscoveryService(
		{ ...service, addresses: ["192.168.1.10", "192.168.1.11", "192.168.1.12", "169.254.22.8"] },
		[
			{ address: "192.168.1.2", netmask: "255.255.255.0" },
			{ address: "169.254.22.2", netmask: "255.255.0.0" },
		],
	);
	if (!row) throw new Error("fixture parse failed");
	expect(row.candidates?.[0].url).toBe("http://169.254.22.8:38081/v1");
	expect(mergeDiscoveryServices([row, { ...row, id: "forged" }]).every((s) => s.conflict)).toBe(
		true,
	);
});
