import { useSignal } from "@preact/signals";
import type { ComponentChildren } from "preact";
import { useEffect } from "preact/hooks";
import { ApiError, apiSend } from "../api/client";
import type {
	BriefSchedule,
	Channel,
	ChannelKind,
	ProviderKind,
	Settings as SettingsData,
} from "../api/settings.gen";
import type { GitHubSetup, GitHubStatus, Me, SettingsResponse, TestResult } from "../api/types.gen";
import { invalidate, useApi } from "../api/useApi";
import {
	ErrorLine,
	LinkButton,
	Loading,
	NumberField,
	PageHead,
	PrimaryButton,
	SecondaryButton,
	SectionLabel,
	SelectField,
	TextArea,
	TextField,
	Toggle,
} from "../components/ui";
import { DiscordSetup } from "./DiscordSetup";

const PROVIDERS: { value: ProviderKind; label: string; hint: string }[] = [
	{
		value: "anthropic",
		label: "Claude API",
		hint: "API key from console.anthropic.com. Default model: claude-haiku-5-5.",
	},
	{
		value: "openai",
		label: "OpenAI-compatible",
		hint: "OpenAI, OpenRouter, Ollama, LM Studio. Set the base URL for anything but OpenAI.",
	},
	{
		value: "claude-cli",
		label: "Claude Code CLI",
		hint: "Uses the logged-in `claude` on the server (the -cli image). Default model: haiku.",
	},
	{
		value: "codex-cli",
		label: "Codex CLI",
		hint: "Uses the logged-in `codex` on the server (the -cli image).",
	},
];

const CHANNEL_KINDS: { value: ChannelKind; label: string }[] = [
	{ value: "discord", label: "Discord" },
	{ value: "email", label: "Email" },
	{ value: "ntfy", label: "ntfy" },
	{ value: "webhook", label: "Webhook" },
];

const DAYS = ["Mo", "Tu", "We", "Th", "Fr", "Sa", "Su"];

