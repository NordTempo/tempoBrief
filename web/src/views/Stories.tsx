import { useSignal } from "@preact/signals";
import type { StoriesResponse } from "../api/types.gen";
import { useApi } from "../api/useApi";
import { StoryItem } from "../components/StoryItem";
import { ErrorLine, Loading, PageHead } from "../components/ui";
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

/** Everything scored, best first: for catching up beyond the brief. */
export function Stories({ signedIn }: { signedIn: boolean }) {
	const hours = useSignal(24);
	const min = useSignal(5);
	const topic = useSignal("");
	const query = useSignal("");
	const voted = useSignal(false);

	const params = new URLSearchParams({ hours: String(hours.value), min: String(min.value) });
	if (topic.value) params.set("topic", topic.value);
	if (query.value.trim()) params.set("q", query.value.trim());
	if (voted.value) params.set("voted", "true");
	const data = useApi<StoriesResponse>(`/stories?${params}`, 120_000);
	const stories = data.data?.stories ?? [];

	return (
		<>
			<PageHead title="Stories">
				<p>Everything scored, best first.{signedIn && " Votes here teach it too."}</p>
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
			</div>
			{data.error && <ErrorLine message={data.error.message} onRetry={data.reload} />}
			{!data.data && !data.error && <Loading />}
			{data.data && (
				<>
					<p class="mt-4 border-line border-b pb-2 text-label text-muted">
						{plural(stories.length, "story", "stories")}
					</p>
					{stories.map((s) => (
						<StoryItem key={s.id} story={s} showTopic canVote={signedIn} />
					))}
				</>
			)}
		</>
	);
}

function Choice<T extends number>({
	options,
	value,
	onPick,
}: {
	options: { key: T; label: string }[];
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
