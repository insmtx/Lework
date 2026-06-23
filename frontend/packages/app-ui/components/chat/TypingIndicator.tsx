"use client";

import { AssistantChatAvatar } from "./AssistantChatAvatar";

export function TypingIndicator() {
	return (
		<div data-slot="typing-indicator" className="flex items-start gap-3">
			<AssistantChatAvatar />
			<div className="rounded-2xl rounded-tl-md bg-white/90 px-4 py-3 shadow-sm ring-1 ring-slate-200/50">
				<div className="flex items-center gap-1.5">
					<span className="size-1.5 rounded-full bg-slate-400 animate-pulse" />
					<span className="size-1.5 rounded-full bg-slate-400 animate-pulse [animation-delay:200ms]" />
					<span className="size-1.5 rounded-full bg-slate-400 animate-pulse [animation-delay:400ms]" />
				</div>
			</div>
		</div>
	);
}
