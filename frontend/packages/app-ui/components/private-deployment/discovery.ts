import type { ReactNode } from "react";

export type RenderServerDiscovery = (
	onSelect: (url: string) => void | Promise<void>,
	disabled: boolean,
) => ReactNode;
