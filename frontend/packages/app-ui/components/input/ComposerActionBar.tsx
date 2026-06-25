"use client";

import { type SkillInstalledItem, skillMarketplaceApi } from "@leros/store";
import {
	Command,
	CommandEmpty,
	CommandGroup,
	CommandInput,
	CommandItem,
	CommandList,
} from "@leros/ui/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@leros/ui/components/ui/popover";
import { cn } from "@leros/ui/lib/utils";
import { Bot, Plus, Sparkles, WandSparkles, X } from "lucide-react";
import { type ReactNode, type RefObject, useEffect, useMemo, useState } from "react";
import { mockAssistants } from "./mockDirectiveData";
import type { ComposerSkillOption, StructuredComposerHandle } from "./StructuredComposer";

type ComposerActionBarProps = {
	inputValue: string;
	composerRef: RefObject<StructuredComposerHandle | null>;
	onUpload?: () => void;
	onBeforeAction?: () => boolean;
	children?: ReactNode;
	className?: string;
	projectSkillOptions?: ComposerSkillOption[];
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

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null;
}

function stringFromValue(value: unknown): string {
	return typeof value === "string" ? value : "";
}

function skillItemFromValue(value: unknown): SkillInstalledItem | null {
	if (!isRecord(value)) return null;

	const name = stringFromValue(value.name || value.skill_id || value.id);
	if (!name) return null;

	return {
		name,
		description: stringFromValue(value.description),
		category: stringFromValue(value.category),
		source: stringFromValue(value.source || value.source_type),
		trust: stringFromValue(value.trust),
	};
}

function normalizeInstalledSkillsPayload(value: unknown): SkillInstalledItem[] {
	const toItems = (items: unknown[]) =>
		items.map(skillItemFromValue).filter((item): item is SkillInstalledItem => item !== null);

	if (Array.isArray(value)) return toItems(value);
	if (!isRecord(value)) return [];

	const nestedData = value.data;
	if (isRecord(nestedData)) {
		if (Array.isArray(nestedData.skills)) return toItems(nestedData.skills);
		if (Array.isArray(nestedData.items)) return toItems(nestedData.items);
	}

	if (Array.isArray(value.skills)) return toItems(value.skills);
	if (Array.isArray(value.items)) return toItems(value.items);
	return [];
}

function installedSkillToOption(skill: SkillInstalledItem): SkillOption {
	return {
		code: skill.name,
		label: skill.name,
		description: skill.description || skill.category || "已安装技能",
		keywords: [skill.name, skill.description, skill.category, skill.source, skill.trust].filter(
			Boolean,
		),
	};
}

