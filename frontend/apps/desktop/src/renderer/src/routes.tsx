import {
	type AppNavigation,
	AssistantListView,
	CenterCanvas,
	ProjectPage,
	Shell,
	SkillMarketView,
	TaskDetailPage,
	WorkbenchPanel,
} from "@leros/app-ui";
import {
	Navigate,
	Route,
	Routes,
	useLocation,
	useNavigate,
	useParams,
	useSearchParams,
} from "react-router-dom";

export function AppRoutes() {
	const navigation = useDesktopNavigation();

	return (
		<Shell navigation={navigation}>
			<Routes>
				<Route path="/" element={<Navigate to="/workbench" replace />} />
				<Route path="/workbench" element={<WorkbenchRoutePage />} />
				<Route path="/chat" element={<CenterCanvas />} />
				<Route path="/projects/:projectId" element={<ProjectRoutePage />} />
				<Route path="/projects/:projectId/tasks" element={<ProjectRoutePage tab="tasks" />} />
				<Route path="/projects/:projectId/files" element={<ProjectRoutePage tab="files" />} />
				<Route path="/projects/:projectId/tasks/:taskId" element={<TaskDetailRoutePage />} />
				<Route path="/assistants" element={<AssistantListView />} />
				<Route path="/tasks" element={<EmptyRoutePage />} />
				<Route path="/skills" element={<SkillMarketView />} />
				<Route path="/knowledge" element={<EmptyRoutePage />} />
				<Route path="/settings" element={<EmptyRoutePage />} />
				<Route path="*" element={<Navigate to="/workbench" replace />} />
			</Routes>
		</Shell>
	);
}

function useDesktopNavigation(): AppNavigation {
	const location = useLocation();
	const navigate = useNavigate();

	return {
		currentPath: location.pathname,
		goToRoute(route) {
			const routePath = {
				chat: "/chat",
				workbench: "/workbench",
				tasks: "/tasks",
				project: "/workbench",
				taskDetail: "/workbench",
				digitalAssistant: "/assistants",
				knowledge: "/knowledge",
				skills: "/skills",
				settings: "/settings",
			}[route];
			navigate(routePath);
		},
		goToProject(projectId) {
			navigate(`/projects/${projectId}`);
		},
		goToTaskDetail(projectId, taskId, sessionId) {
			const search = sessionId ? `?sessionId=${encodeURIComponent(sessionId)}` : "";
			navigate(`/projects/${projectId}/tasks/${taskId}${search}`);
		},
	};
}

function WorkbenchRoutePage() {
	const navigation = useDesktopNavigation();

	return <WorkbenchPanel navigation={navigation} />;
}

function ProjectRoutePage({ tab = "chat" }: { tab?: "chat" | "tasks" | "files" }) {
	const navigation = useDesktopNavigation();
	const navigate = useNavigate();
	const { projectId = "" } = useParams();

	return (
		<ProjectPage
			projectId={projectId}
			tab={tab}
			navigation={navigation}
			onTabChange={(nextTab) => {
				if (nextTab === "chat") {
					navigation.goToProject(projectId);
					return;
				}
				navigate(`/projects/${projectId}/${nextTab === "tasks" ? "tasks" : "files"}`);
			}}
		/>
	);
}

function TaskDetailRoutePage() {
	const navigation = useDesktopNavigation();
	const { projectId = "", taskId = "" } = useParams();
	const [searchParams] = useSearchParams();

	return (
		<TaskDetailPage
			projectId={projectId}
			taskId={taskId}
			sessionId={searchParams.get("sessionId")}
			navigation={navigation}
		/>
	);
}

function EmptyRoutePage() {
	return <div data-slot="empty-page" className="min-h-0 flex-1 bg-[#f7f8fd]" />;
}
