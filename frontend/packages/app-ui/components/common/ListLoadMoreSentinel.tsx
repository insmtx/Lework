"use client";

import { cn } from "@leros/ui/lib/utils";
import { Loader2 } from "lucide-react";
import { useEffect, useRef } from "react";

type ListLoadMoreSentinelProps = {
	hasMore: boolean;
	loading: boolean;
	onLoadMore: () => void;
	root?: Element | null;
	className?: string;
};

export function ListLoadMoreSentinel({
	hasMore,
	loading,
	onLoadMore,
	root,
	className,
}: ListLoadMoreSentinelProps) {
	const ref = useRef<HTMLDivElement>(null);

	useEffect(() => {
		if (!hasMore || loading) return;
		const node = ref.current;
		if (!node) return;

		const observer = new IntersectionObserver(
			(entries) => {
				if (entries.some((entry) => entry.isIntersecting)) {
					onLoadMore();
				}
			},
			{ root: root ?? null, rootMargin: "160px", threshold: 0 },
		);
		observer.observe(node);
		return () => observer.disconnect();
	}, [hasMore, loading, onLoadMore, root]);

	if (!hasMore && !loading) return null;

	return (
		<div
			ref={ref}
			className={cn("flex items-center justify-center py-3", className)}
			role="status"
			aria-label={loading ? "加载更多" : "还有更多"}
		>
			{loading ? (
				<Loader2 className="size-4 animate-spin text-[var(--leros-text-subtle)]" />
			) : (
				<span className="sr-only">滚动加载更多</span>
			)}
		</div>
	);
}
