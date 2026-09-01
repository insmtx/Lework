"use client";

import {
	appendProjectsFromListResult,
	fetchProjectListPage,
	mergeProjectsFromListResult,
	PROJECT_LIST_PAGE_SIZE,
	type Project,
	useLayoutStore,
} from "@leros/store";
import { useCallback, useEffect, useMemo, useState } from "react";

export function usePaginatedProjectList(options: {
	enabled: boolean;
	keyword?: string;
	debounceMs?: number;
	pageSize?: number;
}) {
	const { enabled, keyword = "", debounceMs = 300, pageSize = PROJECT_LIST_PAGE_SIZE } = options;
	const upsertProjects = useLayoutStore((state) => state.upsertProjects);
	const mutationEpoch = useLayoutStore((state) => state.projectsMutationEpoch);
	const projectCache = useLayoutStore((state) => state.projects);

	const [debouncedKeyword, setDebouncedKeyword] = useState(keyword.trim());
	const [items, setItems] = useState<Project[]>([]);
	const [total, setTotal] = useState(0);
	const [nextOffset, setNextOffset] = useState(0);
	const [hasMore, setHasMore] = useState(false);
	const [loading, setLoading] = useState(false);
	const [loadingMore, setLoadingMore] = useState(false);

	useEffect(() => {
		if (debounceMs <= 0) {
			setDebouncedKeyword(keyword.trim());
			return;
		}
		const timer = window.setTimeout(() => {
			setDebouncedKeyword(keyword.trim());
		}, debounceMs);
		return () => window.clearTimeout(timer);
	}, [debounceMs, keyword]);

	useEffect(() => {
		if (!enabled) {
			setItems([]);
			setTotal(0);
			setNextOffset(0);
			setHasMore(false);
			setLoading(false);
			setLoadingMore(false);
			return;
		}

		let cancelled = false;
		setLoading(true);
		void fetchProjectListPage({
			keyword: debouncedKeyword || undefined,
			offset: 0,
			limit: pageSize,
		})
			.then((page) => {
				if (cancelled) return;
				upsertProjects(page.items);
				setItems(page.items);
				setTotal(page.total);
				setNextOffset(page.items.length);
				setHasMore(page.hasMore);
			})
			.catch((error) => {
				if (cancelled) return;
				console.error("usePaginatedProjectList error:", error);
				setItems([]);
				setTotal(0);
				setNextOffset(0);
				setHasMore(false);
			})
			.finally(() => {
				if (!cancelled) setLoading(false);
			});

		return () => {
			cancelled = true;
		};
	}, [debouncedKeyword, enabled, mutationEpoch, pageSize, upsertProjects]);

	const loadMore = useCallback(() => {
		if (!enabled || loading || loadingMore || !hasMore) return;
		setLoadingMore(true);
		void fetchProjectListPage({
			keyword: debouncedKeyword || undefined,
			offset: nextOffset,
			limit: pageSize,
		})
			.then((page) => {
				upsertProjects(page.items);
				setItems((current) => appendProjectsFromListResult(page.items, current));
				setNextOffset((current) => current + page.items.length);
				setHasMore(page.hasMore);
				setTotal(page.total);
			})
			.catch((error) => {
				console.error("usePaginatedProjectList load more error:", error);
			})
			.finally(() => {
				setLoadingMore(false);
			});
	}, [
		debouncedKeyword,
		enabled,
		hasMore,
		loading,
		loadingMore,
		nextOffset,
		pageSize,
		upsertProjects,
	]);

	const projects = useMemo(
		() => mergeProjectsFromListResult(items, projectCache),
		[items, projectCache],
	);

	return {
		projects,
		total,
		hasMore,
		loading,
		loadingMore,
		loadMore,
	};
}
