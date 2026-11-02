// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, expect, it, vi } from "vitest";

const inspect = vi.hoisted(() => vi.fn());
vi.mock("@leros/store", () => ({
	API_BASE_URL: "http://current.local:38081/v1",
	inspectSparkDevice: inspect,
}));
afterEach(() => {
	vi.useRealTimers();
	vi.clearAllMocks();
});

import { SparkDiscoveryList } from "./SparkDiscoveryList";

it("only selects a discovered address on an explicit click", async () => {
	inspect.mockResolvedValue(null);
	const scan = vi.fn().mockResolvedValue({
		servers: [{ id: "a", name: "Spark", url: "http://spark.local:38081/v1" }],
	});
	window.lerosDiscovery = { scan };
	const container = document.createElement("div");
	document.body.append(container);
	const root = createRoot(container);
	const select = vi.fn();
	try {
		await act(async () =>
			root.render(<SparkDiscoveryList onSelect={select} disabled={false} autoStart />),
		);
		expect(scan).toHaveBeenCalledOnce();
		expect(select).not.toHaveBeenCalled();
		const candidate = [...container.querySelectorAll("button")].find((b) =>
			b.textContent?.includes("spark.local"),
		);
		expect(candidate).toBeDefined();
		await act(async () => candidate?.click());
		expect(select).toHaveBeenCalledWith("http://spark.local:38081/v1");
		expect(container.textContent).not.toContain("正在验证连接");
	} finally {
		act(() => root.unmount());
		container.remove();
	}
});

it("prefers an anonymously verified direct path only on explicit connect and rejects mismatched IDs", async () => {
	const row = {
		id: "a",
		name: "Spark",
		url: "http://spark.local:38081/v1",
		candidates: [
			{ url: "http://169.254.22.8:38081/v1", transport: "link-local" as const },
			{ url: "http://192.168.1.20:38081/v1", transport: "local-network" as const },
			{ url: "http://spark.local:38081/v1", transport: "hostname" as const },
		],
	};
	inspect.mockImplementation(async (url) => ({
		id: url.includes("169.254") ? "forged" : "a",
		type: "spark",
		protocol_version: 1,
		name: "Spark",
		management: true,
	}));
	window.lerosDiscovery = { scan: vi.fn().mockResolvedValue({ servers: [row] }) };
	const container = document.createElement("div");
	document.body.append(container);
	const root = createRoot(container);
	const select = vi.fn();
	try {
		await act(async () =>
			root.render(<SparkDiscoveryList onSelect={select} disabled={false} autoStart />),
		);
		expect(inspect).not.toHaveBeenCalled();
		const button = [...container.querySelectorAll("button")].find((b) =>
			b.textContent?.includes("spark.local"),
		);
		if (!button) throw new Error("device button missing");
		await act(async () => {
			button.click();
			button.click();
		});
		expect(select).toHaveBeenCalledOnce();
		expect(select).toHaveBeenCalledWith("http://192.168.1.20:38081/v1");
	} finally {
		act(() => root.unmount());
		container.remove();
	}
});
it("network reset clears results, starts a fresh scan and ignores older completion", async () => {
	let reset!: () => void;
	let finishOld!: (v: unknown) => void;
	const scan = vi
		.fn()
		.mockImplementationOnce(
			() =>
				new Promise((resolve) => {
					finishOld = resolve;
				}),
		)
		.mockResolvedValue({ servers: [] });
	window.lerosDiscovery = {
		scan,
		subscribeReset: (listener) => {
			reset = listener;
			return vi.fn();
		},
	};
	const container = document.createElement("div");
	const root = createRoot(container);
	try {
		await act(async () =>
			root.render(<SparkDiscoveryList onSelect={vi.fn()} disabled={false} autoStart />),
		);
		await act(async () => reset());
		expect(scan).toHaveBeenCalledTimes(2);
		await act(async () =>
			finishOld({ servers: [{ id: "old", name: "Old Spark", url: "http://old.local/v1" }] }),
		);
		expect(container.textContent).not.toContain("Old Spark");
	} finally {
		act(() => root.unmount());
		container.remove();
	}
});
it("expires old devices and ignores validation after unmount", async () => {
	vi.useFakeTimers();
	window.lerosDiscovery = {
		scan: vi.fn().mockResolvedValue({
			servers: [{ id: "a", name: "Spark", url: "http://spark.local:38081/v1" }],
		}),
	};
	let finish!: (v: unknown) => void;
	inspect.mockImplementation(
		() =>
			new Promise((resolve) => {
				finish = resolve;
			}),
	);
	const container = document.createElement("div");
	const root = createRoot(container);
	const select = vi.fn();
	await act(async () =>
		root.render(<SparkDiscoveryList onSelect={select} disabled={false} autoStart />),
	);
	const button = [...container.querySelectorAll("button")].find((b) =>
		b.textContent?.includes("spark.local"),
	);
	if (!button) throw new Error("device button missing");
	await act(async () => button.click());
	await act(async () => vi.advanceTimersByTimeAsync(31000));
	expect(container.textContent).not.toContain("spark.local");
	act(() => root.unmount());
	await act(async () => finish(null));
	expect(select).not.toHaveBeenCalled();
});

