import type { ReactNode } from "react";

export type RenderServerDiscovery = (
	onSelect: (url: string) => void,
	disabled: boolean,
) => ReactNode;
