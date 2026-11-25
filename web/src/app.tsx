import { useSignalEffect } from "@preact/signals";
import { LocationProvider, Route, Router } from "preact-iso";
import { sessionValid } from "./api/client";
import type { Me } from "./api/types.gen";
import { useApi } from "./api/useApi";
import { Shell } from "./components/Shell";
import { ErrorLine, PageHead } from "./components/ui";
import { Brief } from "./views/Brief";
import { Feeds } from "./views/Feeds";
import { Learning } from "./views/Learning";
import { Login } from "./views/Login";
import { Settings } from "./views/Settings";
import { Stories } from "./views/Stories";

export function App() {
	const me = useApi<Me>("/me", 0);

	// Any 401 drops back to the login page; refetch /me to confirm.
	useSignalEffect(() => {
		if (!sessionValid.value) me.reload();
	});

	if (!me.data) {
		return (
			<div class="px-5 pt-10">
				{me.error ? (
					<ErrorLine message={me.error.message} onRetry={me.reload} />
				) : (
					<p class="text-muted">Loading</p>
				)}
			</div>
		);
	}
	if (!me.data.authenticated) {
		return (
			<Login
				onLoggedIn={() => {
					sessionValid.value = true;
					me.reload();
				}}
			/>
		);
	}
	const user = me.data;
	return (
		<LocationProvider>
			<Shell>
				<Router>
					<Route path="/" component={() => <Brief />} />
					<Route path="/briefs/:id" component={({ id }: { id: string }) => <Brief id={id} />} />
					<Route path="/stories" component={Stories} />
					<Route path="/learning" component={Learning} />
					<Route path="/feeds" component={Feeds} />
					<Route path="/settings" component={() => <Settings me={user} />} />
					<Route default component={() => <PageHead title="Not found" />} />
				</Router>
			</Shell>
		</LocationProvider>
	);
}
