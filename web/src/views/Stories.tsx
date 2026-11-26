import { type Signal, signal, useSignal, useSignalEffect } from "@preact/signals";
import { useMemo } from "preact/hooks";
import type { StoriesResponse, Story } from "../api/types.gen";
import { useApi } from "../api/useApi";
import { StoryItem } from "../components/StoryItem";
import { ErrorLine, Loading, PageHead, SectionLabel } from "../components/ui";
import { plural } from "../lib/format";

const RANGES = [
	{ hours: 24, label: "24 h" },
	{ hours: 72, label: "3 days" },
	{ hours: 168, label: "7 days" },
];
const BARS = [
	{ min: 0, label: "All" },
	{ min: 5, label: "5+" },
	{ min: 7, label: "7+" },
];
const SORTS = [
	{ key: "best", label: "Best" },
	{ key: "newest", label: "Newest" },
	{ key: "outlets", label: "Most outlets" },
] as const;
type Sort = (typeof SORTS)[number]["key"];

/**
 * Everything scored, for catching up beyond the brief. The range and score
 * bar pick what is in the list; sort and grouping only order it. Sort and
 * grouping are remembered on the device.
 */
export function Stories({ signedIn }: { signedIn: boolean }) {
	const hours = useSignal(24);
	const min = useSignal(5);
	const topic = useSignal("");
	const query = useSignal("");
	const voted = useSignal(false);
	const sort = useStored("stories.sort", "best", (v): v is Sort => SORTS.some((s) => s.key === v));
	const grouped = useStored("stories.byTopic", false, (v) => typeof v === "boolean");

	const params = new URLSearchParams({
		hours: String(hours.value),
		min: String(min.value),
		sort: sort.value,
	});
	if (topic.value) params.set("topic", topic.value);
	if (query.value.trim()) params.set("q", query.value.trim());
	if (voted.value) params.set("voted", "true");
	const data = useApi<StoriesResponse>(`/stories?${params}`, 120_000);
	const stories = data.data?.stories ?? [];

	return (
		<>
			<PageHead title="Stories">
				<p>Everything scored.{signedIn && " Votes here teach it too."}</p>
			</PageHead>
			<div class="mt-6 flex flex-col gap-3">
				<input
					type="search"
					placeholder="Search titles and summaries"
					class="w-full rounded-sm border border-line bg-surface px-3 py-2.5 placeholder:text-faint"
					value={query.value}
					onInput={(e) => {
						query.value = e.currentTarget.value;
					}}
				/>
				<div class="flex flex-wrap items-center gap-x-5 gap-y-1 text-label">
					<Choice
						options={RANGES.map((r) => ({ key: r.hours, label: r.label }))}
						value={hours.value}
						onPick={(v) => {
							hours.value = v;
						}}
					/>
					<Choice
						options={BARS.map((b) => ({ key: b.min, label: b.label }))}
						value={min.value}
						onPick={(v) => {
							min.value = v;
						}}
					/>
					<select
						aria-label="Topic"
						class="min-h-11 bg-transparent text-muted"
						value={topic.value}
						onChange={(e) => {
							topic.value = e.currentTarget.value;
						}}
					>
						<option value="">All topics</option>
						{(data.data?.topics ?? []).map((t) => (
							<option key={t} value={t}>
								{t}
							</option>
						))}
					</select>
					{signedIn && (
						<button
							type="button"
							class={`min-h-11 ${voted.value ? "text-ink" : "text-muted"}`}
							aria-pressed={voted.value}
							onClick={() => {
								voted.value = !voted.value;
							}}
						>
							Voted
						</button>
					)}
				</div>
				<div class="-mt-3 flex items-center justify-between gap-5 text-label">
					<Choice
						options={SORTS}
						value={sort.value}
						onPick={(v) => {
							sort.value = v;
						}}
					/>
					<Switch
						label="By topic"
						on={grouped.value}
						onToggle={() => {
							grouped.value = !grouped.value;
						}}
					/>
				</div>
			</div>
			{data.error && <ErrorLine message={data.error.message} onRetry={data.reload} />}
			{!data.data && !data.error && <Loading />}
			{data.data && (
				<>
					<p
						class={`mt-4 pb-2 text-label text-muted ${grouped.value ? "" : "border-line border-b"}`}
					>
						{plural(stories.length, "story", "stories")}
					</p>
					{grouped.value
						? byTopic(stories).map(([t, list]) => (
								<section key={t}>
									<SectionLabel>{t}</SectionLabel>
									{list.map((s) => (
										<StoryItem key={s.id} story={s} canVote={signedIn} />
									))}
								</section>
							))
						: stories.map((s) => <StoryItem key={s.id} story={s} showTopic canVote={signedIn} />)}
				</>
			)}
		</>
	);
}

/** Stories per topic, keeping the list's order. A topic sits where its first story did. */
function byTopic(stories: Story[]): [string, Story[]][] {
	const groups = new Map<string, Story[]>();
	for (const s of stories) {
		const list = groups.get(s.topic);
		if (list) list.push(s);
		else groups.set(s.topic, [s]);
	}
	return [...groups];
}

/** A signal kept in localStorage. Stored values `valid` rejects fall back. */
function useStored<T>(key: string, fallback: T, valid: (v: unknown) => v is T): Signal<T> {
	const value = useMemo(() => {
		try {
			const raw = localStorage.getItem(key);
			const parsed: unknown = raw == null ? null : JSON.parse(raw);
			return signal(valid(parsed) ? parsed : fallback);
		} catch {
			return signal(fallback); // private mode or a bad value
		}
	}, [key]);
	useSignalEffect(() => {
		try {
			localStorage.setItem(key, JSON.stringify(value.value));
		} catch {
			// Not saved; the choice still holds for this visit.
		}
	});
	return value;
}

function Switch({ label, on, onToggle }: { label: string; on: boolean; onToggle: () => void }) {
	return (
		<button
			type="button"
			role="switch"
			aria-checked={on}
			class={`flex min-h-11 items-center gap-2 ${on ? "text-ink" : "text-muted hover:text-ink"}`}
			onClick={onToggle}
		>
			{label}
			<span
				class={`relative h-3.5 w-6.5 rounded-full border ${on ? "border-accent" : "border-line-strong"}`}
			>
				<span
					class={`absolute top-0.5 size-2 rounded-full ${on ? "left-3.5 bg-accent" : "left-0.5 bg-faint"}`}
				/>
			</span>
		</button>
	);
}

function Choice<T extends string | number>({
	options,
	value,
	onPick,
}: {
	options: readonly { key: T; label: string }[];
	value: T;
	onPick: (v: T) => void;
}) {
	return (
		<span class="flex gap-3">
			{options.map((o) => (
				<button
					type="button"
					key={o.key}
					class={`min-h-11 ${o.key === value ? "text-ink shadow-[inset_0_-2px_0_var(--color-accent)]" : "text-muted hover:text-ink"}`}
					onClick={() => onPick(o.key)}
				>
					{o.label}
				</button>
			))}
		</span>
	);
}
