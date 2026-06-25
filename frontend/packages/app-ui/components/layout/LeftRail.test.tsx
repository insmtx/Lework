import "@testing-library/jest-dom/vitest";
import { render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { LeftRail } from "./LeftRail";

const mockAuthenticatedFetch = vi.fn();
const mockFetchProjects = vi.fn();
const mockFetchTasks = vi.fn();
const mockDeleteProject = vi.fn();
const mockSetLeftRailCollapsed = vi.fn();
const mockSetLeftRailWidth = vi.fn();
const mockSwitchView = vi.fn();
const mockSwitchProject = vi.fn();
const mockOpenTaskDetail = vi.fn();
const mockUpdateProject = vi.fn();
const mockClearComposerInput = vi.fn();
const mockSetAuthUser = vi.fn();

const mockUser = {
	publicId: "user-1",
	name: "测试用户",
	email: "test@example.com",
	avatarUrl: "http://localhost:18080/v1/files/file_TN3691n6qd/download",
};

vi.mock("@leros/store", () => ({
	authenticatedFetch: (...args: unknown[]) => mockAuthenticatedFetch(...args),
	getFileDownloadUrl: (publicId: string) => `http://localhost:18080/v1/files/${publicId}/download`,
	projectFileApi: {},
	useLayoutStore: (selector: (state: Record<string, unknown>) => unknown) =>
		selector({
			navGroups: [],
			projects: [],
			currentView: "taskDetail",
			activeProjectId: null,
			activeTaskDetailProjectId: "project-1",
			activeTaskDetailTaskId: "task-1",
			leftRailCollapsed: false,
			leftRailWidth: 240,
			fetchProjects: mockFetchProjects,
			fetchTasks: mockFetchTasks,
			deleteProject: mockDeleteProject,
			setLeftRailCollapsed: mockSetLeftRailCollapsed,
			setLeftRailWidth: mockSetLeftRailWidth,
			switchView: mockSwitchView,
			switchProject: mockSwitchProject,
			openTaskDetail: mockOpenTaskDetail,
			updateProject: mockUpdateProject,
		}),
	useChatStore: (selector: (state: Record<string, unknown>) => unknown) =>
		selector({
			clearComposerInput: mockClearComposerInput,
		}),
	useAuthStore: (selector: (state: Record<string, unknown>) => unknown) =>
		selector({
			setAuthUser: mockSetAuthUser,
		}),
	userApi: {},
}));

vi.mock("../auth", () => ({
	useAuth: () => ({
		isHydrated: true,
		isAuthenticated: true,
		openAuthDialog: vi.fn(),
		requireAuth: (afterAuth?: () => void) => {
			afterAuth?.();
			return true;
		},
		logout: vi.fn(),
		user: mockUser,
	}),
}));

vi.mock("../avatar/DiceBearAvatar", () => ({
	DiceBearAvatar: () => <div data-testid="dicebear-avatar" />,
}));

vi.mock("../../assets", () => ({
	APP_LOGO_SRC: "/logo.png",
}));

vi.mock("sonner", () => ({
	toast: {
		success: vi.fn(),
		error: vi.fn(),
	},
}));

describe("LeftRail avatar download", () => {
	beforeEach(() => {
		mockAuthenticatedFetch.mockReset();
		mockAuthenticatedFetch.mockResolvedValue({
			ok: true,
			blob: async () => new Blob(["avatar"], { type: "image/png" }),
		});
		mockFetchProjects.mockReset();
		mockFetchTasks.mockReset();
		mockDeleteProject.mockReset();
		mockSetLeftRailCollapsed.mockReset();
		mockSetLeftRailWidth.mockReset();
		mockSwitchView.mockReset();
		mockSwitchProject.mockReset();
		mockOpenTaskDetail.mockReset();
		mockUpdateProject.mockReset();
		mockClearComposerInput.mockReset();
		mockSetAuthUser.mockReset();
		window.localStorage.clear();
	});

	it("同一头像地址在父组件重渲染后不会重复下载", async () => {
		const { rerender } = render(<LeftRail />);

		await waitFor(() => {
			expect(mockAuthenticatedFetch).toHaveBeenCalledTimes(1);
		});

		rerender(<LeftRail />);

		await waitFor(() => {
			expect(mockAuthenticatedFetch).toHaveBeenCalledTimes(1);
		});
	});
});
