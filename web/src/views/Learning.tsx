import { useSignal } from "@preact/signals";
import { ApiError, apiSend } from "../api/client";
import type { Learning as LearningData, Policy } from "../api/types.gen";
import { useApi } from "../api/useApi";
import {
	ErrorLine,
	LinkButton,
	Loading,
	PageHead,
	PrimaryButton,
	SecondaryButton,
	SectionLabel,
} from "../components/ui";
import { formatAgo, formatShortDate, plural } from "../lib/format";

/** Policy text is a bulleted list; one rule per non-empty line. */
function rules(text: string): string[] {
	return text
		.split("\n")
		.map((l) => l.replace(/^\s*[-*•]\s*/, "").trim())
		.filter(Boolean);
}

const KIND = {
	liked: { mark: "▲", class: "text-good", label: "liked" },
	disliked: { mark: "▼", class: "text-bad", label: "passed on" },
	clicked: { mark: "↗", class: "text-secondary", label: "opened" },
	ignored: { mark: "·", class: "text-faint", label: "ignored" },
} as const;

/** What it learned: the editorial policy, what changed, history, reactions. */
export function Learning() {
	const data = useApi<LearningData>("/learning", 60_000);
	const editing = useSignal<string | null>(null);
	const busy = useSignal(false);
	const error = useSignal<string | null>(null);

	if (!data.data)
		return data.error ? (
			<ErrorLine message={data.error.message} onRetry={data.reload} />
		) : (
			<Loading />
		);
	const l = data.data;
	const cur = l.current;

	const run = async (action: () => Promise<unknown>) => {
		busy.value = true;
		error.value = null;
		try {
			await action();
			editing.value = null;
			data.reload();
		} catch (err) {
			error.value = err instanceof ApiError ? err.message : "Something went wrong.";
		} finally {
			busy.value = false;
		}
	};
	const relearn = () => run(() => apiSend<Policy>("POST", "/policy/learn"));
	const save = () => run(() => apiSend<Policy>("PUT", "/policy", { text: editing.value ?? "" }));
	const restore = (id: number) => run(() => apiSend<Policy>("POST", `/policy/${id}/restore`));

	const c = l.counts;
	const toNext = Math.max(0, l.learnAfter - l.sinceLastLearn);

	return (
		<>
			<PageHead
				context={
					cur
						? `Version ${l.history.length} · ${cur.source === "edited" ? "edited" : "learned"} ${formatAgo(cur.createdAt)}${cur.signals ? ` from ${plural(cur.signals, "reaction")}` : ""}`
						: "No policy yet"
				}
				title="What it learned"
			>
				<p>The scorer follows these rules on top of your interests. Edit any line, or roll back.</p>
			</PageHead>

			{editing.value !== null ? (
				<div class="mt-6">
					<textarea
						aria-label="Policy"
						rows={Math.max(10, rules(editing.value).length + 3)}
						class="block w-full rounded-sm border border-line bg-surface p-3 font-mono text-[14px] leading-relaxed"
						value={editing.value}
						onInput={(e) => {
							editing.value = e.currentTarget.value;
						}}
					/>
					<div class="mt-3 flex gap-3">
						<PrimaryButton disabled={busy.value} onClick={save}>
							Save as version {l.history.length + 1}
						</PrimaryButton>
						<SecondaryButton
							onClick={() => {
								editing.value = null;
							}}
						>
							Cancel
						</SecondaryButton>
					</div>
				</div>
			) : cur ? (
				<ol class="mt-6 list-decimal pl-6 text-secondary marker:text-faint">
					{rules(cur.text).map((r) => (
						<li key={r} class="border-line border-b py-2 pl-1">
							{r}
						</li>
					))}
				</ol>
			) : (
				<p class="mt-6 text-secondary">
					Vote on stories in the brief. After {plural(l.learnAfter, "reaction")}, tempoBrief writes
					its first set of rules from them.
				</p>
			)}

			{editing.value === null && (
				<p class="mt-2 flex flex-wrap gap-x-5 text-label">
					{cur && (
						<LinkButton
							onClick={() => {
								editing.value = cur.text;
							}}
						>
							Edit
						</LinkButton>
					)}
					<LinkButton
						disabled={
							busy.value ||
							l.counts.liked + l.counts.disliked + l.counts.clicked + l.counts.ignored === 0
						}
						onClick={relearn}
					>
						{busy.value ? "Learning" : "Relearn now"}
					</LinkButton>
				</p>
			)}
			{error.value && <ErrorLine message={error.value} />}

			{cur && cur.changes.length > 0 && (
				<section>
					<SectionLabel>Changed {formatAgo(cur.createdAt)}</SectionLabel>
					<ul class="mt-2 list-disc pl-5 text-secondary marker:text-faint">
						{cur.changes.map((ch) => (
							<li key={ch} class="py-1">
								{ch}
							</li>
						))}
					</ul>
				</section>
			)}

			<p class="mt-6 text-label text-muted">
				{c.liked} liked · {c.disliked} passed on · {c.clicked} opened · {c.ignored} ignored, last 60
				days. {toNext > 0 ? `Next rewrite after ${toNext} more.` : "Rewrites at the next check."}
			</p>

			{l.recent.length > 0 && (
				<section>
					<SectionLabel>Recent reactions</SectionLabel>
					<ul>
						{l.recent.slice(0, 15).map((r) => {
							const k = KIND[r.kind as keyof typeof KIND] ?? KIND.ignored;
							return (
								<li key={`${r.storyId}-${r.at}`} class="flex gap-3 border-line border-b py-2.5">
									<span class={`w-4 shrink-0 text-center ${k.class}`} title={k.label}>
										{k.mark}
									</span>
									<span class="min-w-0 flex-1">{r.title}</span>
									<span class="shrink-0 text-label text-faint">{r.topic}</span>
								</li>
							);
						})}
					</ul>
				</section>
			)}

			{l.history.length > 1 && (
				<section>
					<SectionLabel>History</SectionLabel>
					<ul>
						{l.history.map((p, i) => (
							<li key={p.id} class="border-line border-b py-3">
								<div class="flex items-baseline justify-between gap-4">
									<span>
										Version {l.history.length - i}{" "}
										<span class="text-label text-muted">
											· {formatShortDate(p.createdAt)} ·{" "}
											{p.source === "edited"
												? "edited"
												: `learned from ${plural(p.signals, "reaction")}`}
										</span>
									</span>
									{i === 0 ? (
										<span class="text-label text-muted">current</span>
									) : (
										<LinkButton
											class="text-label"
											disabled={busy.value}
											onClick={() => restore(p.id)}
										>
											Restore
										</LinkButton>
									)}
								</div>
								{p.changes.length > 0 && (
									<p class="mt-1 text-label text-muted">{p.changes.slice(0, 2).join(" ")}</p>
								)}
							</li>
						))}
					</ul>
				</section>
			)}
		</>
	);
}
