"use client";

import { useDAStore } from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import { ScrollArea } from "@leros/ui/components/ui/scroll-area";
import { cn } from "@leros/ui/lib/utils";
import { Plus, Search } from "lucide-react";
import { useEffect, useState } from "react";
import { AssistantAvatar } from "./AssistantAvatar";

const statusDotMap: Record<string, string> = {
	active: "bg-green-500",
	inactive: "bg-slate-400",
	draft: "bg-yellow-400",
};

export type AssistantListPanelProps = {
	onCreateClick: () => void;
};

export function AssistantListPanel({ onCreateClick }: AssistantListPanelProps) {
	const { assistants, activeAssistantId, fetchAssistants, switchAssistant } = useDAStore((s) => s);

	const [searchQuery, setSearchQuery] = useState("");

	useEffect(() => {
		fetchAssistants();
	}, [fetchAssistants]);

	const filteredAssistants = searchQuery
		? assistants.filter((a) => a.name.toLowerCase().includes(searchQuery.toLowerCase()))
		: assistants;

	return (
		<div
			data-slot="assistant-list-panel"
			className="flex h-full w-[260px] flex-col border-r border-slate-200 bg-white transition-all duration-300"
		>
			<div className="flex items-center gap-2 border-b border-slate-200 h-12 px-3">
				<div className="relative flex-1">
					<Search className="absolute left-2.5 top-1/2 -translate-y-1/2 size-3.5 text-slate-400" />
					<input
						type="text"
						value={searchQuery}
						onChange={(e) => setSearchQuery(e.target.value)}
						placeholder="搜索员工"
						className="w-full rounded-md border border-slate-200 bg-slate-50 py-1.5 pl-7 pr-2 text-xs text-slate-600 placeholder:text-slate-400 focus:border-blue-300 focus:bg-white focus:outline-none transition-colors"
					/>
				</div>
				<Button
					variant="ghost"
					size="icon-sm"
					className="text-slate-500 hover:text-slate-700 hover:bg-slate-50 shrink-0"
					onClick={onCreateClick}
				>
					<Plus className="size-4" />
				</Button>
			</div>

			<ScrollArea className="flex-1">
				<div className="px-3 pt-2 pb-2">
					{filteredAssistants.map((a) => (
						<button
							key={a.id}
							type="button"
							className={cn(
								"group relative flex items-center gap-2 rounded-md px-2 py-1.5 text-sm cursor-pointer transition-colors w-full text-left",
								activeAssistantId === a.id
									? "bg-blue-50 text-blue-700"
									: "text-slate-600 hover:bg-slate-50",
							)}
							onClick={() => switchAssistant(a.id)}
						>
							<AssistantAvatar name={a.name} src={a.avatar} size="sm" />
							<span className="truncate flex-1">{a.name}</span>
							<span
								className={`size-2 rounded-full shrink-0 ${statusDotMap[a.status] ?? "bg-slate-300"}`}
							/>
						</button>
					))}
				</div>
			</ScrollArea>
		</div>
	);
}
