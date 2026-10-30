export const discoveryScanChannel = "leros-desktop:discovery-scan";
export interface DiscoveredServer {
	id: string;
	name: string;
	url: string;
	conflict?: boolean;
}
export interface DiscoveryResult {
	servers: DiscoveredServer[];
	error?: string;
}
export interface DiscoveryApi {
	scan: () => Promise<DiscoveryResult>;
}
