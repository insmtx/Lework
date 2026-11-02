"use client";

import { useLayoutStore } from "@lework/store";
import { Button } from "@lework/ui/components/ui/button";
import { ArrowLeft, Calculator } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { useAuth } from "../auth";
import { filePreviewActions } from "./file-preview-store";
import type { AppNavigation } from "./LeftRail";
import { PayrollFileSlots } from "./PayrollFileSlots";
import { PayrollHistoricalSalaryTable } from "./PayrollHistoricalSalaryTable";
import { PayrollProjectFilePicker } from "./PayrollProjectFilePicker";
import { PayrollStoredPeopleTable } from "./PayrollStoredPeopleTable";
import { ProjectTaskPickerField } from "./ProjectTaskPicker";
import {
	canStartPayrollAccounting,
	canStartPayrollTask,
	type PayrollSelectedFile,
	type PayrollSourceKind,
	payrollStarterPrompt,
	WORKBENCH_PAYROLL_PATH,
} from "./payroll-accounting-mock";
import {
	appendPayrollLocalFiles,
	revokePayrollPreviewUrl,
	uploadPayrollAttachments,
} from "./payroll-file-upload";
import {
	listPayrollPeople,
	listPayrollSalary,
	markPayrollTaskSession,
} from "./payroll-service-client";

