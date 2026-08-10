"use client";

import type { PluginComposerOption } from "@leros/store";
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
import {
	Bot,
	Cable,
	ClipboardPenLine,
	FileText,
	Folder,
	Plus,
	Sparkles,
	WandSparkles,
} from "lucide-react";
import { type ReactNode, type RefObject, useMemo, useState } from "react";
import { MCPConnectorIcon } from "../common/MCPConnectorIcon";
import { renderHighlightedText } from "../common/searchText";
import { AssistantAvatar } from "../digitalAssistant/AssistantAvatar";
import type { ComposerAssistantOption, StructuredComposerHandle } from "./StructuredComposer";

type ComposerActionBarProps = {
	inputValue: string;
	composerRef: RefObject<StructuredComposerHandle | null>;
	onUpload?: () => void;
	onUploadFolder?: () => void;
	onBeforeAction?: () => boolean;
	children?: ReactNode;
	className?: string;
	assistantOptions?: ComposerAssistantOption[];
	skillOptions?: PluginComposerOption[];
	skillsLoading?: boolean;
	disableAssistantAndSkill?: boolean;
	assistantSelectionMode?: "single" | "multiple";
	executionMode?: "default" | "plan";
	setExecutionMode?: (mode: "default" | "plan") => void;
	isGenerating?: boolean;
	/** 连接器候选与选中状态：用于「添加连接器」多选弹窗。已选项在上方独立区域，可在此移除。 */
	connectorOptions?: PluginComposerOption[];
	connectorsLoading?: boolean;
	selectedConnectorIds?: string[];
	onSelectConnector?: (publicId: string) => void;
	onRemoveConnector?: (publicId: string) => void;
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

export function ComposerActionBar({
	inputValue,
	composerRef,
	onUpload,
	onUploadFolder,
	onBeforeAction,
	children,
	className,
	assistantOptions = [],
	skillOptions = [],
	skillsLoading,
	disableAssistantAndSkill = false,
	assistantSelectionMode = "multiple",
	executionMode,
	setExecutionMode,
	isGenerating,
	connectorOptions = [],
	connectorsLoading,
	selectedConnectorIds = [],
	onSelectConnector,
	onRemoveConnector,
}: ComposerActionBarProps) {
	const [assistantOpen, setAssistantOpen] = useState(false);
	const [assistantSearch, setAssistantSearch] = useState("");
	const [skillOpen, setSkillOpen] = useState(false);
	const [skillSearch, setSkillSearch] = useState("");
	const [connectorOpen, setConnectorOpen] = useState(false);
	const [connectorSearch, setConnectorSearch] = useState("");
	const [uploadMenuOpen, setUploadMenuOpen] = useState(false);
	const derivedSkillsLoading = skillOpen && skillsLoading;

	const selectedAssistantNames = useMemo(
		() => parseSelectedAssistantNames(inputValue),
		[inputValue],
	);
	const selectedSlashLabels = useMemo(() => parseSelectedSlashLabels(inputValue), [inputValue]);
	const selectedSkillCodes = useMemo(
		() => selectedSlashLabels.filter((code) => skillOptions.some((option) => option.code === code)),
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
			return [assistant.name, assistant.roleName, assistant.id, assistant.description]
				.join(" ")
				.toLowerCase()
				.includes(query);
		});
	}, [assistantOptions, assistantSearch, assistantSelectionMode, selectedAssistantNames]);
	const filteredSkills = useMemo(() => {
		const query = skillSearch.trim().toLowerCase();
		return skillOptions.filter((skill) => {
			if (selectedSkillCodes.includes(skill.code)) return false;
			if (!query) return true;
			return [skill.label, skill.code, skill.description].join(" ").toLowerCase().includes(query);
		});
	}, [selectedSkillCodes, skillOptions, skillSearch]);
	const filteredConnectors = useMemo(() => {
		const query = connectorSearch.trim().toLowerCase();
		return connectorOptions.filter((connector) => {
			if (connector.pluginId && selectedConnectorIds.includes(connector.pluginId)) {
				return false;
			}
			if (!query) return true;
			return [connector.label, connector.code, connector.description]
				.join(" ")
				.toLowerCase()
				.includes(query);
		});
	}, [connectorOptions, connectorSearch, selectedConnectorIds]);
	const selectedConnectors = useMemo(() => {
		return selectedConnectorIds
			.map((publicId) => connectorOptions.find((item) => item.pluginId === publicId))
			.filter((item): item is PluginComposerOption => Boolean(item));
	}, [connectorOptions, selectedConnectorIds]);

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
			{onUpload &&
				(onUploadFolder ? (
					<Popover open={uploadMenuOpen} onOpenChange={setUploadMenuOpen}>
						<PopoverTrigger
							type="button"
							onClick={(event) => {
								if (!allowAction()) {
									event.preventDefault();
								}
							}}
							className="order-5 inline-flex items-center gap-2 rounded-full px-2 py-1.5 text-sm text-slate-600 transition-colors hover:bg-slate-100 hover:text-slate-900"
						>
							<Plus className="size-4" />
							<span>上传文件</span>
						</PopoverTrigger>
						<PopoverContent
							align="start"
							side="top"
							sideOffset={10}
							collisionAvoidance={{ side: "none", align: "shift", fallbackAxisSide: "none" }}
							className="w-44 p-1.5"
						>
							<button
								type="button"
								onClick={() => {
									if (!allowAction()) return;
									setUploadMenuOpen(false);
									onUpload();
								}}
								className="flex w-full items-center gap-2.5 rounded-lg px-2 py-1.5 text-left text-sm text-slate-700 transition-colors hover:bg-slate-100"
							>
								<FileText className="size-4 shrink-0 text-slate-500" />
								<span>选择文件</span>
							</button>
							<button
								type="button"
								onClick={() => {
									if (!allowAction()) return;
									setUploadMenuOpen(false);
									onUploadFolder();
								}}
								className="flex w-full items-center gap-2.5 rounded-lg px-2 py-1.5 text-left text-sm text-slate-700 transition-colors hover:bg-slate-100"
							>
								<Folder className="size-4 shrink-0 text-slate-500" />
								<span>选择文件夹</span>
							</button>
						</PopoverContent>
					</Popover>
				) : (
					<button
						type="button"
						onClick={() => {
							if (!allowAction()) return;
							onUpload();
						}}
						className="order-5 inline-flex items-center gap-2 rounded-full px-2 py-1.5 text-sm text-slate-600 transition-colors hover:bg-slate-100 hover:text-slate-900"
					>
						<Plus className="size-4" />
						<span>上传文件</span>
					</button>
				))}
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
					className={cn(assistantSkillButtonClassName, "order-4")}
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
										// 中文注释：CommandItem 的 value 同时承担活动项标识；不能使用可能重复的名称。
										key={assistant.id}
										value={`assistant:${assistant.id}`}
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
											{/* 中文注释：选择弹窗固定两行，名称在上、角色名称在下。 */}
											{assistant.roleName ? (
												<div className="truncate text-xs text-slate-500">
													{renderHighlightedText(assistant.roleName, assistantSearch)}
												</div>
											) : null}
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
								{derivedSkillsLoading && (
									<div className="px-2 py-1.5 text-xs text-slate-400">技能加载中...</div>
								)}
								{filteredSkills.map((skill) => (
									<CommandItem
										key={skill.code}
										value={skill.label}
										onSelect={() => {
											composerRef.current?.insertSkill(skill.code);
										}}
										className="rounded-lg px-2 py-1.5"
									>
										<div className="flex size-7 shrink-0 items-center justify-center rounded-lg bg-violet-50 text-violet-600">
											<Sparkles className="size-3.5" />
										</div>
										<div className="min-w-0 flex-1">
											<div className="flex items-center gap-1.5 truncate font-medium">
												<span className="truncate">
													{renderHighlightedText(skill.label, skillSearch)}
												</span>
												{(skill.source === "builtin" || skill.origin === "builtin_worker") && (
													<span className="shrink-0 rounded bg-slate-100 px-1.5 py-0.5 text-[10px] font-normal leading-none text-slate-500">
														系统
													</span>
												)}
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
			<Popover
				open={connectorOpen}
				onOpenChange={(open) => {
					setConnectorOpen(open);
					if (!open) setConnectorSearch("");
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
						if (connectorOpen) return;
						if (event.defaultPrevented) return;
						if (!allowAction()) {
							event.preventDefault();
						}
					}}
					className={cn(assistantSkillButtonClassName, "order-3")}
				>
					<Cable className="size-4" />
					<span>添加连接器</span>
					{selectedConnectorIds.length > 0 && (
						<span className="inline-flex size-4 shrink-0 items-center justify-center rounded-full bg-slate-900 text-[10px] font-medium leading-none text-white">
							{selectedConnectorIds.length}
						</span>
					)}
				</PopoverTrigger>
				{/* 中文注释：固定在按钮上方，避免视口碰撞策略把选择弹窗动态翻到下方。 */}
				<PopoverContent
					align="start"
					side="top"
					sideOffset={10}
					collisionAvoidance={{ side: "none", align: "shift", fallbackAxisSide: "none" }}
					className="w-[340px] p-1.5"
				>
					<Command shouldFilter={false} className="rounded-xl! bg-transparent p-0">
						<div className="px-2 py-1 text-sm font-semibold text-slate-800">选择连接器</div>
						<CommandInput
							value={connectorSearch}
							onValueChange={setConnectorSearch}
							placeholder="搜索连接器"
							className="placeholder:text-slate-300"
						/>
						<CommandSeparator className="mx-1 my-2 bg-slate-200/80" />
						<CommandList className="max-h-72 overflow-y-auto px-1">
							{selectedConnectors.length > 0 && (
								<>
									<div className="px-2 py-1 text-xs font-semibold text-slate-400">已选连接器</div>
									<CommandGroup className="p-0">
										{selectedConnectors.map((connector) => {
											const publicId = connector.pluginId ?? connector.code;
											return (
												<CommandItem
													key={publicId}
													value={`selected:${connector.label}`}
													onSelect={() => {
														if (onRemoveConnector) {
															onRemoveConnector(publicId);
														}
													}}
													className="rounded-lg px-2 py-1.5"
												>
													<MCPConnectorIcon
														code={connector.code}
														name={connector.label}
														className="size-7 shrink-0 rounded-lg"
													/>
													<div className="min-w-0 flex-1">
														<span className="truncate font-medium text-slate-700">
															{connector.label}
														</span>
													</div>
													<span className="shrink-0 rounded-full bg-slate-100 px-1.5 py-0.5 text-[10px] font-medium text-slate-500">
														已选
													</span>
												</CommandItem>
											);
										})}
									</CommandGroup>
									<CommandSeparator className="mx-1 my-2 bg-slate-200/80" />
								</>
							)}
							<div className="px-2 pb-1 pt-1 text-xs font-semibold text-slate-400">可选连接器</div>
							<CommandEmpty className="py-6 text-slate-400">
								{connectorOptions.length === 0 && !connectorsLoading
									? "没有可用的连接器"
									: "没有可继续添加的连接器"}
							</CommandEmpty>
							<CommandGroup className="p-0">
								{connectorsLoading && connectorOptions.length === 0 && (
									<div className="px-2 py-1.5 text-xs text-slate-400">连接器加载中...</div>
								)}
								{filteredConnectors.map((connector) => (
									<CommandItem
										key={connector.pluginId ?? connector.code}
										value={connector.label}
										onSelect={() => {
											if (connector.pluginId && onSelectConnector) {
												onSelectConnector(connector.pluginId);
											}
										}}
										className="rounded-lg px-2 py-1.5"
									>
										<MCPConnectorIcon
											code={connector.code}
											name={connector.label}
											className="size-7 shrink-0 rounded-lg"
										/>
										<div className="min-w-0 flex-1">
											<div className="flex items-center gap-1.5 truncate font-medium">
												<span className="truncate">
													{renderHighlightedText(connector.label, connectorSearch)}
												</span>
											</div>
											<div className="truncate text-xs text-slate-400">{connector.description}</div>
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
