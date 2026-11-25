import { useSignal } from "@preact/signals";
import { ApiError, apiSend } from "../api/client";
import type { Feed, FeedsResponse, ImportResponse } from "../api/types.gen";
import { useApi } from "../api/useApi";
import {
	ErrorLine,
	Loading,
	PageHead,
	PrimaryButton,
	SecondaryButton,
	SectionLabel,
	TextField,
	Toggle,
} from "../components/ui";
import { formatAgo, plural } from "../lib/format";

/** Feeds grouped by topic, with health, add, OPML in and out. */
export function Feeds() {
	const data = useApi<FeedsResponse>("/feeds", 60_000);
	const open = useSignal<number | null>(null);

	if (!data.data)
		return data.error ? (
			<ErrorLine message={data.error.message} onRetry={data.reload} />
		) : (
			<Loading />
		);
	const feeds = data.data.feeds;
	const failing = feeds.filter((f) => f.status === "failing").length;
	const topics = data.data.topics;

	return (
		<>
			<PageHead title="Feeds">
				<p>
					{plural(feeds.length, "feed")}
					{failing > 0 && <span class="text-bad"> · {failing} failing</span>}. The topic is the
					section a story lands in.
				</p>
			</PageHead>
			<AddFeed topics={topics} onAdded={data.reload} />
			<Opml onImported={data.reload} />
			{topics.map((topic) => (
				<section key={topic}>
					<SectionLabel>{topic}</SectionLabel>
					{feeds
						.filter((f) => f.topic === topic)
						.map((f) => (
							<FeedRow
								key={f.id}
								feed={f}
								topics={topics}
								open={open.value === f.id}
								onToggle={() => {
									open.value = open.value === f.id ? null : f.id;
								}}
								onChanged={data.reload}
							/>
						))}
				</section>
			))}
		</>
	);
}

function statusLine(f: Feed) {
	switch (f.status) {
		case "failing":
			return <span class="text-bad">{f.lastError} · retrying with backoff</span>;
		case "new":
			return <span class="text-muted">waiting for the first fetch</span>;
		case "disabled":
			return <span class="text-faint">paused</span>;
		default:
			return (
				<span class="text-muted">
					{plural(f.itemCount, "item")} · fetched {formatAgo(f.lastFetchAt)}
				</span>
			);
	}
}

function FeedRow({
	feed,
	topics,
	open,
	onToggle,
	onChanged,
}: {
	feed: Feed;
	topics: string[];
	open: boolean;
	onToggle: () => void;
	onChanged: () => void;
}) {
	const title = useSignal(feed.title);
	const topic = useSignal(feed.topic);
	const enabled = useSignal(feed.enabled);
	const busy = useSignal(false);
	const error = useSignal<string | null>(null);

	const act = async (action: () => Promise<unknown>) => {
		busy.value = true;
		error.value = null;
		try {
			await action();
			onChanged();
		} catch (err) {
			error.value = err instanceof ApiError ? err.message : "Something went wrong.";
		} finally {
			busy.value = false;
		}
	};

	return (
		<div class="border-line border-b">
			<button
				type="button"
				class="flex min-h-14 w-full items-start gap-3 py-3 text-left"
				onClick={onToggle}
				aria-expanded={open}
			>
				<span class="min-w-0 flex-1">
					<span class={`block truncate ${feed.enabled ? "" : "text-muted"}`}>
						{feed.title || feed.url}
					</span>
					<span class="block truncate text-label">{statusLine(feed)}</span>
				</span>
				<span class="text-faint">{open ? "−" : "+"}</span>
			</button>
			{open && (
				<div class="flex flex-col gap-4 pb-5">
					<p class="break-all font-mono text-[13px] text-muted">{feed.url}</p>
					<TextField
						label="Name"
						value={title.value}
						onInput={(v) => {
							title.value = v;
						}}
					/>
					<TopicField
						value={topic.value}
						topics={topics}
						onInput={(v) => {
							topic.value = v;
						}}
					/>
					<Toggle
						label="Fetch this feed"
						checked={enabled.value}
						onChange={(v) => {
							enabled.value = v;
						}}
					/>
					<div class="flex flex-wrap gap-3">
						<PrimaryButton
							disabled={busy.value}
							onClick={() =>
								act(() =>
									apiSend("PUT", `/feeds/${feed.id}`, {
										title: title.value,
										topic: topic.value,
										enabled: enabled.value,
									}),
								)
							}
						>
							Save
						</PrimaryButton>
						<SecondaryButton
							disabled={busy.value}
							onClick={() => act(() => apiSend("POST", `/feeds/${feed.id}/fetch`))}
						>
							Fetch now
						</SecondaryButton>
						<SecondaryButton
							class="text-bad"
							disabled={busy.value}
							onClick={() => {
								if (
									confirm(`Remove ${feed.title || feed.url}? Its stories stay until they expire.`)
								) {
									act(() => apiSend("DELETE", `/feeds/${feed.id}`));
								}
							}}
						>
							Remove
						</SecondaryButton>
					</div>
					{error.value && <p class="text-bad">{error.value}</p>}
				</div>
			)}
		</div>
	);
}

