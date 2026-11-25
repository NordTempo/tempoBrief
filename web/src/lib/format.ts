// Formatting helpers. Dates use Intl in the browser's locale and time zone,
// which for a self-hosted single reader is the reader's own.

/** "8", "7.6": one decimal only when the score has one. */
export function formatScore(score: number | null | undefined): string {
	if (score == null) return "";
	const rounded = Math.round(score * 10) / 10;
	return Number.isInteger(rounded) ? String(rounded) : rounded.toFixed(1);
}

/** "NRK +2" */
export function formatSources(sources: string[]): string {
	if (sources.length === 0) return "";
	const [first, ...rest] = sources;
	return rest.length ? `${first} +${rest.length}` : (first ?? "");
}

const dateFmt = new Intl.DateTimeFormat("en-GB", {
	weekday: "long",
	day: "numeric",
	month: "long",
});
const shortDateFmt = new Intl.DateTimeFormat("en-GB", { day: "numeric", month: "short" });
const timeFmt = new Intl.DateTimeFormat("en-GB", { hour: "2-digit", minute: "2-digit" });

/** "Wednesday 25 November" */
export function formatDate(iso: string): string {
	return iso ? dateFmt.format(new Date(iso)) : "";
}

/** "25 Nov" */
export function formatShortDate(iso: string): string {
	return iso ? shortDateFmt.format(new Date(iso)) : "";
}

/** "07:00" */
export function formatTime(iso: string): string {
	return iso ? timeFmt.format(new Date(iso)) : "";
}

/** "just now", "5 min ago", "3 h ago", "2 days ago", then a date. */
export function formatAgo(iso: string, now = Date.now()): string {
	if (!iso) return "never";
	const minutes = Math.round((now - new Date(iso).getTime()) / 60_000);
	if (minutes < 1) return "just now";
	if (minutes < 60) return `${minutes} min ago`;
	const hours = Math.round(minutes / 60);
	if (hours < 24) return `${hours} h ago`;
	const days = Math.round(hours / 24);
	if (days < 7) return days === 1 ? "yesterday" : `${days} days ago`;
	return formatShortDate(iso);
}

/** Plural helper: plural(3, "story", "stories") is "3 stories". */
export function plural(n: number, one: string, many = `${one}s`): string {
	return `${n} ${n === 1 ? one : many}`;
}
