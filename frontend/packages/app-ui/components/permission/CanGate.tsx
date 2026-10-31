"use client";

import type { Action, ResourceRef } from "@lework/store";
import { useCan } from "@lework/store";
import type { ReactNode } from "react";

type CanGateProps = {
	action: Action;
	resource: ResourceRef | null | undefined;
	children: ReactNode;
	fallback?: ReactNode;
	showWhileLoading?: boolean;
	ensure?: boolean;
};

export function CanGate({
	action,
	resource,
	children,
	fallback = null,
	showWhileLoading = false,
	ensure = true,
}: CanGateProps) {
	const { allowed, loading } = useCan(action, resource, ensure);

	if (loading) {
		return showWhileLoading ? children : null;
	}
	if (!allowed) {
		return <>{fallback}</>;
	}
	return <>{children}</>;
}