/** All settings in one scrolling page; the save bar appears once something changed. */
export function Settings({ me }: { me: Me }) {
	const data = useApi<SettingsResponse>("/settings", 0);
	const draft = useSignal<SettingsData | null>(null);
	const saved = useSignal<string>("");
	const busy = useSignal(false);
	const error = useSignal<string | null>(null);

	useEffect(() => {
		if (data.data && draft.value === null) {
			draft.value = structuredClone(data.data.settings);
			saved.value = JSON.stringify(data.data.settings);
		}
	}, [data.data]);

	// Jump to #anchors once the page has rendered (links from the setup checklist).
	useEffect(() => {
		if (draft.value && location.hash) document.querySelector(location.hash)?.scrollIntoView();
	}, [draft.value === null]);

	if (!draft.value)
		return data.error ? (
			<ErrorLine message={data.error.message} onRetry={data.reload} />
		) : (
			<Loading />
		);
	const d = draft.value;
	const dirty = JSON.stringify(d) !== saved.value;

	/** Edit a copy of the draft; signals need a new object to notice. */
	const update = (fn: (s: SettingsData) => void) => {
		const next = structuredClone(d);
		fn(next);
		draft.value = next;
	};

	const save = async () => {
		busy.value = true;
		error.value = null;
		try {
			const res = await apiSend<SettingsResponse>("PUT", "/settings", d);
			draft.value = structuredClone(res.settings);
			saved.value = JSON.stringify(res.settings);
			invalidate();
		} catch (err) {
			error.value = err instanceof ApiError ? err.message : "Could not save.";
		} finally {
			busy.value = false;
		}
	};

	const res = data.data;
	return (
		<>
			<PageHead title="Settings" />

			<Section id="interests" title="Interests">
				<TextArea
					label="What do you want to read about, and what not?"
					rows={10}
					value={d.interests}
					onInput={(v) =>
						update((s) => {
							s.interests = v;
						})
					}
					hint="Write it like briefing an editor: topics, sources you trust, what to skip. Reactions refine this over time; they never override it."
				/>
				<div class="grid gap-4 sm:grid-cols-2">
					<TextField
						label="Summary language"
						value={d.language}
						onInput={(v) =>
							update((s) => {
								s.language = v;
							})
						}
						placeholder="English"
					/>
					<TextField
						label="Time zone"
						value={d.timezone}
						onInput={(v) =>
							update((s) => {
								s.timezone = v;
							})
						}
						placeholder="Europe/Oslo"
					/>
				</div>
			</Section>

			<Section id="model" title="Model">
				<SelectField
					label="Provider"
					value={d.provider.kind}
					options={PROVIDERS}
					onChange={(v) =>
						update((s) => {
							s.provider.kind = v;
						})
					}
					hint={PROVIDERS.find((p) => p.value === d.provider.kind)?.hint}
				/>
				<TextField
					label="Model"
					value={d.provider.model}
					onInput={(v) =>
						update((s) => {
							s.provider.model = v;
						})
					}
					placeholder={
						d.provider.kind === "openai" ? "required, such as gpt-5-mini or llama3.3" : "default"
					}
				/>
				{(d.provider.kind === "anthropic" || d.provider.kind === "openai") && (
					<TextField
						label="API key"
						secret
						value={d.provider.apiKey}
						onInput={(v) =>
							update((s) => {
								s.provider.apiKey = v;
							})
						}
						hint={
							d.provider.kind === "anthropic"
								? "Empty uses ANTHROPIC_API_KEY on the server."
								: "Optional for local servers."
						}
					/>
				)}
				{d.provider.kind === "openai" && (
					<TextField
						label="Base URL"
						value={d.provider.baseUrl}
						inputMode="url"
						onInput={(v) =>
							update((s) => {
								s.provider.baseUrl = v;
							})
						}
						placeholder="https://api.openai.com/v1"
						hint="Ollama: http://ollama:11434/v1 · OpenRouter: https://openrouter.ai/api/v1"
					/>
				)}
				<TestButton path="/settings/test-provider" body={{}} dirty={dirty} label="Test the model" />
			</Section>

			<Section id="briefs" title="Briefs">
				{d.briefs.map((b, i) => (
					<BriefEditor
						key={b.id}
						brief={b}
						channels={d.channels}
						onChange={(fn) =>
							update((s) => {
								const target = s.briefs[i];
								if (target) fn(target);
							})
						}
						onRemove={() =>
							update((s) => {
								s.briefs.splice(i, 1);
							})
						}
					/>
				))}
				<div>
					<SecondaryButton
						onClick={() =>
							update((s) => {
								s.briefs.push({
									id: uniqueID(
										"brief",
										s.briefs.map((x) => x.id),
									),
									name: "Evening brief",
									enabled: true,
									time: "18:00",
									days: [],
									minScore: 6,
									maxPerTopic: 6,
									channels: [],
								});
							})
						}
					>
						Add a brief
					</SecondaryButton>
				</div>
			</Section>

			<Section id="alerts" title="Breaking alerts">
				<Toggle
					label="Push big stories as they break"
					checked={d.alerts.enabled}
					onChange={(v) =>
						update((s) => {
							s.alerts.enabled = v;
						})
					}
					hint="Only stories at or above the score, covered by enough outlets, in the last 6 hours."
				/>
				{d.alerts.enabled && (
					<>
						<div class="grid grid-cols-2 gap-4">
							<NumberField
								label="Minimum score"
								min={0}
								max={10}
								value={d.alerts.minScore}
								onInput={(v) =>
									update((s) => {
										s.alerts.minScore = v;
									})
								}
							/>
							<NumberField
								label="Minimum outlets"
								min={1}
								max={20}
								value={d.alerts.minSources}
								onInput={(v) =>
									update((s) => {
										s.alerts.minSources = v;
									})
								}
							/>
						</div>
						<ChannelPicker
							channels={d.channels}
							picked={d.alerts.channels}
							onChange={(ids) =>
								update((s) => {
									s.alerts.channels = ids;
								})
							}
						/>
					</>
				)}
			</Section>

			<Section id="channels" title="Channels">
				{d.channels.length === 0 && (
					<p class="text-muted">No channels yet. Briefs still appear here in the web app.</p>
				)}
				{d.channels.map((c, i) => (
					<ChannelEditor
						key={c.id}
						channel={c}
						dirty={dirty}
						onChange={(fn) =>
							update((s) => {
								const target = s.channels[i];
								if (target) fn(target);
							})
						}
						onRemove={() =>
							update((s) => {
								s.channels.splice(i, 1);
								for (const b of s.briefs) b.channels = b.channels.filter((id) => id !== c.id);
								s.alerts.channels = s.alerts.channels.filter((id) => id !== c.id);
							})
						}
					/>
				))}
				<div class="flex flex-wrap gap-3">
					{CHANNEL_KINDS.map((k) => (
						<SecondaryButton
							key={k.value}
							onClick={() =>
								update((s) => {
									s.channels.push({
										id: uniqueID(
											k.value,
											s.channels.map((x) => x.id),
										),
										kind: k.value,
										name: k.label,
										enabled: true,
									});
								})
							}
						>
							Add {k.label}
						</SecondaryButton>
					))}
				</div>
			</Section>

			<Section id="learning" title="Learning">
				<Toggle
					label="Rewrite the editorial policy from reactions"
					checked={d.learning.policy}
					onChange={(v) =>
						update((s) => {
							s.learning.policy = v;
						})
					}
					hint="Every few days, after 10 new reactions."
				/>
				<Toggle
					label="Adjust scores by taste"
					checked={d.learning.taste}
					onChange={(v) =>
						update((s) => {
							s.learning.taste = v;
						})
					}
					hint="Up to 1.5 points, from how you reacted to similar stories."
				/>
				<NumberField
					label="Count a delivered story as ignored after (hours)"
					min={0}
					max={336}
					value={d.learning.ignoreAfterHours}
					onInput={(v) =>
						update((s) => {
							s.learning.ignoreAfterHours = v;
						})
					}
					hint="0 turns this off. Needs a public URL so opens can be seen."
				/>
			</Section>

			<Section id="public" title="Public pages">
				<Toggle
					label="Anyone can read the brief and stories"
					checked={d.publicPages}
					onChange={(v) =>
						update((s) => {
							s.publicPages = v;
						})
					}
					hint="Without your votes. Everything else still needs a login."
				/>
			</Section>

			<Section id="outputs" title="Home Assistant and feed readers">
				{res?.feedUrl ? (
					<>
						<CopyLine
							label="Latest brief as JSON (Home Assistant REST sensor)"
							value={res.briefJsonUrl}
						/>
						<CopyLine label="RSS of the best stories" value={res.feedUrl} />
						<div>
							<LinkButton
								onClick={async () => {
									if (!confirm("Old links stop working. Continue?")) return;
									await apiSend("POST", "/settings/read-token");
									data.reload();
								}}
							>
								Make new links
							</LinkButton>
						</div>
					</>
				) : (
					<p class="text-muted">
						Set TEMPOBRIEF_PUBLIC_URL on the server to get these links, and tracked links and vote
						buttons in deliveries.
					</p>
				)}
			</Section>

			<GitHubSection />

			{!me.passwordFromEnv && <PasswordSection />}

			{/* Room for the save bar, so it never covers the last field. */}
			<div class="h-24" />
			{(dirty || error.value) && (
				<div class="fixed inset-x-0 bottom-[calc(56px+env(safe-area-inset-bottom))] z-20 border-line border-t bg-bg sm:bottom-0">
					<div class="mx-auto flex max-w-[720px] flex-wrap items-center gap-3 px-5 py-3 sm:px-0">
						<PrimaryButton disabled={busy.value || !dirty} onClick={save}>
							{busy.value ? "Saving" : "Save changes"}
						</PrimaryButton>
						<SecondaryButton
							disabled={busy.value}
							onClick={() => {
								draft.value = JSON.parse(saved.value);
								error.value = null;
							}}
						>
							Discard
						</SecondaryButton>
						{error.value && (
							<p class="w-full whitespace-pre-line text-label text-bad">{error.value}</p>
						)}
					</div>
				</div>
			)}
		</>
	);
}

