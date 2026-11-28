import type { AppNavigation } from "@lework/app-ui";
import {
	AutomationDetailRoute,
	AutomationRoute,
	ControlConsole,
	EmptyRoute,
	NewTaskRoute,
	NodesView,
	OrgAdminRoute,
	PayrollRoute,
	PendingView,
	ProjectFilesView,
	ProjectRoute,
	ProjectsHubRoute,
	SkillDetailRoute,
	SkillsRoute,
	WorkspaceConsole,
	WorkspacesView,
	TaskDetailRoute,
	WorkbenchRoute,
	consoleRoutes,
	consoleSessionPath,
	createConsoleNavigation,
	projectPath,
	workspaceFilesPath,
	workspacePath,
} from "@lework/control-ui";
import {
	Navigate,
	Route,
	Routes,
	useLocation,
	useNavigate,
	useParams,
	useSearchParams,
} from "react-router-dom";

import { ControlPlaneProvider } from "./components/ControlPlaneProvider";
import { DesktopSettingsPage } from "./components/DesktopSettingsPage";

export function AppRoutes() {
	const navigation = useDesktopNavigation();

	return (
		<Routes>
			<Route path="/" element={<Navigate to={consoleRoutes.chat} replace />} />

			<Route path={consoleRoutes.chat} element={<NewTaskRoute navigation={navigation} />} />
			<Route path={consoleRoutes.workbench} element={<WorkbenchRoute navigation={navigation} />} />
			<Route path={consoleRoutes.payroll} element={<PayrollRoute navigation={navigation} />} />
			<Route path={consoleRoutes.projects} element={<ProjectsHubRoute navigation={navigation} />} />

			<Route
				path={consoleRoutes.projects + "/:projectId"}
				element={<ProjectRouteElement navigation={navigation} />}
			/>
			<Route
				path={consoleRoutes.projects + "/:projectId/tasks"}
				element={<ProjectRouteElement tab="tasks" navigation={navigation} />}
			/>
			<Route
				path={consoleRoutes.projects + "/:projectId/files"}
				element={<ProjectRouteElement tab="files" navigation={navigation} />}
			/>
			<Route
				path={consoleRoutes.projects + "/:projectId/activity"}
				element={<ProjectRouteElement tab="activity" navigation={navigation} />}
			/>
			<Route
				path={consoleRoutes.projects + "/:projectId/tasks/:taskId/sessions/:sessionId"}
				element={<TaskDetailRouteElement navigation={navigation} />}
			/>

			<Route path="/org" element={<Navigate to={consoleRoutes.orgProfile} replace />} />
			<Route
				path={consoleRoutes.orgProfile}
				element={<OrgAdminRoute section="profile" navigation={navigation} />}
			/>
			<Route
				path={consoleRoutes.orgDepartments}
				element={<OrgAdminRoute section="departments" navigation={navigation} />}
			/>
			<Route
				path={consoleRoutes.orgAssistants}
				element={<OrgAdminRoute section="assistants" navigation={navigation} />}
			/>
			<Route
				path={consoleRoutes.orgModels}
				element={<OrgAdminRoute section="models" navigation={navigation} />}
			/>

			<Route path={consoleRoutes.tasks} element={<EmptyRoute />} />
			<Route path={consoleRoutes.skills} element={<SkillsRoute navigation={navigation} />} />
			<Route
				path={consoleRoutes.skills + "/:skillId"}
				element={<SkillDetailRouteElement navigation={navigation} />}
			/>
			<Route path={consoleRoutes.automation} element={<AutomationRoute navigation={navigation} />} />
			<Route
				path={consoleRoutes.automation + "/:publicId"}
				element={<AutomationDetailRouteElement navigation={navigation} />}
			/>

			{/* 桌面没有独立知识库页，保持原有「回落到工作台」的行为。 */}
			<Route path={consoleRoutes.knowledge} element={<Navigate to={consoleRoutes.workbench} replace />} />

			<Route path={consoleRoutes.workspaces} element={<WorkspacesRoute />} />
			<Route
				path={consoleRoutes.workspaces + "/:key/files"}
				element={<ProjectFilesRoute />}
			/>
			<Route
				path={consoleRoutes.workspaces + "/:key"}
				element={<WorkspaceConsoleRoute />}
			/>
			<Route
				path={consoleRoutes.workspacesConsole}
				element={
					<ControlPlaneProvider>
						<ConsoleRoute />
					</ControlPlaneProvider>
				}
			/>
			<Route
				path={consoleRoutes.nodes}
				element={
					<ControlPlaneProvider>
						<NodesView />
					</ControlPlaneProvider>
				}
			/>
			<Route path={consoleRoutes.pending} element={<PendingRoute />} />

			<Route path={consoleRoutes.settings} element={<DesktopSettingsPage />} />

			<Route path="*" element={<Navigate to={consoleRoutes.chat} replace />} />
		</Routes>
	);
}

