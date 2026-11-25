import { useSignal } from "@preact/signals";
import { apiSend } from "../api/client";
import type { Story } from "../api/types.gen";
import { formatScore, formatSources } from "../lib/format";

/**
 * One story as direction A shows it: title, summary, and a quiet meta line
 * (score, outlets, why). Votes sit in a column on the right: two 44 px
 * targets, so they work with a thumb. Tapping the chosen vote again clears it.
 * Titles open through /r/{id}, so opening a story counts as a signal.
 */
export function StoryItem({ story, showTopic }: { story: Story; showTopic?: boolean }) {
	const vote = useSignal(story.vote);
	const showSources = useSignal(false);
	const failed = useSignal(false);

	const cast = async (value: number) => {
		const next = vote.value === value ? 0 : value;
		const prev = vote.value;
		vote.value = next;
		failed.value = false;
		try {
			await apiSend("PUT", `/stories/${story.id}/vote`, { vote: next });
		} catch {
			vote.value = prev;
			failed.value = true;
		}
	};

	const taste = tasteNote(story);
	return (
		<article class="flex gap-3 border-line border-b py-4 sm:gap-6">
			<div class="min-w-0 flex-1">
				<a
					href={`/r/${story.id}`}
					target="_blank"
					rel="noopener"
					class="font-medium text-body-lg hover:text-accent"
				>
					{story.title}
				</a>
				{story.summary && <p class="mt-1 text-secondary">{story.summary}</p>}
				<p class="mt-1 text-label text-muted">
					<span class="num text-secondary">{formatScore(story.score)}</span>
					{showTopic && <> · {story.topic}</>}
					{story.sources.length > 0 && (
						<>
							{" · "}
							<button
								type="button"
								class="hover:text-ink"
								onClick={() => {
									showSources.value = !showSources.value;
								}}
								aria-expanded={showSources.value}
							>
								{formatSources(story.sources)}
							</button>
						</>
					)}
					{story.reason && <> · {story.reason}</>}
					{taste && <span class={taste.up ? "text-good" : "text-bad"}> · {taste.text}</span>}
				</p>
				{showSources.value && <p class="mt-1 text-label text-faint">{story.sources.join(", ")}</p>}
				{failed.value && <p class="mt-1 text-label text-bad">Vote not saved. Try again.</p>}
			</div>
			<div class="-mt-1 flex shrink-0 flex-col">
				<VoteButton
					label="More like this"
					active={vote.value === 1}
					tone="good"
					onClick={() => cast(1)}
				>
					▲
				</VoteButton>
				<VoteButton
					label="Less like this"
					active={vote.value === -1}
					tone="bad"
					onClick={() => cast(-1)}
				>
					▼
				</VoteButton>
			</div>
		</article>
	);
}

function VoteButton({
	label,
	active,
	tone,
	onClick,
	children,
}: {
	label: string;
	active: boolean;
	tone: "good" | "bad";
	onClick: () => void;
	children: string;
}) {
	const color = active
		? tone === "good"
			? "text-good"
			: "text-bad"
		: "text-faint hover:text-secondary";
	return (
		<button
			type="button"
			aria-label={label}
			aria-pressed={active}
			onClick={onClick}
			class={`flex size-11 items-center justify-center text-[13px] ${color}`}
		>
			{children}
		</button>
	);
}

/** The taste part of "why", when taste moved the score by a quarter point or more. */
function tasteNote(story: Story): { text: string; up: boolean } | null {
	if (story.taste == null || story.tasteN === 0 || story.score == null || story.llmScore == null)
		return null;
	const delta = story.score - story.llmScore;
	if (Math.abs(delta) < 0.25) return null;
	return delta > 0
		? { text: `like ${story.tasteN} you liked`, up: true }
		: { text: `like ${story.tasteN} you passed on`, up: false };
}
