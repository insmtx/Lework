export const discoveryScanChannel = "leros-desktop:discovery-scan";
export const discoveryResetChannel = "leros-desktop:discovery-reset";
export interface DiscoveryCandidate {
	url: string;
	transport: "link-local" | "local-network" | "hostname";
}
export interface DiscoveredServer {
	id: string;
	name: string;
	url: string;
	conflict?: boolean;
	candidates?: DiscoveryCandidate[];
}
export interface DiscoveryResult {
	servers: DiscoveredServer[];
	error?: string;
}
export interface DiscoveryApi {
	scan: () => Promise<DiscoveryResult>;
	subscribeReset?: (listener: () => void) => () => void;
}
