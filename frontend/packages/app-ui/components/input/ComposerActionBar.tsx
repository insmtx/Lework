"use client";

import { type SkillInstalledItem, useSkillStore } from "@leros/store";
import {
	Command,
	CommandEmpty,
	CommandGroup,
	CommandInput,
	CommandItem,
	CommandList,
	CommandSeparator,
} from "@leros/ui/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@leros/ui/components/ui/popover";
import { Tooltip, TooltipContent, TooltipTrigger } from "@leros/ui/components/ui/tooltip";
import { cn } from "@leros/ui/lib/utils";
import { Bot, ClipboardPenLine, Plus, Sparkles, WandSparkles } from "lucide-react";
import { type ReactNode, type RefObject, useMemo, useState } from "react";
import { renderHighlightedText } from "../common/searchText";
import { AssistantAvatar } from "../digitalAssistant/AssistantAvatar";
import type {
	ComposerAssistantOption,
	ComposerSkillOption,
	StructuredComposerHandle,
} from "./StructuredComposer";

type ComposerActionBarProps = {
	inputValue: string;
	composerRef: RefObject<StructuredComposerHandle | null>;
	onUpload?: () => void;
	onBeforeAction?: () => boolean;
	children?: ReactNode;
	className?: string;
	assistantOptions?: ComposerAssistantOption[];
	projectSkillOptions?: ComposerSkillOption[];
	disableAssistantAndSkill?: boolean;
	assistantSelectionMode?: "single" | "multiple";
	executionMode?: "default" | "plan";
	setExecutionMode?: (mode: "default" | "plan") => void;
	isGenerating?: boolean;
};

type SkillOption = {
	code: string;
	label: string;
	description: string;
	keywords: string[];
};

function dedupeValues(values: string[]): string[] {
	return Array.from(new Set(values.filter(Boolean)));
}

function parseSelectedAssistantNames(value: string): string[] {
	return dedupeValues(
		Array.from(value.matchAll(/(?:^|\s)@([^\s@/]+)/g)).map((match) => match[1] ?? ""),
	);
}

function parseSelectedSlashLabels(value: string): string[] {
	return dedupeValues(
		Array.from(value.matchAll(/(?:^|\s)\/([^\s@/]+)/g)).map((match) => match[1] ?? ""),
	);
}

function installedSkillToOption(skill: SkillInstalledItem): SkillOption {
	const label = skill.display_name || skill.name;
	return {
		code: skill.name,
		label,
		description: skill.description || skill.category || "已安装技能",
		keywords: [
			label,
			skill.name,
			skill.description,
			skill.category,
			skill.source,
			skill.trust,
		].filter(Boolean),
	};
}

