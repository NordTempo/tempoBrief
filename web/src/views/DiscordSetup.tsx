import { useSignal } from "@preact/signals";
import type { ComponentChildren } from "preact";
import { useEffect, useId, useRef } from "preact/hooks";
import { ApiError, apiSend } from "../api/client";
import type { Channel } from "../api/settings.gen";
import type { DiscordBot } from "../api/types.gen";
import { inputClass, LinkButton, PrimaryButton, TextField, Toggle } from "../components/ui";

const PORTAL = "https://discord.com/developers/applications";

/**
 * Discord in four steps, each opening when the one before is done: create the
 * bot on Discord, paste its token, add it to a server, pick the channel. The
 * token is enough for tempoBrief to find the rest, so nobody copies an ID.
 * Discord has no way to prefill a new application, so step 1 is a link and
 * three lines of instructions.
 */
export function DiscordSetup({
	channel: c,
	onChange,
}: {
	channel: Channel;
	onChange: (fn: (c: Channel) => void) => void;
}) {
	const opened = useSignal(false);
	const bot = useSignal<DiscordBot | null>(null);
	const checking = useSignal(false);
	const error = useSignal("");
	const token = c.discordToken ?? "";

	const check = async () => {
		checking.value = true;
		error.value = "";
		try {
			const b = await apiSend<DiscordBot>("POST", "/discord/check", {
				token,
				channelId: c.id,
			});
			bot.value = b;
			// A new setup: whoever made the bot is the one who votes.
			if (!c.discordChannelId && !c.discordOwnerId) {
				onChange((ch) => {
					ch.discordOwnerId = b.ownerId;
				});
			}
		} catch (err) {
			bot.value = null;
			error.value =
				err instanceof ApiError ? (err.fields.token ?? err.message) : "Could not reach Discord.";
		} finally {
			checking.value = false;
		}
	};

	// A saved channel checks its token on its own, and again when the reader
	// comes back from Discord's invite page with the bot in a new server. The
	// ref keeps the listener on the latest token.
	const latest = useRef(check);
	latest.current = check;
	useEffect(() => {
		if (token) latest.current();
		const again = () => {
			if (bot.value) latest.current();
		};
		window.addEventListener("focus", again);
		return () => window.removeEventListener("focus", again);
	}, [c.id]);

	const b = bot.value;
	const step = !(opened.value || token) ? 1 : !b ? 2 : b.channels.length === 0 ? 3 : 4;

	return (
		<ol>
			<Step
				n={1}
				done={step > 1}
				title={step > 1 ? "Bot created" : "Create the bot"}
				open={step === 1}
			>
				<p class="text-secondary">Discord can't prefill this, so it takes three steps there:</p>
				<ol class="mt-1 list-decimal pl-5 text-secondary">
					<li>New Application, name it tempoBrief</li>
					<li>
						Open <b class="font-medium text-ink">Bot</b>
					</li>
					<li>
						<b class="font-medium text-ink">Reset Token</b>, then copy it
					</li>
				</ol>
				<PrimaryButton
					class="mt-3"
					onClick={() => {
						window.open(PORTAL, "_blank", "noopener");
						opened.value = true;
					}}
				>
					Open Discord
				</PrimaryButton>
			</Step>

			<Step
				n={2}
				done={step > 2}
				title={b ? `Token works: ${b.name}` : "Paste its token"}
				open={step >= 2}
				reached={step >= 2}
			>
				<TextField
					label="Bot token"
					secret
					value={token}
					onInput={(v) =>
						onChange((ch) => {
							ch.discordToken = v;
						})
					}
					error={error.value}
				/>
				<div class="mt-2 flex items-center gap-4">
					<LinkButton disabled={checking.value || !token} onClick={check}>
						{checking.value ? "Checking" : b ? "Check again" : "Check"}
					</LinkButton>
					{b && <span class="text-label text-muted">Made by {b.ownerName}.</span>}
				</div>
			</Step>

			<Step
				n={3}
				done={step > 3}
				title={step > 3 ? "Added to a server" : "Add it to your server"}
				open={step === 3}
				reached={step >= 3}
			>
				<p class="text-secondary">
					Opens Discord's invite page with the two permissions it needs. Come back here after.
				</p>
				<PrimaryButton
					class="mt-3"
					onClick={() => {
						if (b) window.open(b.inviteUrl, "_blank", "noopener");
					}}
				>
					Add to server
				</PrimaryButton>
			</Step>

			<Step
				n={4}
				done={!!c.discordChannelId}
				title="Pick the channel"
				open={step === 4}
				reached={step >= 4}
			>
				{b && (
					<ChannelSelect
						bot={b}
						value={c.discordChannelId ?? ""}
						onPick={(id) =>
							onChange((ch) => {
								ch.discordChannelId = id;
							})
						}
					/>
				)}
				<p class="mt-1.5 text-label text-faint">
					Not listed? Add the bot to that server, then come back.{" "}
					{b && (
						<a href={b.inviteUrl} target="_blank" rel="noopener" class="text-accent">
							Invite again
						</a>
					)}
				</p>
				{b && (
					<Toggle
						label={`Only ${b.ownerName} can vote`}
						checked={!!c.discordOwnerId}
						onChange={(on) =>
							onChange((ch) => {
								ch.discordOwnerId = on ? b.ownerId : "";
							})
						}
						hint="Others in the channel can still read."
					/>
				)}
			</Step>
		</ol>
	);
}

function Step({
	n,
	done,
	title,
	open,
	reached = true,
	children,
}: {
	n: number;
	done: boolean;
	title: string;
	open: boolean;
	reached?: boolean;
	children: ComponentChildren;
}) {
	return (
		<li class={`flex gap-3 border-line border-b py-3.5 ${reached ? "" : "opacity-40"}`}>
			<span class={`num w-5 shrink-0 pt-px ${done ? "text-good" : "text-muted"}`}>
				{done ? (
					<svg viewBox="0 0 12 12" class="mt-1 size-3" aria-label="done" role="img">
						<path d="M1.5 6.5l3 3 6-7" fill="none" stroke="currentColor" stroke-width="1.6" />
					</svg>
				) : (
					n
				)}
			</span>
			<div class="min-w-0 flex-1">
				<p class="font-medium">{title}</p>
				{open && <div class="mt-2">{children}</div>}
			</div>
		</li>
	);
}

function ChannelSelect({
	bot,
	value,
	onPick,
}: {
	bot: DiscordBot;
	value: string;
	onPick: (id: string) => void;
}) {
	const id = useId();
	const guilds = [...new Set(bot.channels.map((ch) => ch.guild))];
	return (
		<>
			<label for={id} class="block text-label text-muted">
				Channel
			</label>
			<select
				id={id}
				class={inputClass}
				value={value}
				onChange={(e) => onPick(e.currentTarget.value)}
			>
				{!value && <option value="">Pick a channel</option>}
				{guilds.map((g) => (
					<optgroup key={g} label={g}>
						{bot.channels
							.filter((ch) => ch.guild === g)
							.map((ch) => (
								<option key={ch.id} value={ch.id}>
									#{ch.name}
								</option>
							))}
					</optgroup>
				))}
			</select>
		</>
	);
}
