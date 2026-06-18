"use client";

import type { DigitalAssistantItem } from "@leros/store";
import { Badge } from "@leros/ui/components/ui/badge";
import { Button } from "@leros/ui/components/ui/button";
import { ScrollArea } from "@leros/ui/components/ui/scroll-area";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@leros/ui/components/ui/tabs";
import { cn } from "@leros/ui/lib/utils";
import { BookOpen, Bot, Brain, Network, Pencil, Settings, Shield, Star } from "lucide-react";
import { useState } from "react";
import { AssistantAvatar } from "./AssistantAvatar";
import { AssistantEditDialog } from "./AssistantEditDialog";

const statusLabelMap: Record<
	string,
	{ label: string; variant: "default" | "secondary" | "destructive" }
> = {
	active: { label: "运行中", variant: "default" },
	inactive: { label: "已停用", variant: "secondary" },
	draft: { label: "草稿", variant: "secondary" },
};

const configTabs = [
	{ value: "basic", label: "基础信息", icon: <Bot className="size-3.5" /> },
	{ value: "llm", label: "LLM 配置", icon: <Brain className="size-3.5" /> },
	{ value: "skills", label: "技能", icon: <Star className="size-3.5" /> },
	{ value: "channels", label: "渠道", icon: <Network className="size-3.5" /> },
	{ value: "knowledge", label: "知识", icon: <BookOpen className="size-3.5" /> },
	{ value: "memory", label: "记忆", icon: <Brain className="size-3.5" /> },
	{ value: "policy", label: "策略", icon: <Shield className="size-3.5" /> },
	{ value: "runtime", label: "运行时", icon: <Settings className="size-3.5" /> },
];

export type AssistantDetailPanelProps = {
	assistant: DigitalAssistantItem;
	className?: string;
};

export function AssistantDetailPanel({ assistant, className }: AssistantDetailPanelProps) {
	const [editOpen, setEditOpen] = useState(false);

	const statusInfo = statusLabelMap[assistant.status] ?? {
		label: assistant.status,
		variant: "secondary" as const,
	};

	return (
		<div
			data-slot="assistant-detail-panel"
			className={cn("flex h-full flex-col bg-white", className)}
		>
			<div className="flex flex-col items-center gap-3 border-b border-slate-200 px-4 py-6">
				<AssistantAvatar name={assistant.name} src={assistant.avatar} size="lg" />
				<div className="flex items-center gap-2">
					<span className="text-base font-semibold text-slate-900">{assistant.name}</span>
					<Badge variant={statusInfo.variant} className="text-xs">
						{statusInfo.label}
					</Badge>
				</div>
				<Button variant="outline" size="sm" onClick={() => setEditOpen(true)}>
					<Pencil className="size-3.5 mr-1" />
					编辑
				</Button>
			</div>

			<ScrollArea className="flex-1">
				<div className="p-4">
					<Tabs defaultValue="basic">
						<TabsList variant="line" className="w-full">
							{configTabs.map((tab) => (
								<TabsTrigger key={tab.value} value={tab.value} className="flex-1 text-xs">
									{tab.icon}
									<span className="hidden xl:inline ml-1">{tab.label}</span>
								</TabsTrigger>
							))}
						</TabsList>

						<TabsContent value="basic" className="mt-4 space-y-3">
							<DetailField label="编码" value={assistant.code} />
							<DetailField label="描述" value={assistant.description || "暂无"} />
							<DetailField label="系统提示词" value={assistant.systemPrompt || "暂无"} multiline />
							<DetailField label="版本" value={`v${assistant.version}`} />
							<DetailField
								label="创建时间"
								value={new Date(assistant.createdAt).toLocaleDateString("zh-CN")}
							/>
							<DetailField
								label="更新时间"
								value={new Date(assistant.updatedAt).toLocaleDateString("zh-CN")}
							/>
						</TabsContent>

						<TabsContent value="llm" className="mt-4">
							<EmptyConfig message="LLM 配置暂未设置" />
						</TabsContent>

						<TabsContent value="skills" className="mt-4">
							<EmptyConfig message="技能配置暂未设置" />
						</TabsContent>

						<TabsContent value="channels" className="mt-4">
							<EmptyConfig message="渠道配置暂未设置" />
						</TabsContent>

						<TabsContent value="knowledge" className="mt-4">
							<EmptyConfig message="知识配置暂未设置" />
						</TabsContent>

						<TabsContent value="memory" className="mt-4">
							<EmptyConfig message="记忆配置暂未设置" />
						</TabsContent>

						<TabsContent value="policy" className="mt-4">
							<EmptyConfig message="策略配置暂未设置" />
						</TabsContent>

						<TabsContent value="runtime" className="mt-4">
							<EmptyConfig message="运行时配置暂未设置" />
						</TabsContent>
					</Tabs>
				</div>
			</ScrollArea>

			<AssistantEditDialog assistant={assistant} open={editOpen} onOpenChange={setEditOpen} />
		</div>
	);
}

function DetailField({
	label,
	value,
	multiline,
}: {
	label: string;
	value: string;
	multiline?: boolean;
}) {
	return (
		<div className="space-y-1">
			<span className="text-xs font-medium text-slate-500">{label}</span>
			<div
				className={cn(
					"text-sm text-slate-800",
					multiline
						? "whitespace-pre-wrap break-all bg-slate-50 rounded-md p-2 border border-slate-100"
						: "",
				)}
			>
				{value}
			</div>
		</div>
	);
}

function EmptyConfig({ message }: { message: string }) {
	return (
		<div className="flex flex-col items-center justify-center py-8 text-slate-400 text-xs">
			<span>{message}</span>
		</div>
	);
}
