import { ThemeProvider } from "@leros/ui/components/common/theme-provider";
import { Toaster } from "@leros/ui/components/ui/sonner";
import { HashRouter } from "react-router-dom";
import { AppRoutes } from "./routes";

export default function App() {
	return (
		<HashRouter>
			<ThemeProvider defaultTheme="system">
				<AppRoutes />
				<Toaster />
			</ThemeProvider>
		</HashRouter>
	);
}
