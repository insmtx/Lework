import { ControlPlaneProvider } from "@/components/ControlPlaneProvider";
import { WorkspacesView } from "@/components/control-plane/workspaces-view";

export default function WorkspacesPage() {
	return (
		<ControlPlaneProvider>
			<WorkspacesView />
		</ControlPlaneProvider>
	);
}
