"use client";

import type { BackendTask, BackendTaskDeliverables, ProjectTask } from "@lework/store";
import { taskApi } from "@lework/store/api/taskApi";
import { Button } from "@lework/ui/components/ui/button";
import { CheckCircle2, FileText, LoaderCircle, XCircle } from "lucide-react";
import { useState } from "react";

const STATUS_LABELS: Record<string, string> = {
	created: "待开始",
	pending_assignment: "待指派",
	in_progress: "进行中",
	pending_approval: "待审批",
	pending_acceptance: "待验收",
	completed: "已完成",
	failed: "失败",
	cancelled: "已取消",
	paused: "已暂停",
};

type TaskAcceptancePanelProps = {
	task: ProjectTask;
	assigneeName?: string;
	deliverables?: BackendTaskDeliverables | null;
	loading?: boolean;
	onAccepted?: (task: BackendTask) => void;
	onRejected?: (task: BackendTask) => void;
};

export function TaskAcceptancePanel({
	task,
	assigneeName,
	deliverables,
	loading = false,
	onAccepted,
	onRejected,
}: TaskAcceptancePanelProps) {
	const [rejecting, setRejecting] = useState(false);
	const [rejectNote, setRejectNote] = useState("");
	const [submitting, setSubmitting] = useState<"accept" | "reject" | null>(null);
	const [error, setError] = useState("");

	const status = task.backendStatus ?? task.status;
	const pending = status === "pending_acceptance";
	const statusLabel = STATUS_LABELS[status] ?? status;
	const artifacts = deliverables?.artifacts ?? [];
	const files = deliverables?.files ?? [];
	const hasDeliverables = artifacts.length > 0 || files.length > 0;

	const accept = async () => {
		setSubmitting("accept");
		setError("");
		try {
			const response = await taskApi.accept({ task_id: task.id, note: "" });
			const updated = response.data.data;
			if (updated) onAccepted?.(updated);
		} catch (err) {
			setError(err instanceof Error ? err.message : "验收失败，请重试");
		} finally {
			setSubmitting(null);
		}
	};

	const reject = async () => {
		const note = rejectNote.trim();
		if (!note) return;
		setSubmitting("reject");
		setError("");
		try {
			const response = await taskApi.reject({ task_id: task.id, note });
			const updated = response.data.data;
			if (updated) onRejected?.(updated);
			setRejecting(false);
			setRejectNote("");
		} catch (err) {
			setError(err instanceof Error ? err.message : "驳回失败，请重试");
		} finally {
			setSubmitting(null);
		}
	};

	return (
		<section
			data-testid="task-acceptance-panel"
			className="mx-4 mt-3 rounded-xl border border-[var(--lework-control-border)] bg-[var(--lework-surface)] p-4 shadow-sm"
		>
			<div className="flex flex-wrap items-center gap-2">
				<span className="inline-flex items-center gap-1 rounded-full bg-[var(--lework-primary-softer)] px-2.5 py-1 text-xs font-medium text-[var(--lework-primary)]">
					{statusLabel}
				</span>
				{(assigneeName || task.assistantId) && (
					<span className="text-xs text-[var(--lework-text-muted)]">
						队友 {assigneeName || task.assistantId}
					</span>
				)}
			</div>

			<div className="mt-3 grid gap-3 sm:grid-cols-2">
				<TaskField label="预期产出" value={task.expectedOutput} />
				<TaskField label="验收标准" value={task.acceptanceCriteria} />
			</div>

			{(task.resultSummary || deliverables?.result_summary) && (
				<div className="mt-3 rounded-lg bg-[var(--lework-surface-soft)] p-3">
					<p className="text-xs font-semibold text-[var(--lework-text-muted)]">结果摘要</p>
					<p className="mt-1 whitespace-pre-wrap text-sm leading-relaxed text-[var(--lework-text)]">
						{task.resultSummary || deliverables?.result_summary}
					</p>
				</div>
			)}

			{task.acceptanceNote && (
				<div className="mt-3 rounded-lg border border-amber-200 bg-amber-50 p-3">
					<p className="text-xs font-semibold text-amber-700">验收意见</p>
					<p className="mt-1 whitespace-pre-wrap text-sm leading-relaxed text-amber-800">
						{task.acceptanceNote}
					</p>
				</div>
			)}

			{pending && (
				<div className="mt-3">
					<p className="text-xs font-semibold text-[var(--lework-text-muted)]">交付物</p>
					{loading ? (
						<div className="mt-2 flex items-center gap-2 text-sm text-[var(--lework-text-muted)]">
							<LoaderCircle className="size-4 animate-spin" />
							正在加载交付物…
						</div>
					) : !hasDeliverables ? (
						<p className="mt-2 text-sm text-[var(--lework-text-muted)]">
							队友还没有交付，去会话里催一下
						</p>
					) : (
						<ul className="mt-2 space-y-1.5">
							{artifacts.map((artifact) => (
								<li
									key={artifact.artifact_id ?? artifact.storage_uri ?? artifact.filename}
									className="flex items-start gap-2 text-sm text-[var(--lework-text)]"
								>
									<FileText className="mt-0.5 size-4 shrink-0 text-[var(--lework-text-muted)]" />
									<span>{artifact.title || artifact.filename || "未命名产物"}</span>
								</li>
							))}
							{files.map((file) => (
								<li
									key={file.file_public_id}
									className="flex items-start gap-2 text-sm text-[var(--lework-text)]"
								>
									<FileText className="mt-0.5 size-4 shrink-0 text-[var(--lework-text-muted)]" />
									<span>{file.relative_path || file.file_public_id}</span>
								</li>
							))}
						</ul>
					)}
				</div>
			)}

			{error && <p className="mt-3 text-sm text-red-600">{error}</p>}

			{pending && !rejecting && (
				<div className="mt-4 flex flex-wrap gap-2">
					<Button type="button" disabled={submitting !== null} onClick={() => void accept()}>
						<CheckCircle2 className="size-4" />
						验收通过
					</Button>
					<Button
						type="button"
						variant="outline"
						disabled={submitting !== null}
						onClick={() => setRejecting(true)}
					>
						<XCircle className="size-4" />
						驳回
					</Button>
				</div>
			)}

			{pending && rejecting && (
				<div className="mt-4 space-y-2">
					<textarea
						value={rejectNote}
						onChange={(event) => setRejectNote(event.target.value)}
						placeholder="请填写驳回意见"
						className="min-h-20 w-full rounded-md border border-[var(--lework-control-border)] bg-white px-3 py-2 text-sm text-[var(--lework-text)] outline-none focus:border-[var(--lework-primary)]"
					/>
					<div className="flex gap-2">
						<Button
							type="button"
							variant="outline"
							disabled={submitting !== null}
							onClick={() => {
								setRejecting(false);
								setRejectNote("");
							}}
						>
							取消
						</Button>
						<Button
							type="button"
							disabled={!rejectNote.trim() || submitting !== null}
							onClick={() => void reject()}
						>
							确认驳回
						</Button>
					</div>
				</div>
			)}
		</section>
	);
}

function TaskField({ label, value }: { label: string; value?: string }) {
	return (
		<div>
			<p className="text-xs font-semibold text-[var(--lework-text-muted)]">{label}</p>
			<p className="mt-1 whitespace-pre-wrap text-sm leading-relaxed text-[var(--lework-text)]">
				{value || "未填写"}
			</p>
		</div>
	);
}
