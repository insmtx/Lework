"use client";

import type { QuestionRequest } from "@leros/store/types/chat";
import { Badge } from "@leros/ui/components/ui/badge";
import { Button } from "@leros/ui/components/ui/button";
import { Input } from "@leros/ui/components/ui/input";
import { cn } from "@leros/ui/lib/utils";
import { AlertCircle, ChevronLeft, ChevronRight, Info, LoaderCircle } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
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

function isCustomOptionLabel(label: string): boolean {
	const normalized = label.trim().toLowerCase();
	return (
		/请说明|请填写|please\s+specify/.test(normalized) ||
		/^(其他|其它)(?:$|\s|[（(：:])/.test(normalized) ||
		/^other(?:$|\s|[（(：:])/.test(normalized)
	);
}

function isAnswerComplete(answer: string[] | undefined, customActive = false): boolean {
	if (!customActive) return (answer?.length ?? 0) > 0;
	return (answer?.[0]?.trim().length ?? 0) > 0;
}

function normalizeQuestionAnswers(answers: string[][], customActive: boolean[]): string[][] {
	return answers.map((answer, index) => {
		if (!customActive[index]) return answer;
		const customValue = answer[0]?.trim();
		return customValue ? [customValue] : [];
	});
}

/** Extract the custom (non-option) value from the answers array. */
function extractCustomValue(answers: string[], options: { label: string }[]): string {
	for (const ans of answers) {
		if (!options.some((o) => o.label === ans)) return ans;
	}
	return "";
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
	const [customActive, setCustomActive] = useState<boolean[]>(() =>
		question.questions.map((item) => item.custom && item.options.length === 0),
	);
	const [activeQuestionIndex, setActiveQuestionIndex] = useState(0);
	const [focusedOptionIndex, setFocusedOptionIndex] = useState(0);
	const customInputRef = useRef<HTMLInputElement>(null);
	const isSubmitting = question.status === "submitting";
	const isProjectVariant = variant === "project";
	const layout = projectLayout ?? getProjectChatLayoutClasses("sidebar-expanded");

	const allAnswered = question.questions.every((_, index) =>
		isAnswerComplete(answers[index], customActive[index]),
	);
	const currentQuestion = question.questions[activeQuestionIndex] ?? {
		question: "",
		options: [],
		multiple: false,
		custom: false,
	};
	const currentAnswer = answers[activeQuestionIndex] ?? [];
	const currentCustomActive = customActive[activeQuestionIndex] ?? false;
	const currentAnswered = isAnswerComplete(currentAnswer, currentCustomActive);
	const hasMultipleQuestions = question.questions.length > 1;
	const isLastQuestion = activeQuestionIndex >= question.questions.length - 1;
	const isMulti = currentQuestion.multiple;

	// Custom text from answers (non-option entry)
	const customAnswerValue = extractCustomValue(currentAnswer, currentQuestion.options);

	const handleSubmit = useCallback(() => {
		if (!allAnswered || isSubmitting) return;
		onAnswer(messageId, question.requestId, normalizeQuestionAnswers(answers, customActive));
	}, [allAnswered, isSubmitting, onAnswer, messageId, question.requestId, answers, customActive]);

	const handleCancel = useCallback(() => {
		if (isSubmitting) return;
		onAnswer(messageId, question.requestId, []);
	}, [isSubmitting, messageId, onAnswer, question.requestId]);

	const handleRadioChange = useCallback((questionIndex: number, value: string) => {
		setCustomActive((prev) => {
			const next = [...prev];
			next[questionIndex] = false;
			return next;
		});
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

	// In multi-select, typing = select, clearing = deselect. Simple.
	const handleCustomChange = useCallback(
		(questionIndex: number, value: string) => {
			const options = question.questions[questionIndex]?.options ?? [];
			const isMultiSelect = question.questions[questionIndex]?.multiple ?? false;

			setCustomActive((prev) => {
				const next = [...prev];
				next[questionIndex] = value.trim().length > 0;
				return next;
			});
			setAnswers((prev) => {
				const next = prev.map((row) => [...row]);
				if (isMultiSelect) {
					let customIdx = -1;
					const row = next[questionIndex] ?? [];
					for (let idx = 0; idx < row.length; idx++) {
						if (!options.some((o) => o.label === row[idx])) {
							customIdx = idx;
							break;
						}
					}
					if (customIdx >= 0) {
						if (value) {
							next[questionIndex][customIdx] = value;
						} else {
							next[questionIndex].splice(customIdx, 1);
						}
					} else if (value) {
						next[questionIndex] = [...(next[questionIndex] ?? []), value];
					}
				} else {
					next[questionIndex] = value ? [value] : [];
				}
				return next;
			});
		},
		[question.questions],
	);

	// Only for radio mode: activate custom on selection
	const handleCustomSelect = useCallback(
		(questionIndex: number) => {
			const isMultiSelect = question.questions[questionIndex]?.multiple ?? false;
			if (isMultiSelect) return; // multi-select: typing handles selection
			setCustomActive((prev) => {
				const next = [...prev];
				next[questionIndex] = true;
				return next;
			});
		},
		[question.questions],
	);

	useEffect(() => {
		const selectedIndex = currentQuestion.options.findIndex((option) =>
			currentCustomActive
				? isCustomOptionLabel(option.label)
				: currentAnswer.some((a) => a === option.label),
		);
		setFocusedOptionIndex(selectedIndex >= 0 ? selectedIndex : 0);
	}, [activeQuestionIndex, currentAnswer, currentCustomActive, currentQuestion.options]);

	useEffect(() => {
		if (!currentCustomActive || isSubmitting) return;
		const frame = requestAnimationFrame(() => {
			const el = document.querySelector("[data-custom-input]") as HTMLInputElement;
			el?.focus();
		});
		return () => cancelAnimationFrame(frame);
	}, [activeQuestionIndex, currentCustomActive, isSubmitting]);

	useEffect(() => {
		if (isSubmitting) return;

		const handleKeyDown = (event: KeyboardEvent) => {
			if (event.defaultPrevented || event.metaKey || event.ctrlKey || event.altKey) return;
			const activeEl = document.activeElement;
			const isInputFocused = activeEl?.tagName === "TEXTAREA" || activeEl?.tagName === "INPUT";

			// When input is focused, allow Escape and arrow keys to navigate away
			if (isInputFocused) {
				if (event.key === "Escape") {
					event.preventDefault();
					(activeEl as HTMLElement)?.blur();
					handleCancel();
					return;
				}
				if (event.key === "ArrowUp" || event.key === "ArrowDown") {
					event.preventDefault();
					(activeEl as HTMLElement)?.blur();
					// fall through to arrow navigation below
				} else {
					return;
				}
			}

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
				const nextIndex = (focusedOptionIndex + direction + optionCount) % optionCount;
				const nextOption = currentQuestion.options[nextIndex];
				setFocusedOptionIndex(nextIndex);
				// Multi-select: focus custom input when navigating to it
				if (currentQuestion.multiple && nextOption && isCustomOptionLabel(nextOption.label)) {
					requestAnimationFrame(() => {
						const el = document.querySelector("[data-custom-input]") as HTMLInputElement;
						el?.focus();
					});
				}
				// Radio: auto-select on arrow
				if (!currentQuestion.multiple && nextOption) {
					if (isCustomOptionLabel(nextOption.label)) {
						handleCustomSelect(activeQuestionIndex);
					} else {
						handleRadioChange(activeQuestionIndex, nextOption.label);
					}
				}
				return;
			}

			// Multi-select: Space/Enter toggle or focus custom input
			if (currentQuestion.multiple && optionCount > 0) {
				const toggleKeys = event.key === " " || (event.key === "Enter" && !event.shiftKey);
				if (toggleKeys) {
					event.preventDefault();
					const option = currentQuestion.options[focusedOptionIndex];
					if (!option) return;
					if (isCustomOptionLabel(option.label)) {
						// Focus the custom input for typing (typing = select)
						const el = document.querySelector("[data-custom-input]") as HTMLInputElement;
						el?.focus();
					} else {
						handleCheckboxChange(
							activeQuestionIndex,
							option.label,
							!currentAnswer.includes(option.label),
						);
					}
					return;
				}
			}

			if (event.key === "Enter" && !event.shiftKey && !currentQuestion.multiple) {
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
		handleCustomSelect,
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
					{/* Header */}
					<div className="flex items-start justify-between gap-3 px-3.5 pb-2 pt-2.5 sm:px-4">
						<div className="flex min-w-0 items-center gap-2">
							<h3 className="truncate text-[15px] font-semibold leading-5 text-slate-950">
								{currentQuestion.question}
							</h3>
							<div className="shrink-0">
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
									{activeQuestionIndex + 1}/{question.questions.length}
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

					{/* Options */}
					<div className="px-3.5 pb-2 sm:px-4">
						<div className="grid gap-0.5" role={isMulti ? "group" : "radiogroup"}>
							{currentQuestion.options.map((option, optionIndex) => {
								const isCustomOption = isCustomOptionLabel(option.label);
								const selected = isCustomOption
									? currentCustomActive
									: isMulti
										? currentAnswer.includes(option.label)
										: currentAnswer[0] === option.label;
								const focused = focusedOptionIndex === optionIndex;

								// Custom option: inline input, typing = select, clearing = deselect
								if (isCustomOption) {
									return (
										<div
											key={option.label}
											className={cn(
												"flex min-h-7 w-full items-center gap-2.5 rounded-lg border px-2 py-1 transition-all",
												"hover:bg-slate-50",
												currentCustomActive
													? "border-slate-200 bg-[#eef3fa] shadow-[inset_0_0_0_1px_rgba(255,255,255,0.48)]"
													: "border-transparent bg-transparent",
												focused && "ring-2 ring-slate-300",
												isSubmitting && "cursor-not-allowed opacity-70",
											)}
										>
											{/* Index circle: just shows the number, click to focus input */}
											<button
												type="button"
												tabIndex={-1}
												onKeyDown={(e) => {
													if (e.key === "Enter" || e.key === " ") {
														e.preventDefault();
														e.stopPropagation();
														(e.target as HTMLElement)?.click();
													}
												}}
												onClick={(e) => {
													e.stopPropagation();
													setFocusedOptionIndex(optionIndex);
													const el = document.querySelector(
														"[data-custom-input]",
													) as HTMLInputElement;
													el?.focus();
												}}
												className={cn(
													"flex size-5 shrink-0 cursor-pointer items-center justify-center rounded-full border-2 text-[11px] font-medium transition-all",
													currentCustomActive
														? "border-slate-900 bg-slate-900 text-white"
														: "border-[#dce5f1] bg-white text-[#8ca1bc]",
												)}
											>
												{optionIndex + 1}
											</button>
											<Input
												ref={customInputRef}
												data-custom-input={activeQuestionIndex}
												type="text"
												placeholder="输入自定义答案"
												value={customAnswerValue}
												onChange={(e) => handleCustomChange(activeQuestionIndex, e.target.value)}
												onFocus={() => setFocusedOptionIndex(optionIndex)}
												onClick={(e) => e.stopPropagation()}
												disabled={isSubmitting}
												className="!h-6 min-w-0 flex-1 border-0 bg-transparent px-0 py-0 text-[13px] font-normal leading-4 text-slate-950 shadow-none placeholder:text-[#98a8be] caret-slate-500 focus-visible:ring-0 focus-visible:ring-offset-0"
											/>
											{option.description && (
												<span className="group/description relative shrink-0">
													<Info className="size-3.5 text-slate-400" aria-hidden="true" />
													<span className="pointer-events-none absolute bottom-full right-0 z-20 mb-1 hidden min-w-max max-w-56 whitespace-nowrap rounded-md bg-slate-950 px-2 py-1 text-xs font-normal leading-4 text-white shadow-lg group-hover/description:block">
														{option.description}
													</span>
												</span>
											)}
										</div>
									);
								}

								// Regular option button
								return (
									// biome-ignore lint/a11y/useAriaPropsSupportedByRole: button with explicit radio/checkbox role supports aria-checked per ARIA spec
									<button
										key={option.label}
										type="button"
										role={isMulti ? "checkbox" : "radio"}
										aria-checked={selected}
										disabled={isSubmitting}
										onClick={() => {
											setFocusedOptionIndex(optionIndex);
											if (isMulti) {
												handleCheckboxChange(activeQuestionIndex, option.label, !selected);
												return;
											}
											handleRadioChange(activeQuestionIndex, option.label);
										}}
										className={cn(
											"flex min-h-7 w-full items-center gap-2.5 rounded-lg border px-2 py-1 text-left transition-all",
											"hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-70",
											"focus:outline-none",
											selected
												? "border-slate-200 bg-[#eef3fa] shadow-[inset_0_0_0_1px_rgba(255,255,255,0.48)]"
												: "border-transparent bg-transparent",
											focused && !selected && "ring-2 ring-slate-300",
										)}
									>
										<span
											className={cn(
												"flex size-5 shrink-0 items-center justify-center rounded-full border-2 text-[11px] font-medium transition-all",
												selected
													? "border-slate-900 bg-slate-900 text-white"
													: "border-[#dce5f1] bg-white text-[#8ca1bc]",
											)}
										>
											{optionIndex + 1}
										</span>
										<span className="min-w-0 flex-1 text-[13px] font-normal leading-4 text-slate-950">
											{option.label}
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

							{/* Fallback: custom enabled but no custom-labeled option in the list */}
							{currentQuestion.custom &&
								!currentQuestion.options.some((o) => isCustomOptionLabel(o.label)) && (
									<div
										className={cn(
											"flex min-h-7 w-full items-center gap-2.5 rounded-lg border px-2 py-1 transition-all",
											"hover:bg-slate-50",
											currentCustomActive
												? "border-slate-200 bg-[#eef3fa] shadow-[inset_0_0_0_1px_rgba(255,255,255,0.48)]"
												: "border-transparent bg-transparent",
											isSubmitting && "cursor-not-allowed opacity-70",
										)}
									>
										<span
											className={cn(
												"flex size-5 shrink-0 items-center justify-center rounded-full border-2 text-[11px] font-medium transition-all",
												currentCustomActive
													? "border-slate-900 bg-slate-900 text-white"
													: "border-[#dce5f1] bg-white text-[#8ca1bc]",
											)}
										>
											{currentQuestion.options.length + 1}
										</span>
										<Input
											ref={currentCustomActive ? customInputRef : undefined}
											type="text"
											placeholder="输入自定义答案"
											value={customAnswerValue}
											onChange={(e) => handleCustomChange(activeQuestionIndex, e.target.value)}
											onFocus={() => handleCustomSelect(activeQuestionIndex)}
											onClick={(e) => e.stopPropagation()}
											disabled={isSubmitting}
											className="!h-6 min-w-0 flex-1 border-0 bg-transparent px-0 py-0 text-[13px] font-normal leading-4 text-slate-950 shadow-none placeholder:text-[#98a8be] caret-slate-500 focus-visible:ring-0 focus-visible:ring-offset-0"
										/>
									</div>
								)}
						</div>

						{question.error && (
							<div className="mt-2 flex items-center gap-1.5 text-xs text-red-600">
								<AlertCircle className="size-3.5" />
								<span>{question.error}</span>
							</div>
						)}
					</div>

					{/* Footer */}
					<div className="flex items-center justify-between gap-2 border-t border-slate-200 bg-[#f8fafc] px-3.5 py-1.5">
						<button
							type="button"
							onClick={handleCancel}
							disabled={isSubmitting}
							className="inline-flex items-center gap-1 rounded-full px-1 py-1 text-xs font-medium text-[#60708b] transition-all hover:text-slate-800 disabled:cursor-not-allowed disabled:opacity-50"
						>
							取消
							<span className="inline-flex h-5 items-center rounded-md bg-[#e6edf6] px-1.5 text-[11px] font-medium text-[#344154]">
								Esc
							</span>
						</button>
						<button
							type="button"
							onClick={handleContinue}
							disabled={!currentAnswered || isSubmitting}
							className="inline-flex h-7 items-center justify-center gap-2 rounded-full bg-[#070b1a] px-2.5 text-xs font-medium text-white shadow-[0_4px_12px_rgba(7,11,26,0.12)] transition-all hover:bg-slate-900 disabled:cursor-not-allowed disabled:bg-slate-300 disabled:shadow-none active:translate-y-px"
						>
							{isSubmitting && <LoaderCircle className="size-3 animate-spin" />}
							{hasMultipleQuestions && !isLastQuestion ? "继续" : "提交"}
							<span className="flex size-4 items-center justify-center rounded-full bg-white/15 text-[10px] text-[#cbd5e1]">
								↵
							</span>
						</button>
					</div>
				</div>
			</div>
		</div>
	);
}
