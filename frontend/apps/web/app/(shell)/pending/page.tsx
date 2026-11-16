import { ControlPlaneProvider } from "@/components/ControlPlaneProvider";
import { PendingView } from "@/components/control-plane/pending-view";

export default function PendingPage() {
	return (
		<ControlPlaneProvider>
			<PendingView />
		</ControlPlaneProvider>
	);
}