function Section({
	id,
	title,
	children,
}: {
	id: string;
	title: string;
	children: ComponentChildren;
}) {
	return (
		<section id={id} class="scroll-mt-4">
			<SectionLabel>{title}</SectionLabel>
			<div class="mt-4 flex flex-col gap-4">{children}</div>
		</section>
	);
}

function uniqueID(base: string, taken: string[]): string {
	if (!taken.includes(base)) return base;
	for (let n = 2; ; n++) if (!taken.includes(`${base}-${n}`)) return `${base}-${n}`;
}

function BriefEditor({
	brief,
	channels,
	onChange,
	onRemove,
}: {
	brief: BriefSchedule;
	channels: Channel[];
	onChange: (fn: (b: BriefSchedule) => void) => void;
	onRemove: () => void;
}) {
	return (
		<div class="flex flex-col gap-4 border-line border-b pb-6">
			<div class="grid grid-cols-[1fr_150px] gap-4">
				<TextField
					label="Name"
					value={brief.name}
					onInput={(v) =>
						onChange((b) => {
							b.name = v;
						})
					}
				/>
				<div>
					<label class="block text-label text-muted" for={`time-${brief.id}`}>
						Time
					</label>
					<input
						id={`time-${brief.id}`}
						type="time"
						class="mt-1.5 block w-full rounded-sm border border-line bg-surface px-3 py-2.5"
						value={brief.time}
						onInput={(e) => {
							const v = e.currentTarget.value;
							onChange((b) => {
								b.time = v;
							});
						}}
					/>
				</div>
			</div>
			<div>
				<p class="text-label text-muted">
					Days {brief.days.length === 0 && <span class="text-faint">(every day)</span>}
				</p>
				<div class="mt-1.5 grid grid-cols-7 gap-1">
					{DAYS.map((label, i) => {
						const day = i + 1;
						const on = brief.days.includes(day);
						return (
							<button
								type="button"
								key={label}
								aria-pressed={on}
								class={`h-11 rounded-sm border ${on ? "border-accent text-ink" : "border-line text-muted"}`}
								onClick={() =>
									onChange((b) => {
										b.days = on ? b.days.filter((x) => x !== day) : [...b.days, day].sort();
									})
								}
							>
								{label}
							</button>
						);
					})}
				</div>
			</div>
			<div class="grid grid-cols-2 gap-4">
				<NumberField
					label="Minimum score"
					min={0}
					max={10}
					value={brief.minScore}
					onInput={(v) =>
						onChange((b) => {
							b.minScore = v;
						})
					}
				/>
				<NumberField
					label="Stories per topic"
					min={1}
					max={50}
					value={brief.maxPerTopic}
					onInput={(v) =>
						onChange((b) => {
							b.maxPerTopic = v;
						})
					}
				/>
			</div>
			<ChannelPicker
				channels={channels}
				picked={brief.channels}
				onChange={(ids) =>
					onChange((b) => {
						b.channels = ids;
					})
				}
			/>
			<div class="flex flex-wrap gap-x-5">
				<Toggle
					label="On"
					checked={brief.enabled}
					onChange={(v) =>
						onChange((b) => {
							b.enabled = v;
						})
					}
				/>
				<LinkButton class="text-bad" onClick={onRemove}>
					Remove
				</LinkButton>
			</div>
		</div>
	);
}