export function ComposerActionBar({
	inputValue,
	composerRef,
	onUpload,
	onBeforeAction,
	children,
	className,
	projectSkillOptions,
}: ComposerActionBarProps) {
	const [assistantOpen, setAssistantOpen] = useState(false);
	const [skillOpen, setSkillOpen] = useState(false);
	const [skillSearch, setSkillSearch] = useState("");
	const [skillOptions, setSkillOptions] = useState<SkillOption[]>([]);
	const [skillsLoading, setSkillsLoading] = useState(false);
	const [skillsLoaded, setSkillsLoaded] = useState(false);
	const [skillsError, setSkillsError] = useState<string | null>(null);

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
	const filteredAssistants = useMemo(
		() => mockAssistants.filter((assistant) => !selectedAssistantNames.includes(assistant.name)),
		[selectedAssistantNames],
	);
	const filteredSkills = useMemo(() => {
		const query = skillSearch.trim().toLowerCase();
		return skillOptions.filter((skill) => {
			if (selectedSkillLabels.includes(skill.label)) return false;
			if (!query) return true;
			return [skill.label, skill.code, skill.description, ...skill.keywords]
				.join(" ")
				.toLowerCase()
				.includes(query);
		});
	}, [selectedSkillLabels, skillOptions, skillSearch]);

	useEffect(() => {
		if (projectSkillOptions) {
			setSkillOptions(projectSkillOptions);
			setSkillsLoaded(true);
			setSkillsError(null);
			setSkillsLoading(false);
			return;
		}
		if (!skillOpen || skillsLoaded) return;

		setSkillsLoading(true);
		setSkillsError(null);
		skillMarketplaceApi
			.installed()
			.then((response) => {
				const raw = normalizeInstalledSkillsPayload(response.data);
				setSkillOptions(raw.map(installedSkillToOption));
				setSkillsLoaded(true);
			})
			.catch((error: unknown) => {
				const message = error instanceof Error ? error.message : "技能加载失败";
				setSkillsError(message);
				setSkillOptions([]);
			})
			.finally(() => {
				setSkillsLoading(false);
			});
	}, [projectSkillOptions, skillOpen, skillsLoaded]);

	const allowAction = () => (onBeforeAction ? onBeforeAction() : true);

	return (
		<div className={cn("flex flex-wrap items-center gap-2", className)}>
			{onUpload && (
				<button
					type="button"
					onClick={() => {
						if (!allowAction()) return;
						onUpload();
					}}
					className="inline-flex items-center gap-2 rounded-full px-2 py-1.5 text-sm text-slate-600 transition-colors hover:bg-slate-100 hover:text-slate-900"
				>
					<Plus className="size-4" />
					<span>上传文件</span>
				</button>
			)}
			<Popover open={assistantOpen} onOpenChange={setAssistantOpen}>
				<PopoverTrigger
					type="button"
					onClick={(event) => {
						if (assistantOpen) return;
						if (event.defaultPrevented) return;
						if (!allowAction()) {
							event.preventDefault();
						}
					}}
					className="inline-flex items-center gap-2 rounded-full px-2 py-1.5 text-sm text-slate-600 transition-colors hover:bg-slate-100 hover:text-slate-900"
				>
					<Bot className="size-4" />
					<span>召唤AI队友</span>
				</PopoverTrigger>
				<PopoverContent align="start" side="top" sideOffset={10} className="w-[320px] p-1.5">
					<div className="mb-1 px-2 py-1 text-xs font-medium text-slate-400">选择 AI 队友</div>
					{selectedAssistantNames.length > 0 && (
						<div className="px-2 pb-2">
							<div className="mb-1 text-[11px] font-medium text-slate-400">已选 AI 队友</div>
							<div className="flex flex-wrap gap-1.5">
								{selectedAssistantNames.map((name) => (
									<button
										key={name}
										type="button"
										onClick={() => composerRef.current?.removeAssistant(name)}
										className="inline-flex items-center gap-1 rounded-full bg-blue-50 px-2 py-1 text-[11px] text-blue-700 transition-colors hover:bg-blue-100"
									>
										@{name}
										<X className="size-3" />
									</button>
								))}
							</div>
						</div>
					)}
					<div className="max-h-64 overflow-y-auto">
						{filteredAssistants.length === 0 ? (
							<div className="px-3 py-6 text-center text-sm text-slate-400">
								没有可继续添加的 AI 队友
							</div>
						) : (
							filteredAssistants.map((assistant) => (
								<button
									key={assistant.code}
									type="button"
									onClick={() => {
										composerRef.current?.insertAssistant(assistant.name);
									}}
									className="flex w-full items-center gap-3 rounded-xl px-3 py-2 text-left transition-colors hover:bg-slate-100"
								>
									<div className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-blue-50 text-blue-600">
										<Bot className="size-4" />
									</div>
									<div className="min-w-0">
										<div className="truncate text-sm font-medium text-slate-700">
											{assistant.name}
										</div>
										<div className="truncate text-xs text-slate-400">{assistant.description}</div>
									</div>
								</button>
							))
						)}
					</div>
				</PopoverContent>
			</Popover>
			<Popover open={skillOpen} onOpenChange={setSkillOpen}>
				<PopoverTrigger
					type="button"
					onClick={(event) => {
						if (skillOpen) return;
						if (event.defaultPrevented) return;
						if (!allowAction()) {
							event.preventDefault();
						}
					}}
					className="inline-flex items-center gap-2 rounded-full px-2 py-1.5 text-sm text-slate-600 transition-colors hover:bg-slate-100 hover:text-slate-900"
				>
					<WandSparkles className="size-4" />
					<span>添加技能</span>
				</PopoverTrigger>
				<PopoverContent align="start" side="top" sideOffset={10} className="w-[340px] p-1.5">
					<Command shouldFilter={false} className="rounded-xl! bg-transparent p-0">
						<div className="px-2 py-1 text-xs font-medium text-slate-400">选择技能</div>
						<CommandInput
							value={skillSearch}
							onValueChange={setSkillSearch}
							placeholder="搜索技能"
						/>
						{selectedSkillLabels.length > 0 && (
							<div className="px-2 pb-2 pt-1">
								<div className="mb-1 text-[11px] font-medium text-slate-400">已选技能</div>
								<div className="flex flex-wrap gap-1.5">
									{selectedSkillLabels.map((label) => (
										<button
											key={label}
											type="button"
											onClick={() => composerRef.current?.removeSkill(label)}
											className="inline-flex items-center gap-1 rounded-full bg-violet-50 px-2 py-1 text-[11px] text-violet-700 transition-colors hover:bg-violet-100"
										>
											/{label}
											<X className="size-3" />
										</button>
									))}
								</div>
							</div>
						)}
						<CommandList className="max-h-64">
							<CommandEmpty className="py-6 text-slate-400">没有可继续添加的技能</CommandEmpty>
							<CommandGroup className="p-0">
								{skillsLoading && (
									<div className="px-3 py-2 text-xs text-slate-400">技能加载中...</div>
								)}
								{!skillsLoading && skillsError && (
									<div className="px-3 py-2 text-xs text-red-400">{skillsError}</div>
								)}
								{filteredSkills.map((skill) => (
									<CommandItem
										key={skill.code}
										value={skill.label}
										onSelect={() => {
											composerRef.current?.insertSkill(skill.label);
										}}
										className="rounded-xl px-2.5 py-2"
									>
										<div className="flex size-7 shrink-0 items-center justify-center rounded-lg bg-violet-50 text-violet-600">
											<Sparkles className="size-3.5" />
										</div>
										<div className="min-w-0 flex-1">
											<div className="truncate font-medium">/{skill.label}</div>
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