export function ComposerActionBar({
	inputValue,
	composerRef,
	onUpload,
	onBeforeAction,
	children,
	className,
	assistantOptions = [],
	projectSkillOptions,
	disableAssistantAndSkill = false,
	assistantSelectionMode = "multiple",
	executionMode,
	setExecutionMode,
	isGenerating,
}: ComposerActionBarProps) {
	const { installedSkills, installedSkillsLoaded } = useSkillStore((s) => s);
	const [assistantOpen, setAssistantOpen] = useState(false);
	const [assistantSearch, setAssistantSearch] = useState("");
	const [skillOpen, setSkillOpen] = useState(false);
	const [skillSearch, setSkillSearch] = useState("");

	const skillOptions = useMemo<SkillOption[]>(() => {
		if (projectSkillOptions) return projectSkillOptions;
		return installedSkills.map(installedSkillToOption);
	}, [installedSkills, projectSkillOptions]);
	const skillsLoading = skillOpen && !projectSkillOptions && !installedSkillsLoaded;

	const selectedAssistantNames = useMemo(
		() => parseSelectedAssistantNames(inputValue),
		[inputValue],
	);
	const selectedSlashLabels = useMemo(() => parseSelectedSlashLabels(inputValue), [inputValue]);
	const selectedSkillLabels = useMemo(
		() =>
			selectedSlashLabels.filter((label) =>
				skillOptions.some((option) => option.label === label || option.code === label),
			),
		[selectedSlashLabels, skillOptions],
	);
	const filteredAssistants = useMemo(() => {
		const query = assistantSearch.trim().toLowerCase();
		return assistantOptions.filter((assistant) => {
			if (
				assistantSelectionMode === "multiple" &&
				selectedAssistantNames.includes(assistant.name)
			) {
				return false;
			}
			if (!query) return true;
			return [assistant.name, assistant.code, assistant.description]
				.join(" ")
				.toLowerCase()
				.includes(query);
		});
	}, [assistantOptions, assistantSearch, assistantSelectionMode, selectedAssistantNames]);
	const filteredSkills = useMemo(() => {
		const query = skillSearch.trim().toLowerCase();
		return skillOptions.filter((skill) => {
			if (selectedSkillLabels.includes(skill.label)) return false;
			if (!query) return true;
			// 中文注释：技能搜索只按名称/code 匹配，描述和标签不参与搜索，避免弱相关结果排在前面。
			return [skill.label, skill.code].join(" ").toLowerCase().includes(query);
		});
	}, [selectedSkillLabels, skillOptions, skillSearch]);

	const allowAction = () => (onBeforeAction ? onBeforeAction() : true);
	const assistantSkillButtonClassName = cn(
		"inline-flex items-center gap-2 rounded-full px-2 py-1.5 text-sm text-slate-600 transition-colors hover:bg-slate-100 hover:text-slate-900",
		disableAssistantAndSkill &&
			"cursor-not-allowed opacity-45 hover:bg-transparent hover:text-slate-600",
	);

	return (
		<div className={cn("flex flex-wrap items-center gap-2", className)}>
			{setExecutionMode && (
				<Tooltip>
					<TooltipTrigger
						disabled={isGenerating}
						aria-label="Plan Mode"
						aria-pressed={executionMode === "plan"}
						onClick={() => {
							if (!allowAction()) return;
							setExecutionMode(executionMode === "plan" ? "default" : "plan");
						}}
						className={cn(
							"order-1 inline-flex h-8 w-8 items-center justify-center rounded-lg text-slate-600 transition-colors hover:bg-slate-100 hover:text-slate-900",
							executionMode === "plan" &&
								"bg-blue-50 text-blue-600 hover:bg-blue-100 hover:text-blue-700",
						)}
					>
						<ClipboardPenLine className="size-4" />
					</TooltipTrigger>
					<TooltipContent side="top">
						计划模式会先拆解任务并制定方案，提升复杂任务的执行质量
					</TooltipContent>
				</Tooltip>
			)}
			{onUpload && (
				<button
					type="button"
					onClick={() => {
						if (!allowAction()) return;
						onUpload();
					}}
					className="order-4 inline-flex items-center gap-2 rounded-full px-2 py-1.5 text-sm text-slate-600 transition-colors hover:bg-slate-100 hover:text-slate-900"
				>
					<Plus className="size-4" />
					<span>上传文件</span>
				</button>
			)}
			<Popover
				open={assistantOpen}
				onOpenChange={(open) => {
					setAssistantOpen(open);
					if (!open) setAssistantSearch("");
				}}
			>
				<PopoverTrigger
					type="button"
					disabled={disableAssistantAndSkill}
					onClick={(event) => {
						if (disableAssistantAndSkill) {
							event.preventDefault();
							return;
						}
						if (assistantOpen) return;
						if (event.defaultPrevented) return;
						if (!allowAction()) {
							event.preventDefault();
						}
					}}
					className={cn(assistantSkillButtonClassName, "order-3")}
				>
					<Bot className="size-4" />
					<span>召唤AI队友</span>
				</PopoverTrigger>
				{/* 固定在按钮上方，避免视口碰撞策略把选择弹窗动态翻到下方。 */}
				<PopoverContent
					align="start"
					side="top"
					sideOffset={10}
					collisionAvoidance={{ side: "none", align: "shift", fallbackAxisSide: "none" }}
					className="w-[340px] p-1.5"
				>
					<Command shouldFilter={false} className="rounded-xl! bg-transparent p-0">
						<div className="px-2 py-1 text-sm font-semibold text-slate-800">选择 AI 队友</div>
						<CommandInput
							value={assistantSearch}
							onValueChange={setAssistantSearch}
							placeholder="搜索 AI 队友"
							className="placeholder:text-slate-300"
						/>
						<CommandSeparator className="mx-1 my-2 bg-slate-200/80" />
						<CommandList className="max-h-64 px-1">
							<CommandEmpty className="py-6 text-slate-400">没有可继续添加的 AI 队友</CommandEmpty>
							<CommandGroup className="p-0">
								{filteredAssistants.map((assistant) => (
									<CommandItem
										key={assistant.code}
										value={assistant.name}
										onSelect={() => {
											composerRef.current?.insertAssistant(assistant.name);
											setAssistantOpen(false);
											setAssistantSearch("");
										}}
										className="rounded-lg px-2 py-1.5"
									>
										<AssistantAvatar name={assistant.name} src={assistant.avatarUrl} size="sm" />
										<div className="min-w-0 flex-1">
											<div className="truncate font-medium text-slate-700">
												{renderHighlightedText(assistant.name, assistantSearch)}
											</div>
											<div className="truncate text-xs text-slate-400">{assistant.description}</div>
										</div>
									</CommandItem>
								))}
							</CommandGroup>
						</CommandList>
					</Command>
				</PopoverContent>
			</Popover>
			<Popover
				open={skillOpen}
				onOpenChange={(open) => {
					setSkillOpen(open);
					if (!open) setSkillSearch("");
				}}
			>
				<PopoverTrigger
					type="button"
					disabled={disableAssistantAndSkill}
					onClick={(event) => {
						if (disableAssistantAndSkill) {
							event.preventDefault();
							return;
						}
						if (skillOpen) return;
						if (event.defaultPrevented) return;
						if (!allowAction()) {
							event.preventDefault();
						}
					}}
					className={cn(assistantSkillButtonClassName, "order-2")}
				>
					<WandSparkles className="size-4" />
					<span>添加技能</span>
				</PopoverTrigger>
				{/* 固定在按钮上方，避免视口碰撞策略把选择弹窗动态翻到下方。 */}
				<PopoverContent
					align="start"
					side="top"
					sideOffset={10}
					collisionAvoidance={{ side: "none", align: "shift", fallbackAxisSide: "none" }}
					className="w-[340px] p-1.5"
				>
					<Command shouldFilter={false} className="rounded-xl! bg-transparent p-0">
						<div className="px-2 py-1 text-sm font-semibold text-slate-800">选择技能</div>
						<CommandInput
							value={skillSearch}
							onValueChange={setSkillSearch}
							placeholder="搜索技能"
							className="placeholder:text-slate-300"
						/>
						<CommandSeparator className="mx-1 my-2 bg-slate-200/80" />
						<CommandList className="max-h-64 px-1">
							<CommandEmpty className="py-6 text-slate-400">没有可继续添加的技能</CommandEmpty>
							<CommandGroup className="p-0">
								{skillsLoading && (
									<div className="px-2 py-1.5 text-xs text-slate-400">技能加载中...</div>
								)}
								{filteredSkills.map((skill) => (
									<CommandItem
										key={skill.code}
										value={skill.label}
										onSelect={() => {
											composerRef.current?.insertSkill(skill.label);
										}}
										className="rounded-lg px-2 py-1.5"
									>
										<div className="flex size-7 shrink-0 items-center justify-center rounded-lg bg-violet-50 text-violet-600">
											<Sparkles className="size-3.5" />
										</div>
										<div className="min-w-0 flex-1">
											<div className="truncate font-medium">
												{renderHighlightedText(skill.label, skillSearch)}
											</div>
											<div className="truncate text-xs text-slate-400">{skill.description}</div>
										</div>
									</CommandItem>
								))}
							</CommandGroup>
						</CommandList>
					</Command>
				</PopoverContent>
			</Popover>
			{children}
		</div>
	);
}
