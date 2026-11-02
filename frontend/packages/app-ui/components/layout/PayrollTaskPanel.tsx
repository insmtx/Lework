"use client";

import { useChatStore, useLayoutStore } from "@lework/store";
import { Button } from "@lework/ui/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@lework/ui/components/ui/dialog";
import { useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { filePreviewActions } from "./file-preview-store";
import { PayrollFileSlots } from "./PayrollFileSlots";
import { PayrollHistoricalSalaryTable } from "./PayrollHistoricalSalaryTable";
import { PayrollProjectFilePicker } from "./PayrollProjectFilePicker";
import { PayrollStoredPeopleTable } from "./PayrollStoredPeopleTable";
import {
	canStartPayrollAccounting,
	type PayrollSelectedFile,
	type PayrollSourceKind,
	payrollStarterPrompt,
} from "./payroll-accounting-mock";
import {
	appendPayrollLocalFiles,
	revokePayrollPreviewUrl,
	uploadPayrollAttachments,
} from "./payroll-file-upload";

export function PayrollTaskPanel({
	open,
	onOpenChange,
	projectId,
	taskId,
	sessionId,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	projectId: string;
	taskId: string;
	sessionId: string;
}) {
	const { projects } = useLayoutStore((s) => s);
	const sendTaskRoomMessage = useChatStore((s) => s.sendTaskRoomMessage);
	const [files, setFiles] = useState<PayrollSelectedFile[]>([]);
	const [submitting, setSubmitting] = useState(false);
	const [rosterRefreshToken, setRosterRefreshToken] = useState(0);
	const [salaryRefreshToken, setSalaryRefreshToken] = useState(0);
	const [filePickerRole, setFilePickerRole] = useState<PayrollSourceKind | null>(null);
	const filesRef = useRef(files);
	filesRef.current = files;

	const projectOptions = useMemo(
		() => projects.map((project) => ({ id: project.id, name: project.name, tasks: project.tasks })),
		[projects],
	);

	useEffect(() => {
		return () => {
			for (const file of filesRef.current) revokePayrollPreviewUrl(file.previewUrl);
		};
	}, []);

	const startTask = async () => {
		if (!canStartPayrollAccounting(files) || submitting) return;
		setSubmitting(true);
		try {
			const attendanceFiles = files.filter((file) => file.role === "attendance");
			const attachments = await uploadPayrollAttachments(attendanceFiles, projectId);
			const result = await sendTaskRoomMessage(
				payrollStarterPrompt(files),
				{ projectId, taskId, sessionId, scene: "salary_accounting" },
				attachments,
			);
			if (!result) throw new Error("提交失败");
			setFiles((current) => {
				for (const file of current) revokePayrollPreviewUrl(file.previewUrl);
				return [];
			});
			onOpenChange(false);
		} catch {
			toast.error("暂时无法启动工资核算任务，请稍后重试。");
		} finally {
			setSubmitting(false);
		}
	};

	const openProjectFilePicker = (role: PayrollSourceKind) => {
		if (!projects.length) {
			toast.info("当前账号暂无项目，请先创建项目");
			return;
		}
		setFilePickerRole(role);
	};

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="flex h-[min(92dvh,64rem)] max-h-[calc(100dvh-2rem)] w-[min(98vw,88rem)] max-w-none flex-col overflow-hidden p-0">
				<DialogHeader className="shrink-0 px-6 pt-6">
					<DialogTitle>人员工资基准</DialogTitle>
					<DialogDescription>
						人员底表和历史工资直接维护 Payroll 数据；这里只需上传当月考勤后继续核算。
					</DialogDescription>
				</DialogHeader>
				<div className="flex min-h-0 flex-1 flex-col gap-4 overflow-auto px-6 py-4">
					<PayrollFileSlots
						key="payroll-attendance"
						files={files}
						roles={["attendance"]}
						onAddFiles={(selected, role) =>
							setFiles((current) => appendPayrollLocalFiles(current, selected, role))
						}
						onRemove={(id) =>
							setFiles((current) => {
								const target = current.find((file) => file.id === id);
								revokePayrollPreviewUrl(target?.previewUrl);
								return current.filter((file) => file.id !== id);
							})
						}
						onPreview={(file) =>
							filePreviewActions.open({
								name: file.name,
								title: file.name,
								mimeType: file.mimeType,
								url: file.previewUrl,
								publicId: file.publicId,
								storageUri: file.storageUri,
								projectId: file.projectId,
								projectPath: file.projectPath,
							})
						}
						onPickProject={openProjectFilePicker}
					/>
					<PayrollStoredPeopleTable
						key="payroll-roster"
						refreshToken={rosterRefreshToken}
						onPickProject={() => openProjectFilePicker("roster")}
					/>
					<PayrollHistoricalSalaryTable
						key="payroll-historical-salary"
						refreshToken={salaryRefreshToken}
						onPickProject={() => openProjectFilePicker("historical_payroll")}
					/>
				</div>
				<DialogFooter className="shrink-0 border-t px-6 py-3">
					<Button
						type="button"
						disabled={submitting || !canStartPayrollAccounting(files)}
						onClick={() => void startTask()}
						className="bg-[var(--lework-primary)] text-white hover:bg-[var(--lework-primary)]/90"
					>
						{submitting ? "提交中..." : "开始核算"}
					</Button>
				</DialogFooter>
			</DialogContent>
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
					if (role === "roster") setRosterRefreshToken((value) => value + 1);
					if (role === "historical_payroll") setSalaryRefreshToken((value) => value + 1);
				}}
			/>
		</Dialog>
	);
}
