import { useEffect } from "react";
import { useAppStore } from "../appStore";
import type { Action, BatchCheckItem, ResourceRef } from "../permission/types";

export function useCan(action: Action, resource: ResourceRef | null | undefined, ensure = true) {
	const allowed = useAppStore((state) => state.can(action, resource));
	const ensureCapabilities = useAppStore((state) => state.ensureCapabilities);

	useEffect(() => {
		if (!ensure || !resource?.publicId) return;
		void ensureCapabilities([{ action, resource }]);
	}, [action, ensure, ensureCapabilities, resource?.publicId, resource?.type]);

	return {
		allowed: allowed === true,
		loading: allowed === "unknown",
		denied: allowed === false,
	};
}

export function useEnsureCapabilities(items: BatchCheckItem[], enabled = true) {
	const ensureCapabilities = useAppStore((state) => state.ensureCapabilities);

	useEffect(() => {
		if (!enabled || items.length === 0) return;
		void ensureCapabilities(items);
	}, [enabled, ensureCapabilities, items]);
}

export function useProjectCapabilities(projectPublicId: string | null | undefined) {
	const ensureCapabilities = useAppStore((state) => state.ensureCapabilities);

	useEffect(() => {
		if (!projectPublicId) return;
		void ensureCapabilities(
			(
				[
					"project:update",
					"project:delete",
					"project:member.create",
					"project:member.update",
					"project:member.delete",
					"project:member.leave",
					"task:create",
				] as const
			).map((action) => ({
				action,
				resource: { type: "project" as const, publicId: projectPublicId },
			})),
		);
	}, [ensureCapabilities, projectPublicId]);
}

const PROJECT_MENU_ACTIONS = ["project:update", "project:delete", "project:member.leave"] as const;

export function useProjectsMenuCapabilities(projectPublicIds: string[]) {
	const ensureCapabilities = useAppStore((state) => state.ensureCapabilities);
	const permissionRevision = useAppStore((state) => state.permissionRevision);
	const projectIdsKey = projectPublicIds.join("\u0000");

	useEffect(() => {
		if (!projectIdsKey) return;
		// 中文注释：同一页面的项目菜单权限合并为一次批量查询，避免按项目发起几十个请求。
		void ensureCapabilities(
			projectIdsKey.split("\u0000").flatMap((publicId) =>
				PROJECT_MENU_ACTIONS.map((action) => ({
					action,
					resource: { type: "project" as const, publicId },
				})),
			),
		);
	}, [ensureCapabilities, permissionRevision, projectIdsKey]);
}

export function useTaskCapabilities(taskPublicId: string | null | undefined) {
	const ensureCapabilities = useAppStore((state) => state.ensureCapabilities);

	useEffect(() => {
		if (!taskPublicId) return;
		void ensureCapabilities(
			(["task:view", "task:update", "task:delete"] as const).map((action) => ({
				action,
				resource: { type: "task" as const, publicId: taskPublicId },
			})),
		);
	}, [ensureCapabilities, taskPublicId]);
}

/** 项目「更多操作」菜单：预取权限并汇总是否有任一可执行项。 */
export function useProjectMenuCapabilities(
	projectPublicId: string | null | undefined,
	ensure = true,
) {
	const ensureCapabilities = useAppStore((state) => state.ensureCapabilities);
	const resource = projectPublicId ? { type: "project" as const, publicId: projectPublicId } : null;

	useEffect(() => {
		if (!ensure || !resource) return;
		void ensureCapabilities(PROJECT_MENU_ACTIONS.map((action) => ({ action, resource })));
	}, [ensure, ensureCapabilities, resource?.publicId, resource?.type]);

	// 中文注释：菜单权限由页面级批量请求统一加载时，这里只读取缓存结果。
	const rename = useAppStore((state) => state.can("project:update", resource));
	const del = useAppStore((state) => state.can("project:delete", resource));
	const leave = useAppStore((state) => state.can("project:member.leave", resource));

	const loading = rename === "unknown" || del === "unknown" || leave === "unknown";
	const hasAny = rename === true || del === true || leave === true;

	return {
		loading,
		hasAny,
		canRename: rename === true,
		canDelete: del === true,
		canLeave: leave === true,
	};
}
