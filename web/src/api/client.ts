import { signal } from "@preact/signals";
import type { ErrorBody } from "./types.gen";

/** An API error with the server's code, field messages and Retry-After. */
export class ApiError extends Error {
	constructor(
		readonly status: number,
		readonly code: string,
		message: string,
		readonly fields: Record<string, string> = {},
		readonly retryAfter?: number,
	) {
		super(message);
	}
}

/** Flips to false on any 401 so the app shows the login page. */
export const sessionValid = signal(true);

async function parse<T>(res: Response): Promise<T> {
	if (res.status === 204) return undefined as T;
	if (res.ok) return (await res.json()) as T;
	let body: ErrorBody | undefined;
	try {
		body = (await res.json()) as ErrorBody;
	} catch {
		// non-JSON error body
	}
	if (res.status === 401 && body?.error.code === "unauthorized") sessionValid.value = false;
	const retry = res.headers.get("Retry-After");
	throw new ApiError(
		res.status,
		body?.error.code ?? "http_error",
		body?.error.message ?? res.statusText,
		body?.error.fields,
		retry ? Number(retry) : undefined,
	);
}

/** GET a JSON endpoint under /api/v1. */
export async function apiGet<T>(path: string, signal?: AbortSignal): Promise<T> {
	return parse<T>(await fetch(`/api/v1${path}`, { signal, credentials: "same-origin" }));
}

/** Send JSON with POST, PUT or DELETE. */
export async function apiSend<T = void>(
	method: "POST" | "PUT" | "DELETE",
	path: string,
	body?: unknown,
): Promise<T> {
	const res = await fetch(`/api/v1${path}`, {
		method,
		credentials: "same-origin",
		headers: body === undefined ? {} : { "Content-Type": "application/json" },
		body: body === undefined ? undefined : JSON.stringify(body),
	});
	return parse<T>(res);
}
