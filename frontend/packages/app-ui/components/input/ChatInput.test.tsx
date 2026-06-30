import "@testing-library/jest-dom/vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ChatInput } from "./ChatInput";

afterEach(cleanup);

const mockSendProjectMessage = vi.fn();
const mockSetInputText = vi.fn();
const mockSetInputFocused = vi.fn();
const mockSetExecutionMode = vi.fn();
const mockGoToTaskDetail = vi.fn();

vi.mock("@leros/store", () => ({
	useChatStore: (selector: (state: Record<string, unknown>) => unknown) =>
		selector({
			activeSessionId: null,
			inputText: "项目首页首条提问",
			inputAttachments: [],
			isGenerating: false,
			messagesMap: {},
			messageIds: [],
			selectedModel: "gpt-4.1",
			executionMode: "default",
			modelOptions: [{ id: "gpt-4.1", label: "GPT-4.1" }],
			setInputText: mockSetInputText,
			sendMessage: vi.fn(),
			sendProjectMessage: mockSendProjectMessage,
			submitApprovalDecision: vi.fn(),
			submitQuestionAnswer: vi.fn(),
			cancelGeneration: vi.fn(),
			addAttachment: vi.fn(),
			addUploadedAttachment: vi.fn(),
			removeAttachment: vi.fn(),
			setInputFocused: mockSetInputFocused,
			setSelectedModel: vi.fn(),
			setExecutionMode: mockSetExecutionMode,
		}),
	useLayoutStore: (selector: (state: Record<string, unknown>) => unknown) =>
		selector({
			activeProjectId: "project-1",
			activeTaskDetailProjectId: null,
			currentView: "project",
			projects: [
				{
					id: "project-1",
					name: "测试项目",
					description: "",
					emoji: "📁",
					createdAt: "2026-06-26",
					updatedAt: "2026-06-26",
					tasks: [],
					artifacts: [],
					files: [],
					messages: [],
					skills: [],
				},
			],
		}),
}));

vi.mock("./StructuredComposer", () => ({
	StructuredComposer: ({
		value,
		onChange,
		onSubmit,
		onFocus,
		onBlur,
		placeholder,
	}: {
		value: string;
		onChange: (value: string) => void;
		onSubmit: () => void;
		onFocus: () => void;
		onBlur: () => void;
		placeholder?: string;
	}) => (
		<div>
			<textarea
				aria-label="chat-input"
				placeholder={placeholder}
				value={value}
				onChange={(event) => onChange(event.target.value)}
				onFocus={onFocus}
				onBlur={onBlur}
			/>
			<button type="button" onClick={onSubmit}>
				发送
			</button>
		</div>
	),
}));

describe("ChatInput", () => {
	beforeEach(() => {
		mockSendProjectMessage.mockReset();
		mockSetInputText.mockReset();
		mockSetInputFocused.mockReset();
		mockSetExecutionMode.mockReset();
		mockGoToTaskDetail.mockReset();
	});

	it("在项目首页发送消息后跳转到新任务详情页", async () => {
		mockSendProjectMessage.mockResolvedValue({
			project_id: "project-1",
			task_id: "task-9",
			session_id: "session-7",
		});

		const user = userEvent.setup();

		render(
			<ChatInput
				variant="project"
				navigation={{
					currentPath: "/projects/project-1",
					goToRoute: vi.fn(),
					goToProject: vi.fn(),
					goToTaskDetail: mockGoToTaskDetail,
				}}
			/>,
		);

		await user.click(screen.getByRole("button", { name: "发送" }));

		expect(mockSendProjectMessage).toHaveBeenCalledWith(
			"项目首页首条提问",
			"project-1",
			[],
			undefined,
		);
		expect(mockGoToTaskDetail).toHaveBeenCalledWith("project-1", "task-9", "session-7");
	});

	it("始终显示 Plan Mode 开关并可开启", async () => {
		const user = userEvent.setup();
		render(<ChatInput />);

		await user.click(screen.getByRole("button", { name: "Plan Mode" }));

		expect(mockSetExecutionMode).toHaveBeenCalledWith("plan");
	});
});
