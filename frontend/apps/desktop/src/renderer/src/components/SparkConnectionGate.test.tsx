// @vitest-environment jsdom
import { act, useState } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

const mock = vi.hoisted(() => ({
	status: vi.fn(),
	user: { publicId: "user", jwtToken: "one", currentOrg: { id: 7, isAdmin: false } },
}));
vi.mock("@leros/store", async () => {
	const { inspectSparkDevice } = await import("../../../../../../packages/store/api/sparkDevice");
	return {
		inspectSparkDevice,
		API_BASE_URL: "http://spark.local/v1",
		sparkApi: { status: mock.status },
		useAuthStore: (select: (s: unknown) => unknown) => select({ authUser: mock.user }),
	};
});

import { SparkConnectionGate, useSparkConnection } from "./SparkConnectionGate";
import { SparkManagementCard } from "./SparkManagementCard";

const device = {
	type: "spark",
	protocol_version: 1,
	id: "spark-001",
	name: "工作站",
	management: true,
};
let container: HTMLDivElement;
let root: Root;
let fetcher: ReturnType<typeof vi.fn>;
function response(d: unknown = device) {
	return new Response(
		JSON.stringify({
			code: 0,
			data: { edition: "oss", ...(d === undefined ? {} : { device: d }) },
		}),
	);
}
function Connection() {
	const connection = useSparkConnection();
	return (
		<p>
			{connection?.device.name}:{connection?.status}
		</p>
	);
}
async function render(baseURL = "http://spark.local/v1") {
	await act(async () =>
		root.render(
			<SparkConnectionGate baseURL={baseURL}>
				<Connection />
				<SparkManagementCard />
			</SparkConnectionGate>,
		),
	);
}
beforeEach(() => {
	vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
	mock.user = { publicId: "user", jwtToken: "one", currentOrg: { id: 7, isAdmin: false } };
	mock.status.mockReset();
	fetcher = vi.fn().mockImplementation(async () => response());
	vi.stubGlobal("fetch", fetcher);
	container = document.createElement("div");
	document.body.append(container);
	root = createRoot(container);
});
afterEach(() => {
	act(() => root.unmount());
	container.remove();
	vi.unstubAllGlobals();
});
it("shows a verified Spark for a member without requesting administrator status", async () => {
	await render();
	expect(container.textContent).toContain("工作站:online");
	expect(container.textContent).toContain("管理员");
	expect(mock.status).not.toHaveBeenCalled();
});
it("keeps confirmed identity offline and restores it on reconnection", async () => {
	await render();
	fetcher.mockRejectedValue(new Error("offline"));
	await act(async () => window.dispatchEvent(new Event("online")));
	expect(container.textContent).toContain("工作站:offline");
	fetcher.mockImplementation(async () => response());
	await act(async () => window.dispatchEvent(new Event("online")));
	expect(container.textContent).toContain("工作站:online");
});
it("hides initial failures and ordinary backends", async () => {
	fetcher.mockRejectedValue(new Error("offline"));
	await render();
	expect(container.textContent).toBe("");
	fetcher.mockResolvedValue(new Response(JSON.stringify({ code: 0, data: { edition: "oss" } })));
	await act(async () => window.dispatchEvent(new Event("online")));
	expect(container.textContent).toBe("");
});
it("clears confirmed identity when the endpoint becomes ordinary", async () => {
	await render();
	fetcher.mockResolvedValue(new Response(JSON.stringify({ code: 0, data: { edition: "oss" } })));
	await act(async () => window.dispatchEvent(new Event("online")));
	expect(container.textContent).toBe("");
});
it("clears identity immediately when endpoint or account changes and ignores old responses", async () => {
	await render();
	let resolveOld!: (value: Response) => void;
	fetcher.mockImplementationOnce(
		() =>
			new Promise<Response>((resolve) => {
				resolveOld = resolve;
			}),
	);
	await act(async () => window.dispatchEvent(new Event("online")));
	fetcher.mockRejectedValue(new Error("offline"));
	await render("http://different.local/v1");
	expect(container.textContent).toBe("");
	await act(async () => resolveOld(response()));
	expect(container.textContent).toBe("");
	fetcher.mockImplementation(async () => response());
	await render();
	expect(container.textContent).toContain("工作站");
	mock.user = { ...mock.user, currentOrg: { id: 9, isAdmin: false } };
	fetcher.mockRejectedValue(new Error("offline"));
	await render();
	expect(container.textContent).toBe("");
});

