"use client";

import type { QuestionRequest } from "@leros/store/types/chat";
import { Badge } from "@leros/ui/components/ui/badge";
import { Button } from "@leros/ui/components/ui/button";
import { cn } from "@leros/ui/lib/utils";
import { AlertCircle, ClipboardList, LoaderCircle } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { MarkdownRenderer } from "../common/MarkdownRenderer";
import { getProjectChatLayoutClasses } from "../layout/project-chat-layout";

export function PlanConfirmationInput({
	question,
	messageId,
	variant,
	projectLayout,
	onAnswer,
	onExecute,
	onRevise,
}: {
	question: QuestionRequest;
	messageId: string;
	variant: "default" | "project";
	projectLayout?: ReturnType<typeof getProjectChatLayoutClasses>;
	onAnswer: (messageId: string, requestId: string, answers: string[][]) => void | Promise<void>;
	onExecute: () => void;
	onRevise: () => void;
}) {
	const [locked, setLocked] = useState(false);
	const submittedRef = useRef(false);
	const isProjectVariant = variant === "project";
	const layout = projectLayout ?? getProjectChatLayoutClasses("sidebar-expanded");
	const isSubmitting = question.status === "submitting" || locked;
	const canExecute = Boolean(question.plan?.content?.trim()) && !question.plan?.error;
	const prompt = question.questions[0]?.question;

	useEffect(() => {
		if (question.status === "error") {
			submittedRef.current = false;
			setLocked(false);
		}
	}, [question.status]);

	const submit = useCallback(
		(answer: "Yes" | "No") => {
			if (submittedRef.current || question.status === "submitting") return;
			submittedRef.current = true;
			setLocked(true);
			if (answer === "Yes") onExecute();
			if (answer === "No") onRevise();
			void onAnswer(messageId, question.requestId, [[answer]]);
		},
		[messageId, onAnswer, onExecute, onRevise, question.requestId, question.status],
	);

	return (
		<div
			data-slot="plan-confirmation-input"
			className={cn(
				"bg-transparent px-5 pb-5 sm:px-6 lg:px-8",
				isProjectVariant && cn("bg-white pb-8", layout.shell),
			)}
		>
			<div className={cn("mx-auto w-full max-w-[1040px]", isProjectVariant && layout.inner)}>
				<div className="overflow-hidden rounded-[18px] border border-slate-200 bg-white shadow-[0_12px_32px_rgba(15,23,42,0.08)]">
					<div className="flex items-center gap-2 border-b border-slate-100 px-5 py-4">
						<ClipboardList className="size-4 text-slate-600" />
						<span className="text-sm font-medium text-slate-900">计划确认</span>
						<Badge className="bg-slate-100 text-slate-600">
							{isSubmitting ? <LoaderCircle className="size-3 animate-spin" /> : null}
							{isSubmitting ? "提交中" : "等待确认"}
						</Badge>
					</div>
					<div className="max-h-[50vh] overflow-auto px-5 py-4">
						{prompt ? <p className="mb-4 text-sm text-slate-600">{prompt}</p> : null}
						{question.plan?.content ? (
							<MarkdownRenderer
								content={question.plan.content}
								className="prose prose-slate max-w-none text-sm"
							/>
						) : (
							<p className="text-sm text-slate-500">计划内容暂不可用。</p>
						)}
						{question.plan?.error || question.error ? (
							<div className="mt-3 flex items-start gap-2 text-sm text-red-600">
								<AlertCircle className="mt-0.5 size-4 shrink-0" />
								<span>{question.plan?.error || question.error}</span>
							</div>
						) : null}
					</div>
					<div className="flex justify-end gap-2 border-t border-slate-100 bg-slate-50/70 px-5 py-3">
						<Button
							type="button"
							variant="outline"
							size="sm"
							disabled={isSubmitting}
							onClick={() => submit("No")}
						>
							调整计划
						</Button>
						<Button
							type="button"
							size="sm"
							disabled={isSubmitting || !canExecute}
							onClick={() => submit("Yes")}
							className="bg-slate-950 text-white hover:bg-slate-800"
						>
							执行计划
						</Button>
					</div>
				</div>
			</div>
		</div>
	);
}
