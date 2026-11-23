"use client";

import { Popover, PopoverContent, PopoverTrigger } from "@lework/ui/components/ui/popover";
import { cn } from "@lework/ui/lib/utils";
import { Ban, Palette } from "lucide-react";
import { useState } from "react";
import { NODE_COLOR_STYLES, NODE_COLOR_TOKENS, nodeColorStyle } from "../node-colors";

interface NodeColorPickerProps {
	nodeName: string;
	value?: string;
	onSelect: (color: string) => void | Promise<void>;
}

/**
 * NodeColorPicker 是节点身份色的唯一入口。
 *
 * 选择即写入、弹层立即关闭，不设保存按钮；写入失败由父组件回滚并在
 * 侧栏底部提示，因此这里只负责触发，不持有异步状态。
 */
export function NodeColorPicker({ nodeName, value, onSelect }: NodeColorPickerProps) {
	const [open, setOpen] = useState(false);
	const selected = nodeColorStyle(value);

	const select = (color: string) => {
		setOpen(false);
		// 失败由父组件的 actionError 呈现；这里吞掉 rejection 避免控制台告警。
		void Promise.resolve(onSelect(color)).catch(() => {});
	};

	return (
		<Popover open={open} onOpenChange={setOpen}>
			<PopoverTrigger
				type="button"
				aria-label={`Set color for ${nodeName}`}
				className="flex size-7 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
			>
				{selected ? (
					<span aria-hidden className={cn("size-3.5 rounded-full", selected.dot)} />
				) : (
					<Palette className="size-3.5" />
				)}
			</PopoverTrigger>
			<PopoverContent align="end" sideOffset={4} className="w-auto p-2">
				<div className="mb-1.5 px-0.5 text-[11px] font-medium uppercase tracking-wider text-muted-foreground">
					Node color
				</div>
				<div className="grid grid-cols-4 gap-1.5">
					{NODE_COLOR_TOKENS.map((token) => {
						const style = NODE_COLOR_STYLES[token];
						const active = value === token;
						return (
							<button
								key={token}
								type="button"
								title={style.label}
								aria-label={style.label}
								aria-pressed={active}
								onClick={() => select(token)}
								className={cn(
									"flex size-8 items-center justify-center rounded-full transition-transform hover:scale-105 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
									active && "ring-2 ring-foreground/50",
								)}
							>
								<span aria-hidden className={cn("size-6 rounded-full", style.dot)} />
							</button>
						);
					})}
				</div>
				<button
					type="button"
					onClick={() => select("")}
					className="mt-2 flex w-full items-center gap-1.5 rounded-md px-1.5 py-1 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
				>
					<Ban className="size-3.5" />
					No color
				</button>
			</PopoverContent>
		</Popover>
	);
}
