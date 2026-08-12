"use client";

import { Dialog as DialogPrimitive } from "@base-ui/react/dialog";
import { cn } from "@leros/ui/lib/utils";
import { XIcon } from "lucide-react";
import type * as React from "react";
import { Button } from "./button";

function Dialog({ disablePointerDismissal = true, ...props }: DialogPrimitive.Root.Props) {
	return (
		<DialogPrimitive.Root
			data-slot="dialog"
			disablePointerDismissal={disablePointerDismissal}
			{...props}
		/>
	);
}

function DialogTrigger({ ...props }: DialogPrimitive.Trigger.Props) {
	return <DialogPrimitive.Trigger data-slot="dialog-trigger" {...props} />;
}

function DialogClose({ ...props }: DialogPrimitive.Close.Props) {
	return <DialogPrimitive.Close data-slot="dialog-close" {...props} />;
}

function DialogPortal({ ...props }: DialogPrimitive.Portal.Props) {
	return <DialogPrimitive.Portal data-slot="dialog-portal" {...props} />;
}

function DialogOverlay({ className, ...props }: DialogPrimitive.Backdrop.Props) {
	return (
		<DialogPrimitive.Backdrop
			data-slot="dialog-overlay"
			className={cn(
				"fixed inset-0 z-50 bg-black/40 transition-opacity duration-150 data-[ending-style]:opacity-0 data-[starting-style]:opacity-0",
				className,
			)}
			{...props}
		/>
	);
}

function DialogViewport({ className, ...props }: DialogPrimitive.Viewport.Props) {
	return (
		<DialogPrimitive.Viewport
			data-slot="dialog-viewport"
			className={cn("fixed inset-0 z-50 flex items-center justify-center p-4", className)}
			{...props}
		/>
	);
}

function DialogContent({
	className,
	children,
	showCloseButton = true,
	...props
}: DialogPrimitive.Popup.Props & {
	showCloseButton?: boolean;
}) {
	return (
		<DialogPortal>
			<DialogOverlay />
			<DialogViewport>
				<DialogPrimitive.Popup
					data-slot="dialog-content"
					className={cn(
						"relative max-h-[calc(100dvh-2rem)] w-full max-w-sm overflow-y-auto rounded-xl border bg-background p-6 shadow-lg outline-none",
						"transition-[opacity,transform] duration-150 ease-out data-[ending-style]:scale-95 data-[ending-style]:opacity-0 data-[starting-style]:scale-95 data-[starting-style]:opacity-0",
						className,
					)}
					{...props}
				>
					{children}
					{showCloseButton && (
						<DialogPrimitive.Close
							data-slot="dialog-close"
							render={
								<Button
									variant="ghost"
									size="icon-sm"
									className="absolute top-4 right-4 opacity-70 hover:opacity-100"
								/>
							}
						>
							<XIcon className="size-4" />
							<span className="sr-only">关闭</span>
						</DialogPrimitive.Close>
					)}
				</DialogPrimitive.Popup>
			</DialogViewport>
		</DialogPortal>
	);
}

function DialogHeader({ className, ...props }: React.ComponentProps<"div">) {
	return (
		<div data-slot="dialog-header" className={cn("flex flex-col gap-2", className)} {...props} />
	);
}

function DialogFooter({ className, ...props }: React.ComponentProps<"div">) {
	return (
		<div
			data-slot="dialog-footer"
			className={cn("flex flex-col-reverse gap-2 sm:flex-row sm:justify-end", className)}
			{...props}
		/>
	);
}

function DialogTitle({ className, ...props }: DialogPrimitive.Title.Props) {
	return (
		<DialogPrimitive.Title
			data-slot="dialog-title"
			className={cn("text-lg font-semibold leading-none tracking-tight", className)}
			{...props}
		/>
	);
}

function DialogDescription({ className, ...props }: DialogPrimitive.Description.Props) {
	return (
		<DialogPrimitive.Description
			data-slot="dialog-description"
			className={cn("text-sm text-muted-foreground", className)}
			{...props}
		/>
	);
}

export {
	Dialog,
	DialogClose,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogOverlay,
	DialogPortal,
	DialogTitle,
	DialogTrigger,
	DialogViewport,
};