it("remounts management state when a different device answers the same endpoint", async () => {
	let mounts = 0;
	function StatefulChild() {
		const [generation] = useState(() => ++mounts);
		return <p>generation:{generation}</p>;
	}
	await act(async () =>
		root.render(
			<SparkConnectionGate baseURL="http://spark.local/v1">
				<StatefulChild />
			</SparkConnectionGate>,
		),
	);
	expect(container.textContent).toBe("generation:1");
	fetcher.mockImplementation(async () => response({ ...device, id: "spark-002" }));
	await act(async () => window.dispatchEvent(new Event("online")));
	expect(container.textContent).toBe("generation:2");
});

function oldBackend() {
	fetcher.mockImplementation(
		async () => new Response(JSON.stringify({ code: 0, data: { edition: "enterprise" } })),
	);
}
async function renderIdentity(baseURL = "http://spark.local/v1") {
	await act(async () =>
		root.render(
			<SparkConnectionGate baseURL={baseURL}>
				<Connection />
			</SparkConnectionGate>,
		),
	);
}
it("recognizes a legacy Spark from validated current-service status for an administrator", async () => {
	oldBackend();
	mock.user = { ...mock.user, currentOrg: { id: 7, isAdmin: true } };
	mock.status.mockResolvedValue({ snapshot: { host_id: "legacy-host" } });
	await renderIdentity();
	expect(container.textContent).toBe("Spark:online");
	expect(mock.status).toHaveBeenCalledTimes(1);
	mock.status.mockRejectedValue(new Error("offline"));
	await act(async () => window.dispatchEvent(new Event("online")));
	expect(container.textContent).toBe("Spark:offline");
});
it("never probes administrator status for members or a different endpoint", async () => {
	oldBackend();
	await renderIdentity();
	expect(container.textContent).toBe("");
	expect(mock.status).not.toHaveBeenCalled();
	mock.user = { ...mock.user, currentOrg: { id: 7, isAdmin: true } };
	await renderIdentity("http://other.local/v1");
	expect(container.textContent).toBe("");
	expect(mock.status).not.toHaveBeenCalled();
});
it.each([
	"ordinary backend",
	"forbidden",
	"invalid status",
])("does not infer a Spark from a failed legacy probe: %s", async (reason) => {
	oldBackend();
	mock.user = { ...mock.user, currentOrg: { id: 7, isAdmin: true } };
	mock.status.mockRejectedValue(new Error(reason));
	await renderIdentity();
	expect(mock.status).toHaveBeenCalledTimes(1);
	expect(container.textContent).toBe("");
});
it("does not downgrade a malformed explicit declaration to legacy recognition", async () => {
	mock.user = { ...mock.user, currentOrg: { id: 7, isAdmin: true } };
	fetcher.mockImplementation(async () => response({ ...device, protocol_version: 99 }));
	await renderIdentity();
	expect(mock.status).not.toHaveBeenCalled();
	expect(container.textContent).toBe("");
});
it("ignores late legacy identity after changing organization", async () => {
	oldBackend();
	mock.user = { ...mock.user, currentOrg: { id: 7, isAdmin: true } };
	let resolve!: (value: unknown) => void;
	mock.status.mockImplementation(
		() =>
			new Promise((done) => {
				resolve = done;
			}),
	);
	await renderIdentity();
	expect(mock.status).toHaveBeenCalledTimes(1);
	mock.user = { ...mock.user, currentOrg: { id: 9, isAdmin: false } };
	await renderIdentity();
	await act(async () => resolve({ snapshot: { host_id: "legacy-host" } }));
	expect(container.textContent).toBe("");
});
it("does not overlap slow legacy probes on network events", async () => {
	oldBackend();
	mock.user = { ...mock.user, currentOrg: { id: 7, isAdmin: true } };
	let resolve!: (value: unknown) => void;
	mock.status.mockImplementation(
		() =>
			new Promise((done) => {
				resolve = done;
			}),
	);
	await renderIdentity();
	await act(async () => {
		window.dispatchEvent(new Event("online"));
		window.dispatchEvent(new Event("online"));
	});
	expect(mock.status).toHaveBeenCalledTimes(1);
	await act(async () => resolve({ snapshot: { host_id: "legacy-host" } }));
	expect(container.textContent).toBe("Spark:online");
});
