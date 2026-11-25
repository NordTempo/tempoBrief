import type { ButtonHTMLAttributes, ComponentChildren } from "preact";
import { useId } from "preact/hooks";

// Shared building blocks in direction A ("Column"): one reading column, quiet
// meta lines, uppercase topic labels over a hairline. Every tap target is at
// least 44 px tall so the app works one-handed on a phone.

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement>;

/** Filled accent button. */
export function PrimaryButton({ class: cls, ...rest }: ButtonProps) {
	return (
		<button
			type="button"
			{...rest}
			class={`h-11 rounded-sm bg-accent px-5 font-medium text-bg disabled:opacity-50 ${cls ?? ""}`}
		/>
	);
}

/** Outlined button. */
export function SecondaryButton({ class: cls, ...rest }: ButtonProps) {
	return (
		<button
			type="button"
			{...rest}
			class={`h-11 rounded-sm border border-line-strong px-4 text-ink disabled:opacity-50 ${cls ?? ""}`}
		/>
	);
}

/** Accent text button, for actions inside meta lines ("Edit", "Relearn now"). */
export function LinkButton({ class: cls, ...rest }: ButtonProps) {
	return (
		<button
			type="button"
			{...rest}
			class={`inline-flex min-h-11 items-center text-accent hover:text-ink disabled:opacity-50 ${cls ?? ""}`}
		/>
	);
}

/** The page opening: a muted context line, the big title, one secondary sentence. */
export function PageHead({
	context,
	title,
	children,
}: {
	context?: ComponentChildren;
	title: string;
	children?: ComponentChildren;
}) {
	return (
		<div class="pt-8 sm:pt-12">
			{context && <p class="text-label text-muted">{context}</p>}
			<h1 class="mt-1 font-medium text-title-sm tracking-[-0.03em] sm:text-title">{title}</h1>
			{children && <div class="mt-2 text-secondary">{children}</div>}
		</div>
	);
}

/** Uppercase topic or section label over a hairline. */
export function SectionLabel({
	children,
	action,
}: {
	children: ComponentChildren;
	action?: ComponentChildren;
}) {
	return (
		<div class="mt-10 flex items-end justify-between gap-4 border-line border-b pb-2">
			<h2 class="font-medium text-label text-muted uppercase tracking-[0.06em]">{children}</h2>
			{action && <div class="text-label">{action}</div>}
		</div>
	);
}

export function ErrorLine({ message, onRetry }: { message: string; onRetry?: () => void }) {
	return (
		<p class="mt-4 text-bad">
			{message}
			{onRetry && (
				<LinkButton class="ml-3" onClick={onRetry}>
					Try again
				</LinkButton>
			)}
		</p>
	);
}

/** Static loading line; no spinners (they repaint continuously). */
export function Loading() {
	return <p class="mt-10 text-muted">Loading</p>;
}

const inputClass =
	"mt-1.5 block w-full rounded-sm border border-line bg-surface px-3 py-2.5 text-ink placeholder:text-faint focus:border-line-strong";

type TextFieldProps = {
	label: string;
	value: string;
	onInput: (value: string) => void;
	hint?: ComponentChildren;
	error?: string;
	placeholder?: string;
	secret?: boolean;
	autocomplete?: string;
	inputMode?: "text" | "url" | "email" | "numeric";
};

/** Labelled one-line input. */
export function TextField({
	label,
	value,
	onInput,
	hint,
	error,
	placeholder,
	secret,
	autocomplete,
	inputMode,
}: TextFieldProps) {
	const id = useId();
	return (
		<div>
			<label for={id} class="block text-label text-muted">
				{label}
			</label>
			{secret ? (
				<input
					id={id}
					type="password"
					class={inputClass}
					value={value}
					placeholder={placeholder}
					autocomplete={autocomplete ?? "off"}
					onInput={(e) => onInput(e.currentTarget.value)}
				/>
			) : (
				<input
					id={id}
					type="text"
					class={inputClass}
					value={value}
					placeholder={placeholder}
					autocomplete={autocomplete ?? "off"}
					inputMode={inputMode}
					onInput={(e) => onInput(e.currentTarget.value)}
				/>
			)}
			{hint && !error && <p class="mt-1.5 text-label text-faint">{hint}</p>}
			{error && <p class="mt-1.5 text-label text-bad">{error}</p>}
		</div>
	);
}

/** Labelled number input. */
export function NumberField({
	label,
	value,
	onInput,
	min,
	max,
	hint,
}: {
	label: string;
	value: number;
	onInput: (value: number) => void;
	min: number;
	max: number;
	hint?: string;
}) {
	const id = useId();
	return (
		<div>
			<label for={id} class="block text-label text-muted">
				{label}
			</label>
			<input
				id={id}
				type="number"
				inputMode="numeric"
				min={min}
				max={max}
				class={inputClass}
				value={value}
				onInput={(e) => onInput(Number(e.currentTarget.value))}
			/>
			{hint && <p class="mt-1.5 text-label text-faint">{hint}</p>}
		</div>
	);
}

/** Labelled multi-line input. */
export function TextArea({
	label,
	value,
	onInput,
	rows = 8,
	hint,
	mono,
}: {
	label: string;
	value: string;
	onInput: (value: string) => void;
	rows?: number;
	hint?: ComponentChildren;
	mono?: boolean;
}) {
	const id = useId();
	return (
		<div>
			<label for={id} class="block text-label text-muted">
				{label}
			</label>
			<textarea
				id={id}
				rows={rows}
				class={`${inputClass} leading-relaxed ${mono ? "font-mono text-[14px]" : ""}`}
				value={value}
				onInput={(e) => onInput(e.currentTarget.value)}
			/>
			{hint && <p class="mt-1.5 text-label text-faint">{hint}</p>}
		</div>
	);
}

/** Labelled native select: on phones this opens the system picker. */
export function SelectField<T extends string>({
	label,
	value,
	options,
	onChange,
	hint,
}: {
	label: string;
	value: T;
	options: { value: T; label: string }[];
	onChange: (value: T) => void;
	hint?: ComponentChildren;
}) {
	const id = useId();
	return (
		<div>
			<label for={id} class="block text-label text-muted">
				{label}
			</label>
			<select
				id={id}
				class={`${inputClass} appearance-none`}
				value={value}
				onChange={(e) => onChange(e.currentTarget.value as T)}
			>
				{options.map((o) => (
					<option key={o.value} value={o.value}>
						{o.label}
					</option>
				))}
			</select>
			{hint && <p class="mt-1.5 text-label text-faint">{hint}</p>}
		</div>
	);
}

/** A checkbox row with a 44 px tap area. */
export function Toggle({
	label,
	checked,
	onChange,
	hint,
}: {
	label: string;
	checked: boolean;
	onChange: (checked: boolean) => void;
	hint?: string;
}) {
	const id = useId();
	return (
		<label for={id} class="flex min-h-11 cursor-pointer items-start gap-3 py-2">
			<input
				id={id}
				type="checkbox"
				class="mt-1 size-[18px] shrink-0 accent-accent"
				checked={checked}
				onChange={(e) => onChange(e.currentTarget.checked)}
			/>
			<span>
				<span class="block">{label}</span>
				{hint && <span class="block text-label text-faint">{hint}</span>}
			</span>
		</label>
	);
}
