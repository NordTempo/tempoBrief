import { useSignal } from "@preact/signals";
import { ApiError, apiSend } from "../api/client";
import { invalidate } from "../api/useApi";
import { PageHead, PrimaryButton, SecondaryButton, TextField } from "../components/ui";

const GITHUB_ERRORS: Record<string, string> = {
	denied: "That GitHub account is not the one set up for this tempoBrief.",
	expired: "That took too long. Try again.",
	failed: "GitHub sign-in failed. Try again or use the password.",
	off: "Sign in with GitHub is off.",
};

/**
 * Login form, in the column layout. The caller provides the page frame. With
 * Sign in with GitHub set up, that comes first and the password second.
 */
export function Login({ onLoggedIn, github }: { onLoggedIn: () => void; github: boolean }) {
	const githubError = GITHUB_ERRORS[new URLSearchParams(location.search).get("github") ?? ""];
	const password = useSignal("");
	const error = useSignal<string | null>(null);
	const busy = useSignal(false);

	const submit = async (e: Event) => {
		e.preventDefault();
		busy.value = true;
		error.value = null;
		try {
			await apiSend("POST", "/login", { password: password.value });
			invalidate();
			onLoggedIn();
		} catch (err) {
			if (err instanceof ApiError && err.status === 429) {
				error.value = `Too many attempts. Try again in ${err.retryAfter ?? 30} s.`;
			} else if (err instanceof ApiError && err.status === 401) {
				error.value = "Wrong password.";
			} else {
				error.value = "Cannot reach tempoBrief.";
			}
		} finally {
			busy.value = false;
		}
	};

	return (
		<div class="max-w-[480px]">
			<PageHead context="tempoBrief" title="Log in" />
			{github && (
				<div class="mt-8">
					<PrimaryButton
						class="w-full"
						onClick={() => {
							// A full page load: GitHub takes it from here.
							location.assign("/auth/github/login");
						}}
					>
						Sign in with GitHub
					</PrimaryButton>
				</div>
			)}
			{githubError && <p class="mt-3 text-label text-bad">{githubError}</p>}
			<form class="mt-8 flex flex-col gap-5" onSubmit={submit}>
				<TextField
					label="Password"
					secret
					autocomplete="current-password"
					value={password.value}
					onInput={(v) => {
						password.value = v;
					}}
					error={error.value ?? undefined}
				/>
				{github ? (
					<SecondaryButton type="submit" disabled={busy.value || password.value === ""}>
						Log in with password
					</SecondaryButton>
				) : (
					<PrimaryButton type="submit" disabled={busy.value || password.value === ""}>
						Log in
					</PrimaryButton>
				)}
			</form>
		</div>
	);
}
