import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { LeftRail } from "./LeftRail";

const mockAuthenticatedFetch = vi.fn();
const mockFetchFilePreviewByPublicId = vi.fn();
const mockFetchTasks = vi.fn();
const mockDeleteProject = vi.fn();
const mockSetLeftRailCollapsed = vi.fn();
const mockSetLeftRailWidth = vi.fn();
const mockSwitchView = vi.fn();
const mockSwitchProject = vi.fn();
const mockOpenTaskDetail = vi.fn();
const mockUpdateProject = vi.fn();
const mockUpsertProjects = vi.fn();
const mockClearComposerInput = vi.fn();
const mockSetAuthUser = vi.fn();
const mockLogout = vi.fn();

let mockIsAuthenticated = true;
let mockProjects: Array<{
	id: string;
	name: string;
	updatedAt: number;
	tasks: Array<{ id: string; title: string }>;
}> = [];

const mockUser = {
	publicId: "user-1",
	name: "测试用户",
	email: "test@example.com",
	avatarUrl: "http://localhost:18080/v1/files/file_TN3691n6qd/download",
	currentOrg: { id: 1, name: "组织 1" },
};

vi.mock("@leros/store", () => ({
	Action: {},
	authenticatedFetch: (...args: unknown[]) => mockAuthenticatedFetch(...args),
	fetchFilePreviewByPublicId: (...args: unknown[]) => mockFetchFilePreviewByPublicId(...args),
	getNativeFileInputAccept: () => "image/*",
	isPrivateDeployment: false,
	normalizeFilePublicId: (value?: string) => value?.match(/file_[A-Za-z0-9_-]+/)?.[0],
	LEFT_RAIL_MAX_WIDTH: 360,
	LEFT_RAIL_MIN_WIDTH: 220,
	PROJECT_LIST_PAGE_SIZE: 20,
	fetchProjectListPage: async () => ({
		items: mockProjects,
		total: mockProjects.length,
		offset: 0,
		hasMore: false,
	}),
	mergeProjectsFromListResult: (items: unknown[]) => items,
	upsertProjectsIntoCache: (incoming: Array<{ id: string }>, local: Array<{ id: string }>) => {
		const incomingIds = new Set(incoming.map((project) => project.id));
		return [...incoming, ...local.filter((project) => !incomingIds.has(project.id))];
	},
	appendProjectsFromListResult: (incoming: unknown[], local: unknown[]) => [...local, ...incoming],
	projectFileApi: {},
	useProjectMenuCapabilities: () => ({ loading: false, hasAny: false }),
	useProjectsMenuCapabilities: vi.fn(),
	useTaskCapabilities: () => ({ loading: false, can: () => false }),
	useLayoutStore: (selector: (state: Record<string, unknown>) => unknown) =>
		selector({
			navGroups: [],
			projects: mockProjects,
			currentView: "taskDetail",
			activeProjectId: null,
			activeTaskDetailProjectId: "project-1",
			activeTaskDetailTaskId: "task-1",
			projectsMutationEpoch: 0,
			leftRailCollapsed: false,
			leftRailWidth: 240,
			upsertProjects: mockUpsertProjects,
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
	useGlobalConfigStore: (selector: (state: Record<string, unknown>) => unknown) =>
		selector({
			edition: "enterprise",
		}),
	userApi: {},
}));

vi.mock("../auth", () => ({
	useAuth: () => ({
		isHydrated: true,
		isAuthenticated: mockIsAuthenticated,
		openAuthDialog: vi.fn(),
		requireAuth: (afterAuth?: () => void) => {
			if (mockIsAuthenticated) {
				afterAuth?.();
				return true;
			}
			return false;
		},
		logout: mockLogout,
		user: mockIsAuthenticated ? mockUser : null,
	}),
}));

vi.mock("../avatar/DiceBearAvatar", () => ({
	DiceBearAvatar: () => <div data-testid="dicebear-avatar" />,
}));

vi.mock("../private-deployment/useBrandIdentity", () => ({
	useBrandIdentity: () => ({ logo: null, name: "Lework" }),
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

afterEach(cleanup);

describe("LeftRail avatar download", () => {
	beforeEach(() => {
		mockIsAuthenticated = true;
		mockProjects = [];
		mockUser.avatarUrl = "http://localhost:18080/v1/files/file_TN3691n6qd/download";
		mockAuthenticatedFetch.mockReset();
		mockAuthenticatedFetch.mockResolvedValue({
			ok: true,
			blob: async () => new Blob(["avatar"], { type: "image/png" }),
		});
		mockFetchFilePreviewByPublicId.mockReset();
		mockFetchFilePreviewByPublicId.mockResolvedValue({
			ok: true,
			blob: async () => new Blob(["avatar"], { type: "image/png" }),
		});
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
			expect(mockFetchFilePreviewByPublicId).toHaveBeenCalledTimes(1);
		});
		expect(mockAuthenticatedFetch).not.toHaveBeenCalled();

		rerender(<LeftRail />);

		await waitFor(() => {
			expect(mockFetchFilePreviewByPublicId).toHaveBeenCalledTimes(1);
		});
	});
});

describe("LeftRail project expansion", () => {
	beforeEach(() => {
		mockIsAuthenticated = true;
		mockProjects = [{ id: "project-1", name: "测试项目", tasks: [], updatedAt: 1 }];
		mockFetchTasks.mockReset();
		window.localStorage.clear();
	});

	it("登出后会重置项目展开状态", async () => {
		mockFetchTasks.mockResolvedValue(undefined);

		const { rerender } = render(<LeftRail />);

		fireEvent.click(await screen.findByText("测试项目"));
		await waitFor(() => {
			expect(screen.getByText("暂无任务")).toBeInTheDocument();
		});

		mockIsAuthenticated = false;
		mockProjects = [];
		rerender(<LeftRail />);

		expect(screen.queryByText("暂无任务")).not.toBeInTheDocument();
	});

	it("切换组织后会重置项目展开状态", async () => {
		mockFetchTasks.mockResolvedValue(undefined);
		mockUser.currentOrg = { id: 1, name: "组织 1" };

		const { rerender } = render(<LeftRail />);

		fireEvent.click(await screen.findByText("测试项目"));
		await waitFor(() => {
			expect(screen.getByText("暂无任务")).toBeInTheDocument();
		});

		mockUser.currentOrg = { id: 2, name: "组织 2" };
		rerender(<LeftRail />);

		expect(screen.queryByText("暂无任务")).not.toBeInTheDocument();
	});

	it("展开项目时先显示加载状态", async () => {
		let resolveFetch: (() => void) | undefined;
		mockFetchTasks.mockReturnValue(
			new Promise<void>((resolve) => {
				resolveFetch = resolve;
			}),
		);

		render(<LeftRail />);
		fireEvent.click(await screen.findByText("测试项目"));

		expect(screen.getByText("任务加载中...")).toBeInTheDocument();
		expect(screen.queryByText("暂无任务")).not.toBeInTheDocument();

		resolveFetch?.();
		await waitFor(() => {
			expect(screen.queryByText("任务加载中...")).not.toBeInTheDocument();
		});
		expect(screen.getByText("暂无任务")).toBeInTheDocument();
	});
});
