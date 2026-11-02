"use client";

import { formatDate } from "@lework/store";

export function DateDivider({ timestamp }: { timestamp: number }) {
	return (
		<div data-slot="date-divider" className="flex items-center justify-center py-2">
			<span className="text-xs text-slate-400 bg-slate-100 rounded-full px-3 py-1">
				{formatDate(timestamp)}
			</span>
		</div>
	);
}
