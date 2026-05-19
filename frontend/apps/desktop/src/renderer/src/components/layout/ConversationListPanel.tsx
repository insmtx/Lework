"use client";

import { useChatStore, useLayoutStore } from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@leros/ui/components/ui/dialog";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuTrigger,
} from "@leros/ui/components/ui/dropdown-menu";
import { ScrollArea } from "@leros/ui/components/ui/scroll-area";
import { cn } from "@leros/ui/lib/utils";
import { MoreHorizontal, Pencil, Plus, Search, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";

export function ConversationListPanel() {
	const {
		conversations,
		activeConversationId,
		conversationSearchQuery,
		conversationListOpen,
		switchConversation,
		createConversation,
		deleteConversation,
		updateConversationTitle,
		setConversationSearchQuery,
		fetchConversations,
	} = useLayoutStore((s) => s);

	const { setActiveSession, loadConversationMessages } = useChatStore((s) => s);

	// Rename dialog state
	const [renameDialogOpen, setRenameDialogOpen] = useState(false);
	const [renameTargetId, setRenameTargetId] = useState<string | null>(null);
	const [renameValue, setRenameValue] = useState("");

	useEffect(() => {
		fetchConversations();
	}, [fetchConversations]);

	const filteredConversations = conversationSearchQuery
		? conversations.filter((c) =>
				c.title.toLowerCase().includes(conversationSearchQuery.toLowerCase()),
			)
		: conversations;

	const handleConversationClick = (id: string) => {
		switchConversation(id);
		setActiveSession(id);
		loadConversationMessages(id);
	};

	const handleCreateConversation = async () => {
		const conv = await createConversation("新会话");
		if (conv) {
			switchConversation(conv.id);
			setActiveSession(conv.id);
			loadConversationMessages(conv.id);
		}
	};

	const handleDeleteConversation = async (id: string) => {
		await deleteConversation(id);
	};

	const handleOpenRename = (conv: { id: string; title: string }) => {
		setRenameTargetId(conv.id);
		setRenameValue(conv.title);
		setRenameDialogOpen(true);
	};

	const handleConfirmRename = async () => {
		if (renameTargetId && renameValue.trim()) {
			await updateConversationTitle(renameTargetId, renameValue.trim());
			setRenameDialogOpen(false);
			setRenameTargetId(null);
			setRenameValue("");
		}
	};

	if (!conversationListOpen) return null;

	return (
		<>
			<div
				data-slot="conversation-list-panel"
				className="flex h-full w-[260px] flex-col border-r border-slate-200 bg-white transition-all duration-300"
			>
				<div className="flex items-center gap-2 border-b border-slate-200 px-3 py-2.5">
					<div className="relative flex-1">
						<Search className="absolute left-2.5 top-1/2 -translate-y-1/2 size-3.5 text-slate-400" />
						<input
							type="text"
							value={conversationSearchQuery}
							onChange={(e) => setConversationSearchQuery(e.target.value)}
							placeholder="搜索会话"
							className="w-full rounded-md border border-slate-200 bg-slate-50 py-1.5 pl-7 pr-2 text-xs text-slate-600 placeholder:text-slate-400 focus:border-blue-300 focus:bg-white focus:outline-none transition-colors"
						/>
					</div>
					<Button
						variant="ghost"
						size="icon-sm"
						className="text-slate-500 hover:text-slate-700 hover:bg-slate-50 shrink-0"
						onClick={handleCreateConversation}
					>
						<Plus className="size-4" />
					</Button>
				</div>

				<ScrollArea className="flex-1">
					<div className="px-3 pb-2">
						{filteredConversations.map((conv) => (
							<button
								key={conv.id}
								type="button"
								className={cn(
									"group relative flex items-center rounded-md px-2 py-1.5 text-sm cursor-pointer transition-colors w-full text-left",
									activeConversationId === conv.id
										? "bg-blue-50 text-blue-700"
										: "text-slate-600 hover:bg-slate-50",
								)}
								onClick={() => handleConversationClick(conv.id)}
							>
								<span className="truncate flex-1">{conv.title}</span>
								<DropdownMenu>
									<DropdownMenuTrigger
										render={
											<Button
												variant="ghost"
												size="icon-xs"
												className="opacity-0 group-hover:opacity-100 transition-opacity text-slate-400 hover:text-slate-600 shrink-0"
												onClick={(e: React.MouseEvent) => e.stopPropagation()}
											>
												<MoreHorizontal className="size-3.5" />
											</Button>
										}
									/>
									<DropdownMenuContent align="end" sideOffset={4}>
										<DropdownMenuItem onClick={() => handleOpenRename(conv)}>
											<Pencil className="size-3.5 mr-2" />
											<span>重命名</span>
										</DropdownMenuItem>
										<DropdownMenuItem
											variant="destructive"
											onClick={() => handleDeleteConversation(conv.id)}
										>
											<Trash2 className="size-3.5 mr-2" />
											<span>删除</span>
										</DropdownMenuItem>
									</DropdownMenuContent>
								</DropdownMenu>
							</button>
						))}
					</div>
				</ScrollArea>
			</div>

			{/* Rename Dialog */}
			<Dialog open={renameDialogOpen} onOpenChange={setRenameDialogOpen}>
				<DialogContent className="sm:max-w-md" showCloseButton={false}>
					<DialogHeader>
						<DialogTitle>重命名会话</DialogTitle>
						<DialogDescription>请输入新的会话名称</DialogDescription>
					</DialogHeader>
					<div className="mt-4">
						<input
							type="text"
							value={renameValue}
							onChange={(e) => setRenameValue(e.target.value)}
							onKeyDown={(e) => {
								if (e.key === "Enter") {
									handleConfirmRename();
								}
							}}
							placeholder="会话名称"
							autoFocus
							className="w-full rounded-md border border-slate-200 bg-white px-3 py-2 text-sm text-slate-800 placeholder:text-slate-400 focus:border-blue-300 focus:outline-none transition-colors"
						/>
					</div>
					<DialogFooter className="mt-4">
						<Button variant="outline" onClick={() => setRenameDialogOpen(false)}>
							取消
						</Button>
						<button
							type="button"
							onClick={handleConfirmRename}
							disabled={!renameValue.trim()}
							className="inline-flex items-center justify-center rounded-lg bg-primary text-primary-foreground h-8 px-2.5 text-sm font-medium transition-all disabled:pointer-events-none disabled:opacity-50 hover:bg-primary/80"
						>
							确认
						</button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		</>
	);
}
