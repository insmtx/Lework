"use client";

import { Avatar, Style } from "@dicebear/core";
import notionists from "@dicebear/styles/notionists.json" with { type: "json" };
import { cn } from "@leros/ui/lib/utils";
import { useMemo } from "react";

const notionistsStyle = new Style(notionists);
const DEFAULT_BACKGROUND_COLOR = "#4f46e5";

export function DiceBearAvatar({
	seed,
	alt,
	className,
	size = 128,
	backgroundColor = DEFAULT_BACKGROUND_COLOR,
}: {
	seed: string;
	alt: string;
	className?: string;
	size?: number;
	backgroundColor?: string;
}) {
	const src = useMemo(() => {
		try {
			return new Avatar(notionistsStyle, {
				seed,
				size,
				borderRadius: 50,
				backgroundColor: [backgroundColor],
			}).toDataUri();
		} catch (err) {
			console.error("generate DiceBear avatar error:", err);
			return null;
		}
	}, [backgroundColor, seed, size]);

	if (!src) {
		return (
			<span
				className={cn(
					"flex aspect-square items-center justify-center rounded-full bg-[var(--leros-primary)] font-bold text-white",
					className,
				)}
			>
				{getAvatarInitial(seed)}
			</span>
		);
	}

	return (
		<img
			src={src}
			alt={alt}
			className={cn("aspect-square rounded-full object-cover", className)}
			loading="lazy"
			decoding="async"
		/>
	);
}

function getAvatarInitial(label: string) {
	const trimmed = label.trim();
	return (trimmed[0] ?? "L").toUpperCase();
}
