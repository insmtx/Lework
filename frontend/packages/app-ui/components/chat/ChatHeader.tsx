"use client";

import { formatTokenCount, useChatStore, useLayoutStore } from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import { Ellipsis, FileText, Plus, Search, Settings, Share2 } from "lucide-react";
import { AssistantChatAvatar } from "./AssistantChatAvatar";

export function ChatHeader() {
	const { selectedModel, modelOptions, tokenUsage } = useChatStore((s) => s);
	const { conversations, activeConversationId } = useLayoutStore((s) => s);

	const currentModel = modelOptions.find((m) => m.id === selectedModel);
	const activeConversation = conversations.find((c) => c.id === activeConversationId);

	return (
		<div
			data-slot="chat-header"
			className="flex h-14 items-center justify-between border-b border-slate-200/60 bg-white/90 px-5 backdrop-blur"
		>
			<div className="flex items-center gap-3">
				<AssistantChatAvatar />
				<div className="flex flex-col">
					<span className="text-sm font-semibold text-slate-800">
						{activeConversation?.title ?? "选择一个会话"}
					</span>
					<span className="text-xs text-slate-400">{currentModel?.label ?? "GPT-4"}</span>
				</div>
			</div>

			<div className="flex items-center gap-1">
				<Button variant="ghost" size="icon-sm" className="text-slate-400 hover:text-slate-700">
					<Plus className="size-4" />
				</Button>

				<div className="flex items-center gap-1 rounded-md bg-slate-100/70 px-2.5 py-1 text-xs text-slate-500 ring-1 ring-slate-200/50">
					<FileText className="size-3.5" />
					<span>{formatTokenCount(tokenUsage.total)}</span>
				</div>

				<Button variant="ghost" size="icon-sm" className="text-slate-400 hover:text-slate-700">
					<Search className="size-4" />
				</Button>

				<Button variant="ghost" size="icon-sm" className="text-slate-400 hover:text-slate-700">
					<Settings className="size-4" />
				</Button>

				<Button variant="ghost" size="sm" className="text-slate-400 hover:text-slate-700">
					<Share2 className="size-3.5" />
					<span className="ml-1">分享</span>
				</Button>

				<Button variant="ghost" size="icon-sm" className="text-slate-400 hover:text-slate-700">
					<Ellipsis className="size-4" />
				</Button>
			</div>
		</div>
	);
}
