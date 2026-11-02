"use client";

import { Popover, PopoverContent, PopoverTrigger } from "@lework/ui/components/ui/popover";
import { cn } from "@lework/ui/lib/utils";
import { Clock } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";

const HOURS = Array.from({ length: 24 }, (_, i) => i);
const MINUTES = Array.from({ length: 60 }, (_, i) => i);

const ITEM_CLASS =
	"flex h-8 w-full shrink-0 cursor-pointer items-center justify-center rounded-md text-sm tabular-nums outline-none transition-colors hover:bg-[var(--lework-primary-softer)] hover:text-[var(--lework-primary)]";
const ITEM_SELECTED_CLASS =
	"bg-[var(--lework-primary-softer)] font-medium text-[var(--lework-primary)] hover:bg-[var(--lework-primary-softer)] hover:text-[var(--lework-primary)]";

function pad2(n: number): string {
	return String(n).padStart(2, "0");
}

function parseTime(value: string): { hour: number; minute: number } {
	const [hStr, mStr] = value.split(":");
	const hour = Number(hStr);
	const minute = Number(mStr);
	return {
		hour: Number.isFinite(hour) ? Math.min(23, Math.max(0, hour)) : 0,
		minute: Number.isFinite(minute) ? Math.min(59, Math.max(0, minute)) : 0,
	};
}

export function TimePickerField({
	value,
	onChange,
	"aria-label": ariaLabel = "选择时间",
}: {
	value: string;
	onChange: (value: string) => void;
	"aria-label"?: string;
}) {
	const [open, setOpen] = useState(false);
	const { hour, minute } = useMemo(() => parseTime(value), [value]);
	const hourItemRef = useRef<HTMLButtonElement | null>(null);
	const minuteItemRef = useRef<HTMLButtonElement | null>(null);

	useEffect(() => {
		if (!open) return;
		const centerInParent = (el: HTMLElement | null) => {
			const parent = el?.parentElement;
			if (!el || !parent) return;
			parent.scrollTop = el.offsetTop - parent.clientHeight / 2 + el.clientHeight / 2;
		};
		centerInParent(hourItemRef.current);
		centerInParent(minuteItemRef.current);
	}, [open]);

	return (
		<Popover open={open} onOpenChange={setOpen}>
			<PopoverTrigger
				type="button"
				aria-label={ariaLabel}
				className="relative flex h-9 min-w-0 w-full cursor-pointer items-center rounded-lg border border-slate-200 bg-white pl-3 pr-8 text-left text-sm font-normal text-slate-800 shadow-none transition-colors hover:border-slate-300 focus:border-[#4f46e5] focus:outline-none"
			>
				<span className="tabular-nums">{`${pad2(hour)}:${pad2(minute)}`}</span>
				<Clock className="pointer-events-none absolute right-2.5 size-4 text-slate-400" />
			</PopoverTrigger>
			<PopoverContent align="start" sideOffset={4} className="w-[152px] overflow-hidden p-1.5">
				<div className="grid grid-cols-2 gap-1">
					<div
						role="listbox"
						aria-label="小时"
						className="no-scrollbar relative flex max-h-49 flex-col gap-1 overflow-y-auto overscroll-contain"
					>
						{HOURS.map((h) => {
							const selected = h === hour;
							return (
								<button
									key={h}
									type="button"
									role="option"
									aria-selected={selected}
									ref={selected ? hourItemRef : undefined}
									className={cn(ITEM_CLASS, selected && ITEM_SELECTED_CLASS)}
									onClick={() => onChange(`${pad2(h)}:${pad2(minute)}`)}
								>
									{pad2(h)}
								</button>
							);
						})}
					</div>
					<div
						role="listbox"
						aria-label="分钟"
						className="no-scrollbar relative flex max-h-49 flex-col gap-1 overflow-y-auto overscroll-contain"
					>
						{MINUTES.map((m) => {
							const selected = m === minute;
							return (
								<button
									key={m}
									type="button"
									role="option"
									aria-selected={selected}
									ref={selected ? minuteItemRef : undefined}
									className={cn(ITEM_CLASS, selected && ITEM_SELECTED_CLASS)}
									onClick={() => onChange(`${pad2(hour)}:${pad2(m)}`)}
								>
									{pad2(m)}
								</button>
							);
						})}
					</div>
				</div>
			</PopoverContent>
		</Popover>
	);
}
