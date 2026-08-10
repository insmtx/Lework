import { afterEach, describe, expect, it, vi } from "vitest";

import { waitForGlobalAssistantOrFail } from "./assistantFallback";
import type { SendPipelineDeps } from "./deps";

vi.mock("../../api/sessionApi", () => ({
	sessionApi: {
		get: vi.fn(),
	},
}));

import { sessionApi } from "../../api/sessionApi";

function createDeps(overrides?: Partial<ReturnType<SendPipelineDeps["get"]>>): SendPipelineDeps {
	let state = {
		activeSessionId: "session-1",
		streamingMessageId: "msg-assistant-waiting-1",
		isGenerating: true,
		messagesMap: {
			"msg-assistant-waiting-1": {
				id: "msg-assistant-waiting-1",
				conversationId: "session-1",
				role: "assistant" as const,
				content: "",
				timestamp: 1,
				status: "waiting" as const,
			},
		},
		messageIds: ["msg-assistant-waiting-1"],
		...overrides,
	};
	return {
		get: () => state as never,
		set: (partial: Parameters<SendPipelineDeps["set"]>[0]) => {
			state = {
				...state,
				...(typeof partial === "function" ? partial(state as never) : partial),
			};
		},
		updateMessage: vi.fn(),
		finishStream: vi.fn(() => {
			state = { ...state, isGenerating: false, streamingMessageId: null };
		}),
		loadConversationMessages: vi.fn().mockResolvedValue(undefined),
		startGlobalEvents: vi.fn(),
		drainGlobalEvents: vi.fn(),
		effects: {
			navigateToTaskDetail: vi.fn(),
			clearComposer: vi.fn(),
		},
	} as unknown as SendPipelineDeps;
}

describe("waitForGlobalAssistantOrFail", () => {
	afterEach(() => {
		vi.useRealTimers();
		vi.clearAllMocks();
	});

	it("baseline 已是 responding 后不再轮询 GetSession，只等本地 GE 接管", async () => {
		vi.useFakeTimers();
		const get = vi.mocked(sessionApi.get).mockResolvedValue({
			data: { data: { runtime_status: "responding", message_count: 1 } },
		} as never);
		const deps = createDeps();

		const promise = waitForGlobalAssistantOrFail(deps, "session-1", "msg-assistant-waiting-1");
		await vi.advanceTimersByTimeAsync(0);
		expect(get).toHaveBeenCalledTimes(1);

		await vi.advanceTimersByTimeAsync(10_000);
		expect(get).toHaveBeenCalledTimes(1);

		deps.set({
			streamingMessageId: "msg-assistant-real",
			isGenerating: true,
		});
		await vi.advanceTimersByTimeAsync(2_000);
		await promise;
		expect(get).toHaveBeenCalledTimes(1);
	});

	it("从未 responding 时会轮询，一旦变成 responding 即停止 GetSession", async () => {
		vi.useFakeTimers();
		const get = vi
			.mocked(sessionApi.get)
			.mockResolvedValueOnce({
				data: { data: { runtime_status: "idle", message_count: 1 } },
			} as never)
			.mockResolvedValueOnce({
				data: { data: { runtime_status: "responding", message_count: 1 } },
			} as never);
		const deps = createDeps();

		const promise = waitForGlobalAssistantOrFail(deps, "session-1", "msg-assistant-waiting-1");
		await vi.advanceTimersByTimeAsync(0);
		expect(get).toHaveBeenCalledTimes(1);

		await vi.advanceTimersByTimeAsync(2_000);
		expect(get).toHaveBeenCalledTimes(2);

		await vi.advanceTimersByTimeAsync(10_000);
		expect(get).toHaveBeenCalledTimes(2);

		deps.set({ streamingMessageId: "msg-assistant-real" });
		await vi.advanceTimersByTimeAsync(2_000);
		await promise;
	});
});
