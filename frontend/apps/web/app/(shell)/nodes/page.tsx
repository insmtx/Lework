import { ControlPlaneProvider } from "@/components/ControlPlaneProvider";
import { NodesView } from "@/components/control-plane/nodes-view";

export default function NodesPage() {
	return (
		<ControlPlaneProvider>
			<NodesView />
		</ControlPlaneProvider>
	);
}
