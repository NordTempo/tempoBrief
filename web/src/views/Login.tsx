import { useSignal } from "@preact/signals";
import { ApiError, apiSend } from "../api/client";
import { invalidate } from "../api/useApi";
import { PageHead, PrimaryButton, TextField } from "../components/ui";

/** Login form, in the column layout. The caller provides the page frame. */
export function Login({ onLoggedIn }: { onLoggedIn: () => void }) {
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
				<PrimaryButton type="submit" disabled={busy.value || password.value === ""}>
					Log in
				</PrimaryButton>
			</form>
		</div>
	);
}
