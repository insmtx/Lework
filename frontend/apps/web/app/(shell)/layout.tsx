import type { ReactNode } from "react";
import { LeworkShell } from "@/components/LeworkShell";

export default function ShellLayout({ children }: { children: ReactNode }) {
	return <LeworkShell>{children}</LeworkShell>;
}
