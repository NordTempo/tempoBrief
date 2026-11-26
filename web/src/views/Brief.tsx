import { useSignal } from "@preact/signals";
import { ApiError, apiSend } from "../api/client";
import type { Settings } from "../api/settings.gen";
import type {
	Brief as BriefData,
	BriefsResponse,
	SendBriefResponse,
	SettingsResponse,
	Status,
} from "../api/types.gen";
import { invalidate, useApi } from "../api/useApi";
import { StoryItem } from "../components/StoryItem";
import { ErrorLine, LinkButton, Loading, PageHead, SectionLabel } from "../components/ui";
import { formatDate, formatShortDate, formatTime, plural } from "../lib/format";
import { Setup } from "./Setup";

/**
 * The latest brief (route "/") or one earlier brief ("/briefs/:id"). Visitors
 * (signedIn false, with public pages on) see the stories without votes, setup
 * or sending.
 */
export function Brief({ id, signedIn }: { id?: string; signedIn: boolean }) {
	const path = id ? `/briefs/${id}` : "/briefs/latest";
	const brief = useApi<BriefData>(path, 60_000);
	const status = useApi<Status>(signedIn ? "/status" : null, 60_000);
	const settings = useApi<SettingsResponse>(signedIn ? "/settings" : null, 0);

	if (brief.error?.status === 404 && !id) {
		return signedIn ? (
			<NoBrief status={status.data} settings={settings.data?.settings} onSent={brief.reload} />
		) : (
			<PageHead title="No brief yet" />
		);
	}
	if (!brief.data) {
		return brief.error ? (
			<ErrorLine message={brief.error.message} onRetry={brief.reload} />
		) : (
			<Loading />
		);
	}
	const b = brief.data;
	const [name, ...rest] = b.title.split(" · ");
	const stories = b.sections.flatMap((s) => s.stories);
	const outlets = new Set(stories.flatMap((s) => s.sources)).size;

	return (
		<>
			{!id && status.data?.setup.some((k) => k !== "channel") && (
				<Setup status={status.data} compact />
			)}
			<PageHead
				context={
					<>
						{rest.join(" · ") || formatDate(b.createdAt)} · {formatTime(b.createdAt)}
						<Deliveries brief={b} />
					</>
				}
				title={name ?? b.title}
			>
				{stories.length > 0 ? (
					<p>
						{plural(stories.length, "story", "stories")} from {plural(outlets, "outlet")}.
					</p>
				) : (
					<p>Nothing cleared the bar.</p>
				)}
			</PageHead>
			{brief.error && <ErrorLine message={brief.error.message} onRetry={brief.reload} />}
			{b.sections.map((sec) => (
				<section key={sec.topic}>
					<SectionLabel>{sec.topic}</SectionLabel>
					{sec.stories.map((s) => (
						<StoryItem key={s.id} story={s} canVote={signedIn} />
					))}
				</section>
			))}
			<EarlierBriefs currentId={b.id} />
			<SendNow settings={settings.data?.settings} />
		</>
	);
}

function Deliveries({ brief }: { brief: BriefData }) {
	if (brief.deliveries.length === 0) return null;
	const failed = brief.deliveries.filter((d) => !d.ok);
	return (
		<>
			{" · sent to "}
			{brief.deliveries.map((d, i) => (
				<span key={d.channel} class={d.ok ? "" : "text-bad"} title={d.error}>
					{i > 0 && ", "}
					{d.channel}
				</span>
			))}
			{failed.length > 0 && <span class="text-bad"> ({failed.length} failed)</span>}
		</>
	);
}

function EarlierBriefs({ currentId }: { currentId: number }) {
	const list = useApi<BriefsResponse>("/briefs", 60_000);
	const others = (list.data?.briefs ?? []).filter((b) => b.id !== currentId).slice(0, 7);
	if (others.length === 0) return null;
	return (
		<section>
			<SectionLabel>Earlier briefs</SectionLabel>
			<ul>
				{others.map((b) => (
					<li key={b.id} class="border-line border-b">
						<a
							href={`/briefs/${b.id}`}
							class="flex min-h-11 items-center justify-between gap-4 py-2 hover:text-accent"
						>
							<span>{b.title.split(" · ")[0]}</span>
							<span class="num shrink-0 text-label text-muted">
								{formatShortDate(b.createdAt)} · {plural(b.storyCount, "story", "stories")}
							</span>
						</a>
					</li>
				))}
			</ul>
		</section>
	);
}

/** "Send now" for each brief schedule. */
function SendNow({ settings, onSent }: { settings?: Settings; onSent?: () => void }) {
	const busy = useSignal("");
	const message = useSignal<{ text: string; bad: boolean } | null>(null);
	if (!settings || settings.briefs.length === 0) return null;

	const send = async (scheduleId: string) => {
		busy.value = scheduleId;
		message.value = null;
		try {
			const res = await apiSend<SendBriefResponse>("POST", "/briefs/send", { scheduleId });
			invalidate();
			message.value = res.error
				? { text: `Saved, but some channels failed: ${res.error}`, bad: true }
				: { text: "Sent.", bad: false };
			onSent?.();
			location.assign(`/briefs/${res.briefId}`);
		} catch (err) {
			message.value = {
				text: err instanceof ApiError ? err.message : "Could not send.",
				bad: true,
			};
		} finally {
			busy.value = "";
		}
	};

	return (
		<p class="mt-8 text-label text-muted">
			{settings.briefs.map((b, i) => (
				<span key={b.id}>
					{i > 0 && " · "}
					<LinkButton disabled={busy.value !== ""} onClick={() => send(b.id)}>
						{busy.value === b.id ? "Sending" : `Send ${b.name.toLowerCase()} now`}
					</LinkButton>
				</span>
			))}
			{message.value && (
				<span class={`block ${message.value.bad ? "text-bad" : "text-good"}`}>
					{message.value.text}
				</span>
			)}
		</p>
	);
}

function NoBrief({
	status,
	settings,
	onSent,
}: {
	status?: Status;
	settings?: Settings;
	onSent: () => void;
}) {
	const next = settings?.briefs.find((b) => b.enabled);
	return (
		<>
			{status && status.setup.length > 0 ? (
				<Setup status={status} />
			) : (
				<PageHead title="No brief yet">
					<p>
						{next
							? `The ${next.name.toLowerCase()} goes out at ${next.time}.`
							: "Add a brief schedule in Settings."}
						{status &&
							` ${plural(status.stories, "story", "stories")} scored in the last 24 hours.`}
					</p>
				</PageHead>
			)}
			<SendNow settings={settings} onSent={onSent} />
		</>
	);
}