/** Channel checkboxes; none checked means every enabled channel. */
function ChannelPicker({
	channels,
	picked,
	onChange,
}: {
	channels: Channel[];
	picked: string[];
	onChange: (ids: string[]) => void;
}) {
	if (channels.length === 0) return null;
	return (
		<div>
			<p class="text-label text-muted">
				Send to {picked.length === 0 && <span class="text-faint">(every channel)</span>}
			</p>
			<div class="flex flex-wrap gap-x-5">
				{channels.map((c) => (
					<Toggle
						key={c.id}
						label={c.name}
						checked={picked.includes(c.id)}
						onChange={(on) => onChange(on ? [...picked, c.id] : picked.filter((id) => id !== c.id))}
					/>
				))}
			</div>
		</div>
	);
}

function ChannelEditor({
	channel: c,
	dirty,
	onChange,
	onRemove,
}: {
	channel: Channel;
	dirty: boolean;
	onChange: (fn: (c: Channel) => void) => void;
	onRemove: () => void;
}) {
	const set = (key: keyof Channel) => (v: string) =>
		onChange((ch) => {
			(ch as unknown as Record<string, string>)[key] = v;
		});
	const kind = CHANNEL_KINDS.find((k) => k.value === c.kind)?.label ?? c.kind;
	return (
		<div class="flex flex-col gap-4 border-line border-b pb-6">
			<p class="text-label text-muted">{kind}</p>
			<TextField
				label="Name"
				value={c.name}
				onInput={(v) =>
					onChange((ch) => {
						ch.name = v;
					})
				}
			/>
			{c.kind === "discord" && <DiscordSetup channel={c} onChange={onChange} />}
			{c.kind === "email" && (
				<>
					<div class="grid grid-cols-[1fr_96px] gap-4">
						<TextField
							label="SMTP host"
							value={c.smtpHost ?? ""}
							onInput={set("smtpHost")}
							placeholder="smtp.fastmail.com"
						/>
						<NumberField
							label="Port"
							min={1}
							max={65535}
							value={c.smtpPort || 587}
							onInput={(v) =>
								onChange((ch) => {
									ch.smtpPort = v;
								})
							}
						/>
					</div>
					<TextField label="Username" value={c.smtpUsername ?? ""} onInput={set("smtpUsername")} />
					<TextField
						label="Password"
						secret
						value={c.smtpPassword ?? ""}
						onInput={set("smtpPassword")}
					/>
					<TextField
						label="From"
						value={c.emailFrom ?? ""}
						onInput={set("emailFrom")}
						inputMode="email"
						placeholder="tempoBrief <brief@example.com>"
					/>
					<TextField
						label="To"
						value={c.emailTo ?? ""}
						onInput={set("emailTo")}
						inputMode="email"
						hint="Several addresses separated by commas."
					/>
				</>
			)}
			{c.kind === "ntfy" && (
				<>
					<TextField
						label="Server"
						value={c.ntfyServer ?? ""}
						onInput={set("ntfyServer")}
						inputMode="url"
						placeholder="https://ntfy.sh"
					/>
					<TextField
						label="Topic"
						value={c.ntfyTopic ?? ""}
						onInput={set("ntfyTopic")}
						hint="Pick something hard to guess on the public server."
					/>
					<TextField
						label="Access token"
						secret
						value={c.ntfyToken ?? ""}
						onInput={set("ntfyToken")}
						hint="Optional."
					/>
				</>
			)}
			{c.kind === "webhook" && (
				<TextField
					label="URL"
					value={c.webhookUrl ?? ""}
					onInput={set("webhookUrl")}
					inputMode="url"
					hint="Receives each brief and alert as JSON."
				/>
			)}
			<div class="flex flex-wrap items-center gap-x-5">
				<Toggle
					label="On"
					checked={c.enabled}
					onChange={(v) =>
						onChange((ch) => {
							ch.enabled = v;
						})
					}
				/>
				<TestButton
					path="/settings/test-channel"
					body={{ channelId: c.id }}
					dirty={dirty}
					label="Send a test"
				/>
				<LinkButton class="text-bad" onClick={onRemove}>
					Remove
				</LinkButton>
			</div>
		</div>
	);
}

