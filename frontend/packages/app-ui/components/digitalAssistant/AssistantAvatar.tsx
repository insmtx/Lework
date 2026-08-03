"use client";

import { cn } from "@leros/ui/lib/utils";
import { CUSTOM_ASSISTANT_DEFAULT_AVATAR_SRC } from "../../assets";
import { DiceBearAvatar } from "../avatar/DiceBearAvatar";
import { ProtectedImage } from "../avatar/ProtectedImage";

const sizeClassMap = {
	sm: "size-7 text-xs",
	default: "size-12 text-lg",
	md: "size-14 text-xl",
	lg: "size-16 text-2xl",
	xl: "size-20 text-3xl",
	"2xl": "size-24 text-4xl",
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
	const sizeClass = sizeClassMap[size];
	const pixelSize =
		size === "2xl"
			? 192
			: size === "xl"
				? 160
				: size === "lg"
					? 128
					: size === "md"
						? 112
						: size === "sm"
							? 56
							: 96;
	// 中文注释：有头像但加载失败时仍按名称生成 DiceBear，避免预设队友因 preview 失败长成同一张图。
	const loadErrorFallback = (
		<DiceBearAvatar
			seed={`digital-assistant:${name}`}
			alt={name}
			className={sizeClass}
			size={pixelSize}
		/>
	);
	// 中文注释：未上传头像时展示自定义 AI 队友固定默认图。
	const emptyFallback = (
		<img
			src={CUSTOM_ASSISTANT_DEFAULT_AVATAR_SRC}
			alt={name}
			className={cn("rounded-full object-cover", sizeClass)}
		/>
	);

	return (
		<div
			className={cn(
				"flex shrink-0 items-center justify-center overflow-hidden rounded-full bg-transparent",
				sizeClass,
				className,
			)}
		>
			{src ? (
				<ProtectedImage
					src={src}
					alt={name}
					className={cn("rounded-full object-cover", sizeClass)}
					fallback={loadErrorFallback}
					loadingFallback={
						// 中文注释：受保护头像首次加载时展示中性占位，避免短暂闪现兜底头像。
						<span aria-hidden="true" className="size-full animate-pulse bg-slate-100" />
					}
				/>
			) : (
				emptyFallback
			)}
		</div>
	);
}
