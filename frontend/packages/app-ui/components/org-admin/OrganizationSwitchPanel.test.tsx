import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { OrganizationSwitchPanel } from "./OrganizationSwitchPanel";

const mockRefreshAuthSession = vi.fn();
const mockSwitchOrganization = vi.fn();
const mockCreateOrganization = vi.fn();
const mockFetchProjects = vi.fn();
const mockFetchAssistants = vi.fn();
const mockFetchInstalledSkills = vi.fn();
const mockResetLayout = vi.fn();
const mockResetAssistants = vi.fn();
const mockResetSkills = vi.fn();
const mockResetMessages = vi.fn();
const mockClearComposerInput = vi.fn();
const mockSwitchView = vi.fn();
let mockEdition: "oss" | "enterprise" = "enterprise";

const mockUser = {
	publicId: "user-1",
	name: "测试用户",
	email: "test@example.com",
	uin: 1,
	currentOrg: { id: 1, uin: 10001, publicId: "org-1", code: "org-1", name: "个人组织" },
	organizations: [
		{ id: 1, uin: 10001, publicId: "org-1", code: "org-1", name: "个人组织" },
		{ id: 2, uin: 10002, publicId: "org-2", code: "org-2", name: "AI冲锋队" },
	],
};

vi.mock("@leros/store", () => ({
	normalizeFilePublicId: () => "",
	useAuthStore: (selector: (state: Record<string, unknown>) => unknown) =>
		selector({
			authUser: mockUser,
			refreshAuthSession: mockRefreshAuthSession,
			switchOrganization: mockSwitchOrganization,
			createOrganization: mockCreateOrganization,
		}),
	useLayoutStore: (selector: (state: Record<string, unknown>) => unknown) =>
		selector({
			fetchProjects: mockFetchProjects,
			resetAuthScopedData: mockResetLayout,
			switchView: mockSwitchView,
		}),
	useDAStore: (selector: (state: Record<string, unknown>) => unknown) =>
		selector({
			fetchAssistants: mockFetchAssistants,
			resetAuthScopedData: mockResetAssistants,
		}),
	useSkillStore: (selector: (state: Record<string, unknown>) => unknown) =>
		selector({
			fetchInstalledSkills: mockFetchInstalledSkills,
			resetAuthScopedData: mockResetSkills,
		}),
	useGlobalConfigStore: (selector: (state: Record<string, unknown>) => unknown) =>
		selector({ edition: mockEdition }),
	useChatStore: (selector: (state: Record<string, unknown>) => unknown) =>
		selector({
			clearComposerInput: mockClearComposerInput,
			resetLocalMessages: mockResetMessages,
		}),
}));

vi.mock("../avatar/ProtectedImage", () => ({
	ProtectedImage: ({ fallback }: { fallback: ReactNode }) => <>{fallback}</>,
}));

vi.mock("sonner", () => ({
	toast: {
		success: vi.fn(),
		error: vi.fn(),
	},
}));

describe("OrganizationSwitchPanel", () => {
	afterEach(() => {
		cleanup();
	});

	beforeEach(() => {
		vi.clearAllMocks();
		mockEdition = "enterprise";
		mockRefreshAuthSession.mockResolvedValue(true);
		mockSwitchOrganization.mockResolvedValue({});
		mockCreateOrganization.mockResolvedValue({});
		const pendingRequest = new Promise<never>(() => undefined);
		mockFetchProjects.mockReturnValue(pendingRequest);
		mockFetchAssistants.mockReturnValue(pendingRequest);
		mockFetchInstalledSkills.mockReturnValue(pendingRequest);
	});

	it("切换成功后立即进入新建任务页，不等待组织数据预加载", async () => {
		const goToRoute = vi.fn();
		const onDone = vi.fn();
		const { rerender } = render(
			<OrganizationSwitchPanel
				navigation={{ currentPath: "/projects", goToRoute } as never}
				onDone={onDone}
				active
			/>,
		);

		fireEvent.click(screen.getByRole("button", { name: /AI冲锋队/ }));

		await waitFor(() => {
			expect(mockSwitchOrganization).toHaveBeenCalledWith(10002);
			expect(goToRoute).toHaveBeenCalledWith("workbench");
		});
		// 中文注释：新路由尚未到达时保留切换层，避免短暂露出旧组织页面。
		expect(onDone).not.toHaveBeenCalled();
		rerender(
			<OrganizationSwitchPanel
				navigation={{ currentPath: "/workbench", goToRoute } as never}
				onDone={onDone}
				active
			/>,
		);
		await waitFor(() => expect(onDone).toHaveBeenCalledTimes(1));
		expect(mockFetchProjects).toHaveBeenCalledTimes(1);
		expect(mockFetchAssistants).toHaveBeenCalledTimes(1);
		expect(mockFetchInstalledSkills).toHaveBeenCalledTimes(1);
	});

	it("未上传图标的组织使用固定默认头像", () => {
		render(<OrganizationSwitchPanel active />);

		expect(screen.getByAltText("个人组织")).toHaveAttribute(
			"src",
			expect.stringContaining("organization-default-avatar.png"),
		);
	});

	it("创建组织时提交用户填写的昵称", async () => {
		render(<OrganizationSwitchPanel active />);

		fireEvent.click(screen.getByRole("button", { name: /创建新组织/ }));
		fireEvent.change(screen.getByLabelText("组织名称"), { target: { value: "新组织" } });
		fireEvent.change(screen.getByLabelText("用户昵称"), { target: { value: "新用户" } });
		fireEvent.click(screen.getByRole("button", { name: /创建并切换/ }));

		await waitFor(() => expect(mockCreateOrganization).toHaveBeenCalledWith("新组织", "新用户"));
	});

	it("OSS 已有组织时不显示创建组织按钮", () => {
		mockEdition = "oss";
		render(<OrganizationSwitchPanel active />);

		expect(screen.queryByRole("button", { name: /创建新组织/ })).not.toBeInTheDocument();
	});

	it("OSS 首次登录无组织时仍显示创建表单", () => {
		mockEdition = "oss";
		render(
			<OrganizationSwitchPanel
				active
				initialMode="create"
				pendingLogin={{
					organizations: [],
					onChoose: vi.fn(),
					onCreate: vi.fn(),
				}}
			/>,
		);

		expect(screen.getByLabelText("组织名称")).toBeInTheDocument();
	});
});