it("keeps the active hostname path despite a newly available direct path", async () => {
	const row = {
		id: "a",
		name: "Spark",
		url: "http://current.local:38081/v1",
		candidates: [
			{ url: "http://169.254.22.8:38081/v1", transport: "link-local" as const },
			{ url: "http://current.local:38081/v1", transport: "hostname" as const },
		],
	};
	inspect.mockResolvedValue({ id: "a" });
	window.lerosDiscovery = { scan: vi.fn().mockResolvedValue({ servers: [row] }) };
	const container = document.createElement("div");
	const root = createRoot(container);
	const select = vi.fn();
	try {
		await act(async () =>
			root.render(<SparkDiscoveryList onSelect={select} disabled={false} autoStart />),
		);
		await act(async () =>
			[...container.querySelectorAll("button")]
				.find((b) => b.textContent?.includes("current.local"))
				?.click(),
		);
		expect(select).toHaveBeenCalledWith("http://current.local:38081/v1");
		expect(inspect).toHaveBeenCalledTimes(2);
	} finally {
		act(() => root.unmount());
	}
});

it("uses a successful verified candidate when a sibling times out", async () => {
	vi.useFakeTimers();
	inspect.mockImplementation((url, signal) =>
		url.includes("169.254")
			? new Promise((_, reject) =>
					signal.addEventListener("abort", () => reject(new Error("timeout"))),
				)
			: Promise.resolve({ id: "a" }),
	);
	window.lerosDiscovery = {
		scan: vi.fn().mockResolvedValue({
			servers: [
				{
					id: "a",
					name: "Spark",
					url: "http://spark.local:38081/v1",
					candidates: [
						{ url: "http://169.254.22.8:38081/v1", transport: "link-local" },
						{ url: "http://spark.local:38081/v1", transport: "hostname" },
					],
				},
			],
		}),
	};
	const container = document.createElement("div");
	const root = createRoot(container);
	const select = vi.fn();
	try {
		await act(async () =>
			root.render(<SparkDiscoveryList onSelect={select} disabled={false} autoStart />),
		);
		await act(async () =>
			[...container.querySelectorAll("button")]
				.find((b) => b.textContent?.includes("spark.local"))
				?.click(),
		);
		await act(async () => vi.advanceTimersByTimeAsync(8000));
		expect(select).toHaveBeenCalledWith("http://spark.local:38081/v1");
	} finally {
		act(() => root.unmount());
	}
});

