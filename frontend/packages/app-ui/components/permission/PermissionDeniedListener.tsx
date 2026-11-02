"use client";

import { PERMISSION_DENIED_EVENT } from "@lework/store";
import { useEffect } from "react";
import { toast } from "sonner";

export function PermissionDeniedListener() {
	useEffect(() => {
		const handleDenied = (event: Event) => {
			const detail = (event as CustomEvent<{ message?: string }>).detail;
			toast.error(detail?.message || "权限不足");
		};
		window.addEventListener(PERMISSION_DENIED_EVENT, handleDenied);
		return () => window.removeEventListener(PERMISSION_DENIED_EVENT, handleDenied);
	}, []);

	return null;
}