function useDesktopNavigation(): AppNavigation {
	const location = useLocation();
	const navigate = useNavigate();

	return createConsoleNavigation({ pathname: location.pathname, push: navigate });
}

function ProjectRouteElement({
	tab = "chat",
	navigation,
}: {
	tab?: "chat" | "tasks" | "files" | "activity";
	navigation: AppNavigation;
}) {
	const { projectId = "" } = useParams();
	const navigate = useNavigate();

	return (
		<ProjectRoute
			projectId={projectId}
			tab={tab}
			navigation={navigation}
			onTabChange={(nextTab) => navigate(projectPath(projectId, nextTab))}
		/>
	);
}

function TaskDetailRouteElement({ navigation }: { navigation: AppNavigation }) {
	const { projectId = "", taskId = "", sessionId = "" } = useParams();

	return (
		<TaskDetailRoute
			projectId={projectId}
			taskId={taskId}
			sessionId={sessionId}
			navigation={navigation}
		/>
	);
}

function SkillDetailRouteElement({ navigation }: { navigation: AppNavigation }) {
	const { skillId = "" } = useParams();

	return <SkillDetailRoute skillId={skillId} navigation={navigation} />;
}

function AutomationDetailRouteElement({ navigation }: { navigation: AppNavigation }) {
	const { publicId = "" } = useParams();

	return <AutomationDetailRoute publicId={publicId} navigation={navigation} />;
}

function WorkspacesRoute() {
	const navigate = useNavigate();

	return (
		<ControlPlaneProvider>
			<WorkspacesView
				onOpenWorkspace={(key) => navigate(workspacePath(key))}
				onOpenNodes={() => navigate(consoleRoutes.nodes)}
				onBrowseWorkspace={(key) => navigate(workspaceFilesPath(key))}
			/>
		</ControlPlaneProvider>
	);
}

function ProjectFilesRoute() {
	const { key = "" } = useParams();
	const navigate = useNavigate();

	return (
		<ControlPlaneProvider>
			<ProjectFilesView projectKey={key} onBack={() => navigate(consoleRoutes.workspaces)} />
		</ControlPlaneProvider>
	);
}

function WorkspaceConsoleRoute() {
	const { key = "" } = useParams();
	const [search] = useSearchParams();
	const navigate = useNavigate();

	return (
		<ControlPlaneProvider>
			<WorkspaceConsole
				projectKey={key}
				initialSessionID={search.get("session")}
				onBack={() => navigate(consoleRoutes.workspaces)}
			/>
		</ControlPlaneProvider>
	);
}

function ConsoleRoute() {
	const [search] = useSearchParams();

	return (
		<ControlConsole
			initialNodeID={search.get("node")}
			initialSessionID={search.get("session")}
		/>
	);
}

function PendingRoute() {
	const navigate = useNavigate();

	return (
		<ControlPlaneProvider>
			<PendingView
				onOpenSession={(nodeID, sessionID) =>
					navigate(consoleSessionPath(nodeID, sessionID))
				}
			/>
		</ControlPlaneProvider>
	);
}