it("manual editing cancels an older discovered candidate before it can select", async () => {
	let finish!: (value: unknown) => void;
	inspect.mockImplementation(
		() =>
			new Promise((resolve) => {
				finish = resolve;
			}),
	);
	window.lerosDiscovery = {
		scan: vi.fn().mockResolvedValue({
			servers: [{ id: "a", name: "Spark", url: "http://spark.local:38081/v1" }],
		}),
	};
	const container = document.createElement("div");
	const root = createRoot(container);
	const select = vi.fn();
	try {
		await act(async () =>
			root.render(<SparkDiscoveryList onSelect={select} disabled={false} autoStart />),
		);
		await act(async () =>
			[...container.querySelectorAll("button")]
				.find((b) => b.textContent?.includes("spark.local"))
				?.click(),
		);
		await act(async () => window.dispatchEvent(new Event("leros:connection-selection-cancelled")));
		await act(async () => finish(null));
		expect(select).not.toHaveBeenCalled();
	} finally {
		act(() => root.unmount());
	}
});
it("expiry invalidates a parent validation or pending confirmation", async () => {
	vi.useFakeTimers();
	inspect.mockResolvedValue(null);
	window.lerosDiscovery = {
		scan: vi.fn().mockResolvedValue({
			servers: [{ id: "a", name: "Spark", url: "http://spark.local:38081/v1" }],
		}),
	};
	const expired = vi.fn();
	window.addEventListener("leros:connection-invalidated", expired);
	const container = document.createElement("div");
	const root = createRoot(container);
	try {
		await act(async () =>
			root.render(
				<SparkDiscoveryList
					onSelect={() =>
						new Promise(() => {
							/* 保持父验证挂起以测试过期。 */
						})
					}
					disabled={false}
					autoStart
				/>,
			),
		);
		await act(async () =>
			[...container.querySelectorAll("button")]
				.find((b) => b.textContent?.includes("spark.local"))
				?.click(),
		);
		await act(async () => vi.advanceTimersByTimeAsync(30000));
		expect(expired).toHaveBeenCalledOnce();
	} finally {
		act(() => root.unmount());
		window.removeEventListener("leros:connection-invalidated", expired);
	}
});

it("searches local interfaces after a main reset even without internet connectivity", async () => {
	const online = vi.spyOn(navigator, "onLine", "get").mockReturnValue(false);
	let reset!: () => void;
	const scan = vi.fn().mockResolvedValue({ servers: [] });
	window.lerosDiscovery = {
		scan,
		subscribeReset: (listener) => {
			reset = listener;
			return vi.fn();
		},
	};
	const container = document.createElement("div");
	const root = createRoot(container);
	try {
		await act(async () => root.render(<SparkDiscoveryList onSelect={vi.fn()} disabled={false} />));
		await act(async () => reset());
		expect(scan).toHaveBeenCalledOnce();
	} finally {
		act(() => root.unmount());
		online.mockRestore();
	}
});

it.each([
	"rejected",
	"timeout",
])("checks the new LAN address when the saved hostname is %s", async (failure) => {
	vi.useFakeTimers();
	inspect.mockImplementation((url, signal) => {
		if (url.includes("current.local")) {
			if (failure === "timeout")
				return new Promise((_, reject) =>
					signal.addEventListener("abort", () => reject(new Error("timeout"))),
				);
			return Promise.reject(new Error("unreachable"));
		}
		return Promise.resolve({ id: "a" });
	});
	window.lerosDiscovery = {
		scan: vi.fn().mockResolvedValue({
			servers: [
				{
					id: "a",
					name: "Spark",
					url: "http://current.local:38081/v1",
					candidates: [
						{ url: "http://192.168.43.6:38081/v1", transport: "local-network" },
						{ url: "http://current.local:38081/v1", transport: "hostname" },
					],
				},
			],
		}),
	};
	const container = document.createElement("div");
	const root = createRoot(container);
	const select = vi.fn();
	try {
		await act(async () =>
			root.render(<SparkDiscoveryList onSelect={select} disabled={false} autoStart />),
		);
		await act(async () =>
			[...container.querySelectorAll("button")]
				.find((b) => b.textContent?.includes("current.local"))
				?.click(),
		);
		await act(async () => vi.advanceTimersByTimeAsync(8000));
		expect(select).toHaveBeenCalledExactlyOnceWith("http://192.168.43.6:38081/v1");
	} finally {
		act(() => root.unmount());
	}
});
