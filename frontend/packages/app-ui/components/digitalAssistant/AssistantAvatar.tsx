"use client";

import { cn } from "@leros/ui/lib/utils";
import { useState } from "react";
import { DiceBearAvatar } from "../avatar/DiceBearAvatar";

const sizeClassMap = {
	sm: "size-7 text-xs",
	default: "size-12 text-lg",
	lg: "size-16 text-2xl",
};

export function AssistantAvatar({
	name,
	src,
	size = "default",
	className,
}: {
	name: string;
	src?: string | null;
	size?: keyof typeof sizeClassMap;
	className?: string;
}) {
	const [failed, setFailed] = useState(false);
	const sizeClass = sizeClassMap[size];
	const pixelSize = size === "lg" ? 128 : size === "sm" ? 56 : 96;

	return (
		<div
			className={cn(
				"flex shrink-0 items-center justify-center rounded-full bg-gradient-to-br from-blue-500 to-indigo-600 font-semibold text-white",
				sizeClass,
				className,
			)}
		>
			{src && !failed ? (
				<img
					src={src}
					alt={name}
					className={cn("rounded-full object-cover", sizeClass)}
					loading="lazy"
					decoding="async"
					referrerPolicy="no-referrer"
					onError={() => setFailed(true)}
				/>
			) : (
				<DiceBearAvatar
					seed={`digital-assistant:${name}`}
					alt={name}
					className={sizeClass}
					size={pixelSize}
				/>
			)}
		</div>
	);
}
