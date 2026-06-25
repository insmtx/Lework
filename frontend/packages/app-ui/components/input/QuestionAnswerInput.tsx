"use client";

import type { QuestionRequest } from "@leros/store/types/chat";
import { Badge } from "@leros/ui/components/ui/badge";
import { Button } from "@leros/ui/components/ui/button";
import { Textarea } from "@leros/ui/components/ui/textarea";
import { cn } from "@leros/ui/lib/utils";
import {
	AlertCircle,
	ChevronLeft,
	ChevronRight,
	CornerDownLeft,
	Info,
	LoaderCircle,
	Pencil,
} from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { getProjectChatLayoutClasses } from "../layout/project-chat-layout";

function QuestionStatusBadge({ question }: { question: QuestionRequest }) {
	switch (question.status) {
		case "submitting":
			return (
				<Badge className="bg-slate-100 text-slate-600">
					<LoaderCircle className="size-3 animate-spin" />
					提交中
				</Badge>
			);
		case "error":
			return <Badge variant="destructive">提交失败</Badge>;
		case "answered":
			return <Badge className="bg-green-100 text-green-700">已回答</Badge>;
		default:
			return <Badge className="bg-slate-100 text-slate-600">等待回答</Badge>;
	}
}

export function QuestionAnswerInput({
	question,
	messageId,
	variant,
	projectLayout,
	onAnswer,
}: {
	question: QuestionRequest;
	messageId: string;
	variant: "default" | "project";
	projectLayout?: ReturnType<typeof getProjectChatLayoutClasses>;
	onAnswer: (messageId: string, requestId: string, answers: string[][]) => void | Promise<void>;
}) {
	const [answers, setAnswers] = useState<string[][]>(() =>
		question.questions.map((item) => {
			const firstOption = item.options[0];
			return firstOption ? [firstOption.label] : [];
		}),
	);
	const [activeQuestionIndex, setActiveQuestionIndex] = useState(0);
	const [focusedOptionIndex, setFocusedOptionIndex] = useState(0);
	const isSubmitting = question.status === "submitting";
	const isProjectVariant = variant === "project";
	const layout = projectLayout ?? getProjectChatLayoutClasses("sidebar-expanded");

	const allAnswered = answers.every((value) => value.length > 0);
	const currentQuestion = question.questions[activeQuestionIndex] ?? {
		question: "",
		options: [],
		multiple: false,
		custom: false,
	};
	const currentAnswer = answers[activeQuestionIndex] ?? [];
	const currentAnswered = currentAnswer.length > 0;
	const hasMultipleQuestions = question.questions.length > 1;
	const isLastQuestion = activeQuestionIndex >= question.questions.length - 1;
	const customAnswerValue = currentQuestion.options.some(
		(option) => option.label === currentAnswer[0],
	)
		? ""
		: (currentAnswer[0] ?? "");

	const handleSubmit = useCallback(() => {
		if (!allAnswered || isSubmitting) return;
		onAnswer(messageId, question.requestId, answers);
	}, [allAnswered, isSubmitting, onAnswer, messageId, question.requestId, answers]);

	const handleCancel = useCallback(() => {
		if (isSubmitting) return;
		onAnswer(messageId, question.requestId, []);
	}, [isSubmitting, messageId, onAnswer, question.requestId]);

	const handleRadioChange = useCallback((questionIndex: number, value: string) => {
		setAnswers((prev) => {
			const next = prev.map((row) => [...row]);
			next[questionIndex] = [value];
			return next;
		});
	}, []);

	const handleNavigate = useCallback(
		(direction: -1 | 1) => {
			setActiveQuestionIndex((prev) => {
				const next = prev + direction;
				if (next < 0 || next >= question.questions.length) return prev;
				return next;
			});
		},
		[question.questions.length],
	);

	const handleContinue = useCallback(() => {
		if (isSubmitting || !currentAnswered) return;
		if (hasMultipleQuestions && !isLastQuestion) {
			handleNavigate(1);
			return;
		}
		if (allAnswered) {
			handleSubmit();
			return;
		}
		const nextUnansweredIndex = answers.findIndex((answer, index) => {
			if (index <= activeQuestionIndex) return false;
			return answer.length === 0;
		});
		if (nextUnansweredIndex !== -1) {
			setActiveQuestionIndex(nextUnansweredIndex);
			return;
		}
		const firstUnansweredIndex = answers.findIndex((answer) => answer.length === 0);
		if (firstUnansweredIndex !== -1) {
			setActiveQuestionIndex(firstUnansweredIndex);
			return;
		}
		handleNavigate(1);
	}, [
		activeQuestionIndex,
		allAnswered,
		answers,
		currentAnswered,
		handleNavigate,
		handleSubmit,
		hasMultipleQuestions,
		isLastQuestion,
		isSubmitting,
	]);

	const handleCheckboxChange = useCallback(
		(questionIndex: number, optionLabel: string, checked: boolean) => {
			setAnswers((prev) => {
				const next = prev.map((row) => [...row]);
				const current = next[questionIndex] ?? [];
				if (checked) {
					next[questionIndex] = [...current, optionLabel];
				} else {
					next[questionIndex] = current.filter((label) => label !== optionLabel);
				}
				return next;
			});
		},
		[],
	);

	const handleCustomChange = useCallback((questionIndex: number, value: string) => {
		setAnswers((prev) => {
			const next = prev.map((row) => [...row]);
			next[questionIndex] = value ? [value] : [];
			return next;
		});
	}, []);

	useEffect(() => {
		const selectedIndex = currentQuestion.options.findIndex((option) =>
			currentAnswer.includes(option.label),
		);
		setFocusedOptionIndex(selectedIndex >= 0 ? selectedIndex : 0);
	}, [activeQuestionIndex, currentAnswer, currentQuestion.options]);

	useEffect(() => {
		if (isSubmitting) return;

		const handleKeyDown = (event: KeyboardEvent) => {
			if (event.defaultPrevented || event.metaKey || event.ctrlKey || event.altKey) return;
			const activeEl = document.activeElement;
			if (activeEl?.tagName === "TEXTAREA") return;

			if (event.key === "Escape") {
				event.preventDefault();
				handleCancel();
				return;
			}

			if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
				if (!hasMultipleQuestions) return;
				event.preventDefault();
				handleNavigate(event.key === "ArrowLeft" ? -1 : 1);
				return;
			}

			const optionCount = currentQuestion.options.length;
			if ((event.key === "ArrowUp" || event.key === "ArrowDown") && optionCount > 0) {
				event.preventDefault();
				const direction = event.key === "ArrowUp" ? -1 : 1;
				const selectedIndex = currentQuestion.options.findIndex((option) =>
					currentAnswer.includes(option.label),
				);
				const baseIndex = selectedIndex >= 0 ? selectedIndex : focusedOptionIndex;
				const nextIndex = (baseIndex + direction + optionCount) % optionCount;
				const nextOption = currentQuestion.options[nextIndex];
				setFocusedOptionIndex(nextIndex);
				if (nextOption) {
					handleRadioChange(activeQuestionIndex, nextOption.label);
				}
				return;
			}

			if (event.key === " " && currentQuestion.multiple && optionCount > 0) {
				event.preventDefault();
				const option = currentQuestion.options[focusedOptionIndex];
				if (option) {
					handleCheckboxChange(
						activeQuestionIndex,
						option.label,
						!currentAnswer.includes(option.label),
					);
				}
				return;
			}

			if (event.key === "Enter" && !event.shiftKey) {
				event.preventDefault();
				handleContinue();
			}
		};

		window.addEventListener("keydown", handleKeyDown);
		return () => window.removeEventListener("keydown", handleKeyDown);
	}, [
		activeQuestionIndex,
		currentAnswer,
		currentQuestion.multiple,
		currentQuestion.options,
		focusedOptionIndex,
		handleCheckboxChange,
		handleCancel,
		handleContinue,
		handleNavigate,
		handleRadioChange,
		hasMultipleQuestions,
		isSubmitting,
	]);

	return (
		<div
			data-slot="question-answer-input"
			className={cn(
				"bg-transparent px-5 pb-5 sm:px-6 lg:px-8",
				isProjectVariant && cn("bg-white pb-8", layout.shell),
			)}
		>
			<div className={cn("mx-auto w-full max-w-[1040px]", isProjectVariant && layout.inner)}>
				<div className="overflow-hidden rounded-xl border border-slate-200 bg-white text-slate-900 shadow-[0_8px_22px_rgba(15,23,42,0.06)]">
					<div className="px-3.5 pb-2 pt-2.5 sm:px-4">
						<div className="mb-2 flex items-center justify-between gap-3">
							<div className="flex min-w-0 items-center gap-2">
								<div className="truncate text-[15px] font-semibold leading-5 text-slate-950">
									{currentQuestion.question}
								</div>
								<div className="shrink-0 scale-90">
									<QuestionStatusBadge question={question} />
								</div>
							</div>
							{hasMultipleQuestions && (
								<div className="flex shrink-0 items-center gap-1.5 text-xs text-slate-500">
									<Button
										type="button"
										variant="ghost"
										size="icon-xs"
										className="size-6 text-slate-400 hover:text-slate-700"
										onClick={() => handleNavigate(-1)}
										disabled={activeQuestionIndex === 0 || isSubmitting}
										aria-label="上一个问题"
									>
										<ChevronLeft className="size-3.5" />
									</Button>
									<span className="tabular-nums">
										{activeQuestionIndex + 1} of {question.questions.length}
									</span>
									<Button
										type="button"
										variant="ghost"
										size="icon-xs"
										className="size-6 text-slate-400 hover:text-slate-700"
										onClick={() => handleNavigate(1)}
										disabled={activeQuestionIndex === question.questions.length - 1 || isSubmitting}
										aria-label="下一个问题"
									>
										<ChevronRight className="size-3.5" />
									</Button>
								</div>
							)}
						</div>

						<div className="space-y-0.5">
							{currentQuestion.options.map((option, optionIndex) => {
								const selected = currentQuestion.multiple
									? currentAnswer.includes(option.label)
									: currentAnswer[0] === option.label;
								const focused = focusedOptionIndex === optionIndex;
								return (
									<button
										key={option.label}
										type="button"
										disabled={isSubmitting}
										onClick={() => {
											setFocusedOptionIndex(optionIndex);
											if (currentQuestion.multiple) {
												handleCheckboxChange(activeQuestionIndex, option.label, !selected);
												return;
											}
											handleRadioChange(activeQuestionIndex, option.label);
										}}
										className={cn(
											"flex min-h-7 w-full items-center gap-2.5 rounded-lg px-2 py-1 text-left transition-colors",
											"hover:bg-slate-100 disabled:cursor-not-allowed disabled:opacity-70",
											selected ? "bg-slate-100" : "bg-transparent",
											focused && !selected && "bg-slate-50",
										)}
									>
										<span
											className={cn(
												"flex size-5 shrink-0 items-center justify-center rounded-full border text-[11px] font-medium",
												selected
													? "border-slate-900 bg-slate-900 text-white"
													: "border-slate-200 bg-slate-50 text-slate-400",
											)}
										>
											{optionIndex + 1}
										</span>
										<span className="min-w-0 flex-1">
											<span className="block truncate text-[13px] font-normal leading-4 text-slate-950">
												{option.label}
											</span>
										</span>
										{option.description && (
											<span className="group/description relative shrink-0">
												<Info className="size-3.5 text-slate-400" aria-hidden="true" />
												<span className="pointer-events-none absolute bottom-full right-0 z-20 mb-1 hidden min-w-max max-w-56 whitespace-nowrap rounded-md bg-slate-950 px-2 py-1 text-xs font-normal leading-4 text-white shadow-lg group-hover/description:block">
													{option.description}
												</span>
											</span>
										)}
									</button>
								);
							})}

							{currentQuestion.custom && (
								<div className="flex items-start gap-2.5 rounded-lg px-2 py-1">
									<span className="flex size-5 shrink-0 items-center justify-center rounded-full border border-slate-200 bg-slate-50 text-slate-400">
										<Pencil className="size-3" />
									</span>
									<Textarea
										placeholder="否，请告知 Leros 如何调整"
										value={customAnswerValue}
										onChange={(e) => handleCustomChange(activeQuestionIndex, e.target.value)}
										disabled={isSubmitting}
										className="min-h-7 resize-none border-0 bg-transparent p-0 text-[13px] font-normal text-slate-700 shadow-none placeholder:text-slate-400 focus-visible:ring-0"
									/>
								</div>
							)}
						</div>

						{question.error && (
							<div className="mt-3 flex items-center gap-1.5 text-xs text-red-600">
								<AlertCircle className="size-3.5" />
								<span>{question.error}</span>
							</div>
						)}
					</div>

					<div className="flex items-center justify-end gap-2 border-t border-slate-100 bg-slate-50/70 px-3.5 py-1.5">
						<Button
							type="button"
							variant="ghost"
							size="sm"
							onClick={handleCancel}
							disabled={isSubmitting}
							className="h-7 rounded-full px-2 text-xs text-slate-500 hover:bg-slate-100 hover:text-slate-950"
						>
							取消
							<span className="rounded-md bg-slate-200/80 px-1 py-0.5 text-[11px] text-slate-700">
								Esc
							</span>
						</Button>
						<Button
							type="button"
							size="sm"
							onClick={handleContinue}
							disabled={!currentAnswered || isSubmitting}
							className="h-7 rounded-full bg-slate-950 px-2.5 text-xs text-white hover:bg-slate-800 disabled:bg-slate-300"
						>
							{isSubmitting && <LoaderCircle className="size-3 animate-spin" />}
							{hasMultipleQuestions && !isLastQuestion ? "继续" : "提交"}
							<span className="rounded-full bg-white/15 px-1 py-0.5 text-xs text-white/85">
								<CornerDownLeft className="size-2.5" />
							</span>
						</Button>
					</div>
				</div>
			</div>
		</div>
	);
}
