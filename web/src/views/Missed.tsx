import { useSignal } from "@preact/signals";
import { ApiError, apiSend } from "../api/client";
import type { MissedList, MissedResponse, Story } from "../api/types.gen";
import { invalidate, useApi } from "../api/useApi";
import {
	ErrorLine,
	LinkButton,
	PageHead,
	PrimaryButton,
	SectionLabel,
	TextArea,
	TextField,
} from "../components/ui";

/**
 * "Missed?" in the header: the reader pastes a story they heard about
 * elsewhere. tempoBrief says why they did not get it, adds a feed when none
 * had it, and learns from the miss. Below, recent misses with Undo.
 */
export function Missed() {
	const url = useSignal("");
	const note = useSignal("");
	const busy = useSignal(false);
	const errors = useSignal<{ url?: string; note?: string; other?: string }>({});
	const answer = useSignal<MissedResponse | null>(null);
	const list = useApi<MissedList>("/missed", 0);

	const submit = async (e: Event) => {
		e.preventDefault();
		busy.value = true;
		errors.value = {};
		answer.value = null;
		try {
			answer.value = await apiSend<MissedResponse>("POST", "/missed", {
				url: url.value.trim(),
				note: note.value.trim(),
			});
			url.value = "";
			note.value = "";
			invalidate();
		} catch (err) {
			errors.value =
				err instanceof ApiError
					? { url: err.fields.url, note: err.fields.note, other: err.fields.url ? "" : err.message }
					: { other: "Could not reach tempoBrief." };
		} finally {
			busy.value = false;
		}
	};

	return (
		<>
			<PageHead title="Missed something?">
				<p>Paste the link. tempoBrief says why you didn't get it and learns from it.</p>
			</PageHead>
			<form class="mt-6 flex flex-col gap-4" onSubmit={submit}>
				<TextField
					label="Link to the article"
					value={url.value}
					onInput={(v) => {
						url.value = v;
					}}
					inputMode="url"
					placeholder="https://"
					error={errors.value.url}
				/>
				<TextArea
					label="Why should it have reached you? (optional)"
					rows={2}
					value={note.value}
					onInput={(v) => {
						note.value = v;
					}}
				/>
				{errors.value.note && <p class="-mt-2 text-label text-bad">{errors.value.note}</p>}
				<div>
					<PrimaryButton type="submit" disabled={busy.value || url.value.trim() === ""}>
						{busy.value ? "Checking" : "Tell tempoBrief"}
					</PrimaryButton>
				</div>
				{busy.value && (
					<p class="text-label text-muted">
						Looking it up. Finding a feed for a new site can take up to half a minute.
					</p>
				)}
				{errors.value.other && <ErrorLine message={errors.value.other} />}
			</form>
			{answer.value && (
				<Answer
					answer={answer.value}
					onChanged={() => {
						list.reload();
					}}
				/>
			)}
			<MissedStories list={list.data?.stories} onUndo={list.reload} />
		</>
	);
}

function Answer({ answer, onChanged }: { answer: MissedResponse; onChanged: () => void }) {
	const undone = useSignal(false);
	const feedRemoved = useSignal(false);
	const feed = answer.addedFeed;

	const undo = async () => {
		await apiSend("DELETE", `/stories/${answer.story.id}/missed`);
		if (feed && !feedRemoved.value) await apiSend("DELETE", `/feeds/${feed.id}`);
		undone.value = true;
		invalidate();
		onChanged();
	};
	const removeFeed = async () => {
		if (!feed) return;
		await apiSend("DELETE", `/feeds/${feed.id}`);
		feedRemoved.value = true;
		invalidate();
	};

	return (
		<section class="mt-8 border-line border-t pt-4">
			<a
				href={answer.story.url}
				target="_blank"
				rel="noopener"
				class="font-medium text-body-lg hover:text-accent"
			>
				{answer.story.title}
			</a>
			{answer.lines.map((l) => (
				<p key={l} class="mt-1 text-secondary">
					{l}
				</p>
			))}
			{undone.value ? (
				<p class="mt-2 text-label text-muted">Taken back.</p>
			) : (
				<p class="mt-2 text-label text-muted">
					Noted. Stories like it rank higher now, and the next policy update reads it.{" "}
					<LinkButton onClick={undo}>Undo</LinkButton>
					{feed && !feedRemoved.value && (
						<>
							{" · "}
							<LinkButton onClick={removeFeed}>Remove the feed</LinkButton>
						</>
					)}
					{feedRemoved.value && " · Feed removed."}
				</p>
			)}
		</section>
	);
}

function MissedStories({ list, onUndo }: { list?: Story[]; onUndo: () => void }) {
	if (!list || list.length === 0) return null;
	const undo = async (id: number) => {
		await apiSend("DELETE", `/stories/${id}/missed`);
		invalidate();
		onUndo();
	};
	return (
		<section>
			<SectionLabel>Your misses</SectionLabel>
			<ul>
				{list.map((s) => (
					<li key={s.id} class="flex items-start gap-3 border-line border-b py-2.5">
						<span class="w-3 shrink-0 pt-px text-center text-warn">!</span>
						<span class="min-w-0 flex-1">
							<a href={s.url} target="_blank" rel="noopener" class="hover:text-accent">
								{s.title}
							</a>
							{s.missedNote && <span class="block text-label text-muted">{s.missedNote}</span>}
						</span>
						<LinkButton class="-my-2.5 shrink-0 text-label" onClick={() => undo(s.id)}>
							Undo
						</LinkButton>
					</li>
				))}
			</ul>
		</section>
	);
}
