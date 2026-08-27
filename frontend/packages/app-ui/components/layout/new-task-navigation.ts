import type { AppNavigation } from "./LeftRail";

export function navigateToNewTask(
	navigation: AppNavigation | undefined,
	switchView: (view: "chat") => void,
) {
	if (navigation) {
		navigation.goToRoute("chat");
		return;
	}
	switchView("chat");
}
