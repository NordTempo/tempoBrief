import { untracked, useSignalEffect } from "@preact/signals";
import { useEffect } from "preact/hooks";
import { LocationProvider, Route, Router, useLocation } from "preact-iso";
import { apiSend, sessionValid } from "./api/client";
import type { Me } from "./api/types.gen";
import { invalidate, useApi } from "./api/useApi";
import { Shell } from "./components/Shell";
import { ErrorLine, PageHead } from "./components/ui";
import { Brief } from "./views/Brief";
import { Feeds } from "./views/Feeds";
import { Learning } from "./views/Learning";
import { Login } from "./views/Login";
import { Missed } from "./views/Missed";
import { Settings } from "./views/Settings";
import { Stories } from "./views/Stories";

export function App() {
	const me = useApi<Me>("/me", 0);

	// Any 401 means the session ended (or public pages were turned off): reset
	// the flag and refetch /me, untracked so the refetch doesn't rerun this.
	useSignalEffect(() => {
		if (sessionValid.value) return;
		sessionValid.value = true;
		untracked(me.reload);
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
	const user = me.data;
	const loggedIn = () => {
		sessionValid.value = true;
		invalidate();
	};
	if (!user.authenticated && !user.publicPages) {
		return (
			<div class="mx-auto max-w-[480px] px-5 pt-[env(safe-area-inset-top)]">
				<Login onLoggedIn={loggedIn} github={user.githubLogin} />
			</div>
		);
	}
	const logOut = async () => {
		await apiSend("POST", "/logout");
		invalidate();
	};
	return (
		<LocationProvider>
			<Shell signedIn={user.authenticated} onLogOut={logOut}>
				{user.authenticated ? (
					// Keys keep the two routers apart: each caches the page for the URL.
					<Router key="reader">
						<Route path="/" component={() => <Brief signedIn />} />
						<Route
							path="/briefs/:id"
							component={({ id }: { id: string }) => <Brief id={id} signedIn />}
						/>
						<Route path="/stories" component={() => <Stories signedIn />} />
						<Route path="/learning" component={Learning} />
						<Route path="/missed" component={Missed} />
						<Route path="/feeds" component={Feeds} />
						<Route path="/settings" component={() => <Settings me={user} />} />
						<Route
							path="/settings/:area"
							component={({ area }: { area: string }) => <Settings me={user} area={area} />}
						/>
						<Route path="/login" component={GoHome} />
						<Route default component={() => <PageHead title="Not found" />} />
					</Router>
				) : (
					// Visitors read the brief and stories; any other page asks for the
					// password and then shows that page.
					<Router key="visitor">
						<Route path="/" component={() => <Brief signedIn={false} />} />
						<Route
							path="/briefs/:id"
							component={({ id }: { id: string }) => <Brief id={id} signedIn={false} />}
						/>
						<Route path="/stories" component={() => <Stories signedIn={false} />} />
						<Route
							default
							component={() => <Login onLoggedIn={loggedIn} github={user.githubLogin} />}
						/>
					</Router>
				)}
			</Shell>
		</LocationProvider>
	);
}

/** /login after logging in: back to the brief. */
function GoHome() {
	const { route } = useLocation();
	useEffect(() => route("/", true), [route]);
	return null;
}