/** Topic input with the existing topics as suggestions. */
function TopicField({
	value,
	topics,
	onInput,
}: {
	value: string;
	topics: string[];
	onInput: (v: string) => void;
}) {
	return (
		<div>
			<TextField
				label="Topic"
				value={value}
				onInput={onInput}
				placeholder={topics[0] ?? "Tech"}
				hint={topics.length > 0 ? `Existing: ${topics.join(", ")}` : undefined}
			/>
		</div>
	);
}

function AddFeed({ topics, onAdded }: { topics: string[]; onAdded: () => void }) {
	const url = useSignal("");
	const topic = useSignal("");
	const busy = useSignal(false);
	const error = useSignal<string | null>(null);
	const added = useSignal<Feed | null>(null);

	const submit = async (e: Event) => {
		e.preventDefault();
		busy.value = true;
		error.value = null;
		added.value = null;
		try {
			added.value = await apiSend<Feed>("POST", "/feeds", {
				url: url.value.trim(),
				title: "",
				topic: topic.value.trim(),
			});
			url.value = "";
			onAdded();
		} catch (err) {
			error.value =
				err instanceof ApiError ? (err.fields.url ?? err.message) : "Could not add the feed.";
		} finally {
			busy.value = false;
		}
	};

	return (
		<form class="mt-8 flex flex-col gap-4" onSubmit={submit}>
			<TextField
				label="Feed URL"
				value={url.value}
				onInput={(v) => {
					url.value = v;
				}}
				placeholder="https://www.nrk.no/toppsaker.rss"
				inputMode="url"
				error={error.value ?? undefined}
			/>
			<TopicField
				value={topic.value}
				topics={topics}
				onInput={(v) => {
					topic.value = v;
				}}
			/>
			<div>
				<PrimaryButton type="submit" disabled={busy.value || url.value.trim() === ""}>
					{busy.value ? "Checking" : "Add feed"}
				</PrimaryButton>
			</div>
			{added.value && (
				<p class={added.value.status === "failing" ? "text-bad" : "text-good"}>
					{added.value.status === "failing"
						? `Added, but the first fetch failed: ${added.value.lastError}`
						: `Added ${added.value.title || added.value.url}: ${plural(added.value.itemCount, "recent item")}.`}
				</p>
			)}
		</form>
	);
}

function Opml({ onImported }: { onImported: () => void }) {
	const message = useSignal<{ text: string; bad: boolean } | null>(null);

	const importFile = async (file: File) => {
		message.value = null;
		try {
			const res = await apiSend<ImportResponse>("POST", "/feeds/import", {
				opml: await file.text(),
			});
			message.value = { text: `Found ${res.found}, added ${res.added} new.`, bad: false };
			onImported();
		} catch (err) {
			message.value = {
				text: err instanceof ApiError ? err.message : "Could not import.",
				bad: true,
			};
		}
	};

	return (
		<p class="mt-4 flex flex-wrap items-center gap-x-5 text-label">
			<label class="inline-flex min-h-11 cursor-pointer items-center text-accent hover:text-ink">
				Import OPML
				<input
					type="file"
					accept=".opml,.xml,text/xml,text/x-opml"
					class="sr-only"
					onChange={(e) => {
						const file = e.currentTarget.files?.[0];
						if (file) importFile(file);
						e.currentTarget.value = "";
					}}
				/>
			</label>
			<a
				href="/api/v1/feeds/export"
				class="inline-flex min-h-11 items-center text-accent hover:text-ink"
				download
			>
				Export OPML
			</a>
			{message.value && (
				<span class={message.value.bad ? "text-bad" : "text-good"}>{message.value.text}</span>
			)}
		</p>
	);
}
