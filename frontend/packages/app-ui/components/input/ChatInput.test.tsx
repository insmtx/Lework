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
let mockProjectMembers: Array<Record<string, unknown>> = [];

class ResizeObserverStub {
	observe = vi.fn();
	unobserve = vi.fn();
	disconnect = vi.fn();
}

vi.stubGlobal("ResizeObserver", ResizeObserverStub);

vi.mock("@leros/store", () => ({
	useChatStore: (selector: (state: Record<string, unknown>) => unknown) =>
		selector({
			activeSessionId: null,
			inputText: "项目首页首条提问",
			inputAttachments: [],
			isGenerating: false,
			cancellingSessionId: null,
			messagesMap: {},
			messageIds: [],
			selectedModel: "gpt-4.1",
			executionMode: "default",
			modelOptions: [{ id: "gpt-4.1", label: "GPT-4.1" }],
			setInputText: mockSetInputText,
			sendProjectMessage: mockSendProjectMessage,
			sendTaskRoomMessage: vi.fn(),
			submitApprovalDecision: vi.fn(),
			submitQuestionAnswer: vi.fn(),
			cancelGeneration: vi.fn(),
			addAttachment: vi.fn(),
			addUploadedAttachment: vi.fn(),
			addUploadedFolderAttachment: vi.fn(),
			removeAttachment: vi.fn(),
			setInputFocused: mockSetInputFocused,
			setSelectedModel: vi.fn(),
			setExecutionMode: mockSetExecutionMode,
		}),
	COMPOSER_UPLOAD_ACCEPT: ".txt",
	COMPOSER_UPLOAD_EMPTY_FILE_MESSAGE: "不能上传空文件",
	COMPOSER_UPLOAD_SUCCESS_MESSAGE: "文件上传成功",
	COMPOSER_UPLOAD_TYPE_REJECTED_MESSAGE: "不支持的文件类型",
	getComposerUploadAccept: () => ".txt",
	isComposerUploadAllowedFile: () => true,
	isEmptyUploadFile: () => false,
	hasComposerSkillTokens: () => false,
	prepareOutgoingComposer: (content: string) => ({ content }),
	useDAStore: (selector: (state: Record<string, unknown>) => unknown) =>
		selector({
			assistants: [],
			assistantsLoaded: true,
		}),
	useSkillStore: (selector: (state: Record<string, unknown>) => unknown) =>
		selector({
			installedSkills: [],
			installedSkillsLoaded: true,
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
					members: mockProjectMembers,
				},
			],
		}),
	pluginApi: {
		list: () => Promise.resolve({ data: { code: 0, message: "success", data: { plugins: [] } } }),
		listProject: () => Promise.resolve({ data: { code: 0, message: "success", data: [] } }),
		listBuiltinSkills: () =>
			Promise.resolve({ data: { code: 0, message: "success", data: { plugins: [] } } }),
	},
	officialPluginMarketplaceApi: {
		list: () => Promise.resolve({ data: { code: 0, message: "success", data: { items: [] } } }),
	},
	pluginToComposerOption: (item: Record<string, unknown>) => ({
		code: item.code ?? "",
		label: item.name ?? item.code ?? "",
		description: (item.description as string) ?? "",
		keywords: [],
	}),
	mergeSkillOptions: (
		project: unknown[],
		org: unknown[],
		marketplace: unknown[],
		builtin: unknown[],
	) => [...project, ...org, ...marketplace, ...builtin],
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
		mockProjectMembers = [];
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
					goToProjectTasks: vi.fn(),
					goToTaskDetail: mockGoToTaskDetail,
					goToAutomationDetail: vi.fn(),
				}}
			/>,
		);

		await user.click(screen.getByRole("button", { name: "发送" }));

		expect(mockSendProjectMessage).toHaveBeenCalledWith(
			"项目首页首条提问",
			"project-1",
			[],
			undefined,
			{ connectorIds: [] },
		);
		expect(mockGoToTaskDetail).toHaveBeenCalledWith("project-1", "task-9", "session-7");
	});

	it("始终显示 Plan Mode 开关并可开启", async () => {
		const user = userEvent.setup();
		render(<ChatInput />);

		await user.click(screen.getByRole("button", { name: "Plan Mode" }));

		expect(mockSetExecutionMode).toHaveBeenCalledWith("plan");
	});

	it("多人真人项目禁用添加连接器并显示隐私提示", async () => {
		mockProjectMembers = [
			{ id: "user-1", memberId: 1, publicId: "user-1", type: "user", role: "owner" },
			{ id: "user-2", memberId: 2, publicId: "user-2", type: "user", role: "member" },
		];
		const user = userEvent.setup();
		render(<ChatInput variant="project" />);

		const connectorButton = screen.getByRole("button", { name: /添加连接器/ });
		expect(connectorButton).toHaveAttribute("aria-disabled", "true");

		await user.hover(connectorButton);
		expect(
			await screen.findByText(
				"项目包含多名真人队友，为保护个人连接器数据，任务执行时不会使用 MCP 连接器。",
			),
		).toBeInTheDocument();

		await user.click(connectorButton);
		expect(screen.queryByText("选择连接器")).not.toBeInTheDocument();
	});

	it("单真人项目仍可打开连接器选择器", async () => {
		mockProjectMembers = [
			{ id: "user-1", memberId: 1, publicId: "user-1", type: "user", role: "owner" },
		];
		const user = userEvent.setup();
		render(<ChatInput variant="project" />);

		const connectorButton = screen.getByRole("button", { name: "添加连接器" });
		expect(connectorButton).not.toHaveAttribute("aria-disabled");

		await user.click(connectorButton);
		expect(await screen.findByText("选择连接器")).toBeInTheDocument();
	});
});
