import { cn } from "@lework/ui/lib/utils";
import { Layers, type LucideProps } from "lucide-react";

/** Shared icon for project entities across navigation, lists, and pickers. */
export function ProjectIcon({ className, ...props }: LucideProps) {
	return <Layers className={cn("size-4", className)} aria-hidden {...props} />;
}
