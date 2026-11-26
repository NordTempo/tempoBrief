import type { Status } from "../api/types.gen";
import { PageHead, SectionLabel } from "../components/ui";

const STEPS = [
	{
		key: "interests",
		label: "Say what you want to read",
		detail: "A few sentences, like briefing an editor.",
		href: "/settings/interests",
	},
	{
		key: "feeds",
		label: "Add feeds",
		detail: "Paste feed URLs or import an OPML file.",
		href: "/feeds",
	},
	{
		key: "provider",
		label: "Connect a model",
		detail: "Claude, OpenAI-compatible, or a logged-in CLI. Then press Test.",
		href: "/settings/model",
	},
	{
		key: "channel",
		label: "Choose where briefs go",
		detail: "Discord, email, ntfy or a webhook. Optional: the web app always has them.",
		href: "/settings/channels",
	},
];

/**
 * The first-run checklist, driven by /status. Full page before the first
 * brief; a short block above the brief while anything is still missing.
 */
export function Setup({ status, compact }: { status: Status; compact?: boolean }) {
	const missing = new Set(status.setup);
	const steps = STEPS.map((s) => ({ ...s, done: !missing.has(s.key) }));
	const list = (
		<ol>
			{steps.map((s, i) => (
				<li key={s.key} class="border-line border-b">
					<a href={s.href} class="flex min-h-14 items-start gap-4 py-3 hover:text-accent">
						<span class={`num w-5 shrink-0 ${s.done ? "text-good" : "text-faint"}`}>
							{s.done ? "✓" : i + 1}
						</span>
						<span>
							<span class={`block ${s.done ? "text-muted line-through" : ""}`}>{s.label}</span>
							{!s.done && <span class="block text-label text-muted">{s.detail}</span>}
						</span>
					</a>
				</li>
			))}
		</ol>
	);
	if (compact) {
		return (
			<section class="pt-4">
				<SectionLabel>Finish setting up</SectionLabel>
				{list}
			</section>
		);
	}
	return (
		<>
			<PageHead title="Set up tempoBrief">
				<p>Four steps, then the first brief goes out at its scheduled time.</p>
			</PageHead>
			<div class="mt-8">{list}</div>
		</>
	);
}