/** Runs a server-side check against saved settings. */
function TestButton({
	path,
	body,
	dirty,
	label,
}: {
	path: string;
	body: object;
	dirty: boolean;
	label: string;
}) {
	const busy = useSignal(false);
	const result = useSignal<TestResult | null>(null);
	return (
		<div>
			<LinkButton
				disabled={busy.value || dirty}
				onClick={async () => {
					busy.value = true;
					result.value = null;
					try {
						result.value = await apiSend<TestResult>("POST", path, body);
					} catch (err) {
						result.value = {
							ok: false,
							detail: err instanceof ApiError ? err.message : "Request failed.",
						};
					} finally {
						busy.value = false;
					}
				}}
			>
				{busy.value ? "Testing" : label}
			</LinkButton>
			{dirty && <span class="ml-3 text-label text-faint">Save first</span>}
			{result.value && (
				<p class={`text-label ${result.value.ok ? "text-good" : "text-bad"}`}>
					{result.value.detail}
				</p>
			)}
		</div>
	);
}

function CopyLine({ label, value }: { label: string; value: string }) {
	const copied = useSignal(false);
	return (
		<div>
			<p class="text-label text-muted">{label}</p>
			<div class="mt-1.5 flex items-center gap-3">
				<code class="min-w-0 flex-1 truncate rounded-sm border border-line bg-surface px-3 py-2.5 font-mono text-[13px]">
					{value}
				</code>
				<LinkButton
					onClick={async () => {
						await navigator.clipboard.writeText(value);
						copied.value = true;
						setTimeout(() => {
							copied.value = false;
						}, 1500);
					}}
				>
					{copied.value ? "Copied" : "Copy"}
				</LinkButton>
			</div>
		</div>
	);
}

