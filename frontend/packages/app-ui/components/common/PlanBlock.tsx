"use client";

import { Check, Copy, LoaderCircle, Maximize2 } from "lucide-react";
import { type ReactNode, useState } from "react";

export interface PlanBlockProps {
	fileId: string;
	children: ReactNode;
	onOpen?: (fileId: string) => void;
	onCopy?: (fileId: string) => Promise<void>;
}

export function PlanBlock({ fileId, children, onOpen, onCopy }: PlanBlockProps) {
	const [copyStatus, setCopyStatus] = useState<"idle" | "copying" | "copied">("idle");
	const content = (
		<>
			<div className="relative z-20 mb-3 flex items-center justify-between gap-3">
				<div className="text-sm font-semibold tracking-[0.01em] text-slate-800">计划</div>
				{(onCopy || onOpen) && (
					<div className="flex items-center gap-1">
						{onCopy && (
							<button
								type="button"
								aria-label="复制完整计划"
								disabled={copyStatus === "copying"}
								onClick={async (event) => {
									event.stopPropagation();
									setCopyStatus("copying");
									try {
										await onCopy(fileId);
										setCopyStatus("copied");
										window.setTimeout(() => setCopyStatus("idle"), 1500);
									} catch {
										setCopyStatus("idle");
									}
								}}
								className="flex size-8 items-center justify-center rounded-lg text-slate-500 transition-colors hover:bg-slate-100 hover:text-slate-900 disabled:cursor-wait disabled:opacity-60"
								title="复制完整计划"
							>
								{copyStatus === "copying" ? (
									<LoaderCircle className="size-4 animate-spin" />
								) : copyStatus === "copied" ? (
									<Check className="size-4" />
								) : (
									<Copy className="size-4" />
								)}
							</button>
						)}
						{onOpen && (
							<button
								type="button"
								aria-label="预览完整计划"
								onClick={(event) => {
									event.stopPropagation();
									onOpen(fileId);
								}}
								className="flex size-8 items-center justify-center rounded-lg text-slate-500 transition-colors hover:bg-slate-100 hover:text-slate-900"
								title="预览完整计划"
							>
								<Maximize2 className="size-4" />
							</button>
						)}
					</div>
				)}
			</div>
			<div data-testid="plan-overview-viewport" className="relative h-56 overflow-hidden">
				<div
					data-testid="plan-overview-content"
					className="plan-block-content absolute inset-x-0 top-0 min-h-[28rem] text-slate-700"
				>
					{children}
				</div>
				<div
					data-testid="plan-overview-fade"
					className="pointer-events-none absolute inset-x-0 bottom-0 z-[1] h-8 bg-gradient-to-t from-white/65 via-white/20 to-transparent"
				/>
			</div>
		</>
	);

	if (!onOpen) {
		return (
			<div className="my-3 overflow-hidden rounded-2xl border border-slate-200/70 bg-white px-5 pt-5 shadow-[0_18px_45px_-38px_rgba(15,23,42,0.65)]">
				{content}
			</div>
		);
	}

	return (
		<div className="group/plan relative my-3 block w-full overflow-hidden rounded-2xl border border-slate-200/70 bg-white px-5 pt-5 text-left shadow-[0_18px_45px_-38px_rgba(15,23,42,0.65)] transition-[background-color,box-shadow] duration-200 hover:bg-slate-50/45 hover:shadow-[0_22px_52px_-36px_rgba(15,23,42,0.72)]">
			{content}
			<button
				type="button"
				aria-label="打开完整计划"
				onClick={() => onOpen(fileId)}
				className="absolute inset-0 z-10 cursor-pointer rounded-2xl focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-slate-400/60"
				title="点击预览完整计划"
			/>
		</div>
	);
}