export function PayrollAccountingPage({ navigation }: { navigation?: AppNavigation }) {
	const { projects, fetchProjects, fetchTasks, sendWorkbenchMessage } = useLayoutStore((s) => s);
	const { isAuthenticated } = useAuth();
	const [projectId, setProjectId] = useState("");
	const [taskId, setTaskId] = useState("");
	const [files, setFiles] = useState<PayrollSelectedFile[]>([]);
	const [submitting, setSubmitting] = useState(false);
	const [hasBaseline, setHasBaseline] = useState(false);
	const [rosterInitialized, setRosterInitialized] = useState(false);
	const [hisSalaryInitialized, setHisSalaryInitialized] = useState(false);
	const [rosterRefreshToken, setRosterRefreshToken] = useState(0);
	const [salaryRefreshToken, setSalaryRefreshToken] = useState(0);
	const [filePickerRole, setFilePickerRole] = useState<PayrollSourceKind | null>(null);
	const filesRef = useRef(files);
	filesRef.current = files;

	useEffect(() => {
		return () => {
			for (const file of filesRef.current) {
				revokePayrollPreviewUrl(file.previewUrl);
			}
		};
	}, []);

	const projectOptions = useMemo(
		() => projects.map((project) => ({ id: project.id, name: project.name, tasks: project.tasks })),
		[projects],
	);

	useEffect(() => {
		if (!isAuthenticated) return;
		void fetchProjects();
	}, [fetchProjects, isAuthenticated]);

	useEffect(() => {
		void Promise.all([listPayrollPeople({ limit: 1 }), listPayrollSalary({ limit: 1 })])
			.then(([roster, salary]) => {
				setRosterInitialized(roster.initialized);
				setHisSalaryInitialized(salary.initialized);
				setHasBaseline(roster.initialized && salary.initialized);
			})
			.catch(() => {
				toast.error("暂时无法读取工资基础数据，请稍后重试。");
			});
	}, []);

	const refreshBaselineState = useCallback(async () => {
		const [roster, salary] = await Promise.all([
			listPayrollPeople({ limit: 1 }),
			listPayrollSalary({ limit: 1 }),
		]);
		setRosterInitialized(roster.initialized);
		setHisSalaryInitialized(salary.initialized);
		setHasBaseline(roster.initialized && salary.initialized);
	}, []);

	const handleRosterInitializedChange = useCallback(
		(initialized: boolean) => {
			setRosterInitialized(initialized);
			setHasBaseline(initialized && hisSalaryInitialized);
		},
		[hisSalaryInitialized],
	);

	const goBack = () => {
		if (navigation?.goToPath) {
			navigation.goToPath("/workbench");
			return;
		}
		navigation?.goToRoute("workbench");
	};

	const openProjectFilePicker = (role: PayrollSourceKind) => {
		if (!projects.length) {
			toast.info("当前账号暂无项目，请先创建项目");
			return;
		}
		setFilePickerRole(role);
	};

	const openPreview = (file: PayrollSelectedFile) => {
		filePreviewActions.open({
			name: file.name,
			title: file.name,
			mimeType: file.mimeType,
			url: file.previewUrl,
			publicId: file.publicId,
			storageUri: file.storageUri,
			projectId: file.projectId,
			projectPath: file.projectPath,
		});
	};

	const removeFile = (id: string) => {
		setFiles((current) => {
			const target = current.find((file) => file.id === id);
			revokePayrollPreviewUrl(target?.previewUrl);
			return current.filter((file) => file.id !== id);
		});
	};

	const goToCreatedTask = (result: {
		project_id?: string;
		task_id?: string;
		session_id?: string;
	}) => {
		if (!result.project_id || !result.task_id || !result.session_id) {
			throw new Error("创建工资核算任务失败");
		}
		markPayrollTaskSession(result.session_id);
		navigation?.goToTaskDetail(result.project_id, result.task_id, result.session_id);
	};

	const startAnalysis = async () => {
		if (!canStartPayrollTask(files) || submitting) return;
		setSubmitting(true);
		try {
			const computing = canStartPayrollAccounting(files);
			if (!computing) return;
			const attendanceFiles = files.filter((file) => file.role === "attendance");
			const attachments = await uploadPayrollAttachments(attendanceFiles, projectId);
			const result = await sendWorkbenchMessage(
				payrollStarterPrompt(files),
				projectId || undefined,
				"default",
				attachments,
				undefined,
				undefined,
				undefined,
				computing ? "salary_accounting" : undefined,
				undefined,
				taskId || null,
			);
			goToCreatedTask(result ?? {});
		} catch (error) {
			console.error("PayrollAccountingPage start analysis error:", error);
			toast.error("工资核算服务连接失败，请重试。");
		} finally {
			setSubmitting(false);
		}
	};

	return (
		<div
			data-slot="payroll-accounting-page"
			data-path={WORKBENCH_PAYROLL_PATH}
			className="flex h-full min-h-0 min-w-0 flex-1 flex-col overflow-hidden bg-[var(--lework-app-bg)]"
		>
			<header className="shrink-0 border-b border-[var(--lework-control-border)] px-4 py-4 sm:px-6 sm:py-5">
				<Button
					type="button"
					variant="ghost"
					size="sm"
					className="-ml-2 mb-2 text-[var(--lework-text-muted)]"
					onClick={goBack}
				>
					<ArrowLeft className="size-4" />
					返回工作台
				</Button>
				<div className="flex items-center justify-between gap-3">
					<div className="flex min-w-0 items-center gap-3">
						<div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-emerald-50 text-emerald-600">
							<Calculator className="size-5" />
						</div>
						<h1 className="truncate text-xl font-semibold text-[var(--lework-text-strong)]">
							考勤工资核算
						</h1>
					</div>
					<Button
						type="button"
						disabled={!isAuthenticated || submitting || !canStartPayrollTask(files)}
						onClick={() => void startAnalysis()}
						className="shrink-0 bg-[var(--lework-primary)] text-white hover:bg-[var(--lework-primary)]/90"
					>
						{submitting ? "启动中..." : "开始核算"}
					</Button>
				</div>
			</header>

			<main className="flex min-h-0 min-w-0 flex-1 flex-col gap-5 overflow-x-hidden overflow-y-auto px-4 py-5 sm:px-6 sm:py-6">
				<ProjectTaskPickerField
					projectId={projectId}
					taskId={taskId}
					allowNewProject
					allowSelectTask
					onLoadProjectTasks={fetchTasks}
					onSelect={(nextProjectId, nextTaskId) => {
						setProjectId(nextProjectId);
						setTaskId(nextTaskId);
						if (nextProjectId) fetchTasks(nextProjectId);
					}}
				/>

				<PayrollFileSlots
					key="payroll-attendance"
					files={files}
					roles={["attendance"]}
					onAddFiles={(selected, role) =>
						setFiles((current) => appendPayrollLocalFiles(current, selected, role))
					}
					onRemove={removeFile}
					onPreview={openPreview}
					onPickProject={openProjectFilePicker}
				/>

				<PayrollStoredPeopleTable
					key="payroll-roster"
					onInitializedChange={handleRosterInitializedChange}
					refreshToken={rosterRefreshToken}
					onPickProject={() => openProjectFilePicker("roster")}
				/>
				{hasBaseline ? null : (
					<p className="text-xs text-slate-400">
						底表：{rosterInitialized ? "已初始化" : "未初始化"}；历史工资：
						{hisSalaryInitialized ? "已初始化" : "未初始化"}
					</p>
				)}

				<PayrollHistoricalSalaryTable
					key="payroll-historical-salary"
					refreshToken={salaryRefreshToken}
					onInitializedChange={(initialized) => {
						setHisSalaryInitialized(initialized);
						setHasBaseline(rosterInitialized && initialized);
					}}
					onPickProject={() => openProjectFilePicker("historical_payroll")}
				/>
			</main>

			<PayrollProjectFilePicker
				role={filePickerRole}
				projects={projectOptions}
				files={files}
				onClose={() => setFilePickerRole(null)}
				onAttendanceSelected={(projectFiles) =>
					setFiles((current) => [
						...current.filter((file) => file.role !== "attendance"),
						...projectFiles,
					])
				}
				onImported={(role) => {
					void refreshBaselineState();
					if (role === "roster") setRosterRefreshToken((value) => value + 1);
					if (role === "historical_payroll") setSalaryRefreshToken((value) => value + 1);
				}}
			/>
		</div>
	);
}