const GITHUB_RESULT: Record<string, { text: string; bad: boolean }> = {
	ready: { text: "Ready. Log out and try Sign in with GitHub.", bad: false },
	failed: { text: "GitHub did not finish creating the app. Try again.", bad: true },
	expired: { text: "That took too long or came from another browser. Try again.", bad: true },
};

/**
 * Sign in with GitHub. "Set up" posts a prefilled GitHub App to GitHub; the
 * reader clicks Create there and comes back to ?github=ready.
 */
function GitHubSection() {
	const status = useApi<GitHubStatus>("/github", 0);
	const result = GITHUB_RESULT[new URLSearchParams(location.search).get("github") ?? ""];
	const busy = useSignal(false);
	const gh = status.data;
	if (!gh) return null;

	const setUp = async () => {
		busy.value = true;
		try {
			const res = await apiSend<GitHubSetup>("POST", "/github/setup");
			// GitHub's manifest flow wants a form POST with the manifest as a field.
			const form = document.createElement("form");
			form.method = "post";
			form.action = res.action;
			const field = document.createElement("input");
			field.type = "hidden";
			field.name = "manifest";
			field.value = res.manifest;
			form.append(field);
			document.body.append(form);
			form.submit();
		} catch {
			busy.value = false;
		}
	};

	return (
		<Section id="signin" title="Sign in with GitHub">
			{gh.enabled ? (
				<>
					<p>
						On for <span class="text-ink">@{gh.login}</span>. The password still works.
					</p>
					<p class="text-label text-muted">
						<a href={gh.appUrl} target="_blank" rel="noopener" class="text-accent hover:text-ink">
							The app on GitHub
						</a>
						{" · "}
						<LinkButton
							onClick={async () => {
								if (!confirm("Turn off Sign in with GitHub? Delete the app on GitHub too.")) return;
								await apiSend("DELETE", "/github");
								status.reload();
							}}
						>
							Turn off
						</LinkButton>
					</p>
				</>
			) : gh.available ? (
				<>
					<p class="text-secondary">
						Log in with your GitHub account instead of the password. GitHub opens with the app
						filled in; click Create and you are back here.
					</p>
					<div>
						<SecondaryButton disabled={busy.value} onClick={setUp}>
							{busy.value ? "Opening GitHub" : "Set up with GitHub"}
						</SecondaryButton>
					</div>
				</>
			) : (
				<p class="text-muted">Needs TEMPOBRIEF_PUBLIC_URL, so GitHub can send you back.</p>
			)}
			{result && <p class={`text-label ${result.bad ? "text-bad" : "text-good"}`}>{result.text}</p>}
		</Section>
	);
}

function PasswordSection() {
	const current = useSignal("");
	const next = useSignal("");
	const message = useSignal<{ text: string; bad: boolean } | null>(null);
	return (
		<Section id="password" title="Password">
			<TextField
				label="Current password"
				secret
				autocomplete="current-password"
				value={current.value}
				onInput={(v) => {
					current.value = v;
				}}
			/>
			<TextField
				label="New password"
				secret
				autocomplete="new-password"
				value={next.value}
				onInput={(v) => {
					next.value = v;
				}}
				hint="At least 8 characters. Logs out every session."
			/>
			<div>
				<SecondaryButton
					disabled={!current.value || next.value.length < 8}
					onClick={async () => {
						try {
							await apiSend("PUT", "/password", { current: current.value, new: next.value });
							location.reload();
						} catch (err) {
							message.value = {
								text:
									err instanceof ApiError
										? (Object.values(err.fields)[0] ?? err.message)
										: "Could not change.",
								bad: true,
							};
						}
					}}
				>
					Change password
				</SecondaryButton>
				{message.value && <p class="mt-2 text-label text-bad">{message.value.text}</p>}
			</div>
		</Section>
	);
}
