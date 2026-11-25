import type { ComponentChildren } from "preact";
import { useLocation } from "preact-iso";

const NAV = [
	{ href: "/", label: "Brief", match: (p: string) => p === "/" || p.startsWith("/briefs") },
	{ href: "/stories", label: "Stories", match: (p: string) => p.startsWith("/stories") },
	{ href: "/learning", label: "Learning", match: (p: string) => p.startsWith("/learning") },
	{ href: "/feeds", label: "Feeds", match: (p: string) => p.startsWith("/feeds") },
	{ href: "/settings", label: "Settings", match: (p: string) => p.startsWith("/settings") },
];

/**
 * The app frame. Wide screens get the nav in the header; phones get a tab bar
 * at the bottom, within thumb reach, above the home indicator.
 */
export function Shell({ children }: { children: ComponentChildren }) {
	const { path } = useLocation();
	return (
		<div class="min-h-screen pb-tabbar sm:pb-0">
			<header class="border-line border-b pt-[env(safe-area-inset-top)]">
				<div class="mx-auto flex h-14 max-w-[1280px] items-center gap-9 px-5 sm:h-[68px] sm:px-10">
					<a href="/" class="font-medium text-[17px] tracking-[-0.02em]">
						tempoBrief
					</a>
					<nav class="hidden gap-6 sm:flex">
						{NAV.map((n) => (
							<a
								key={n.href}
								href={n.href}
								class={
									n.match(path)
										? "text-ink shadow-[0_2px_0_var(--color-accent)]"
										: "text-muted hover:text-ink"
								}
							>
								{n.label}
							</a>
						))}
					</nav>
				</div>
			</header>
			<main class="mx-auto max-w-[720px] px-5 pb-16 sm:px-0">{children}</main>
			<nav
				class="fixed inset-x-0 bottom-0 z-10 grid grid-cols-5 border-line border-t bg-bg pb-[env(safe-area-inset-bottom)] sm:hidden"
				aria-label="Sections"
			>
				{NAV.map((n) => (
					<a
						key={n.href}
						href={n.href}
						class={`flex h-14 items-center justify-center text-[13px] ${n.match(path) ? "text-ink shadow-[inset_0_2px_0_var(--color-accent)]" : "text-muted"}`}
					>
						{n.label}
					</a>
				))}
			</nav>
		</div>
	);
}
