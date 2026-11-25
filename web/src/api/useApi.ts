import { type Signal, signal } from "@preact/signals";
import { useEffect } from "preact/hooks";
import { ApiError, apiGet } from "./client";

export interface ApiState<T> {
	data?: T;
	error?: ApiError;
	loading: boolean;
}

const cache = new Map<string, Signal<ApiState<unknown>>>();
/** Bumped by invalidate(); every mounted useApi refetches when it changes. */
const generation = signal(0);

function entry<T>(path: string): Signal<ApiState<T>> {
	let s = cache.get(path);
	if (!s) {
		s = signal<ApiState<unknown>>({ loading: true });
		cache.set(path, s);
	}
	return s as Signal<ApiState<T>>;
}

async function load<T>(path: string, s: Signal<ApiState<T>>, abort: AbortSignal) {
	s.value = { ...s.value, loading: true };
	try {
		const data = await apiGet<T>(path, abort);
		s.value = { data, loading: false };
	} catch (err) {
		if (abort.aborted) return;
		const error =
			err instanceof ApiError ? err : new ApiError(0, "network", "cannot reach tempoBrief");
		// Keep the previous data on screen; the view shows the error line beside it.
		s.value = { data: s.value.data, error, loading: false };
	}
}

/**
 * Cached GET of an API path. Keeps previous data while refetching, refetches
 * every `refreshMs` while the tab is visible, and aborts on unmount or path change.
 * Pass null to skip.
 */
export function useApi<T>(
	path: string | null,
	refreshMs = 30_000,
): ApiState<T> & { reload: () => void } {
	const s = path ? entry<T>(path) : null;
	const gen = generation.value;

	useEffect(() => {
		if (!path || !s) return;
		const abort = new AbortController();
		load(path, s, abort.signal);
		const tick = () => {
			if (!document.hidden) load(path, s, abort.signal);
		};
		const timer = refreshMs > 0 ? setInterval(tick, refreshMs) : undefined;
		const onVisible = () => {
			if (!document.hidden) tick();
		};
		document.addEventListener("visibilitychange", onVisible);
		return () => {
			abort.abort();
			clearInterval(timer);
			document.removeEventListener("visibilitychange", onVisible);
		};
	}, [path, refreshMs, gen]);

	const state = s?.value ?? { loading: false };
	return {
		...state,
		reload: () => {
			if (path && s) load(path, s, new AbortController().signal);
		},
	};
}

/**
 * Refetches every mounted view, for example after a login or a settings
 * change, keeping their data on screen meanwhile. Views mount with a fetch
 * anyway, so nothing else needs clearing.
 */
export function invalidate() {
	generation.value++;
}
