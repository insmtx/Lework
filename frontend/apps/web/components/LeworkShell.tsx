"use client";

import type { AppNavigation } from "@lework/app-ui";
import { ConsoleShell, createConsoleNavigation } from "@lework/control-ui";
import { usePathname, useRouter } from "next/navigation";
import type { ReactNode } from "react";

export function LeworkShell({ children }: { children: ReactNode }) {
	return <ConsoleShell navigation={useWebNavigation()}>{children}</ConsoleShell>;
}

export function useWebNavigation(): AppNavigation {
	const pathname = usePathname();
	const router = useRouter();

	return createConsoleNavigation({
		pathname,
		push: (path) => router.push(path),
	});
}
