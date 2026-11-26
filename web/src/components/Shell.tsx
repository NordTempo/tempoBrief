import type { ComponentChildren } from "preact";
import { useLocation } from "preact-iso";

const NAV = [
	{
		href: "/",
		label: "Brief",
		public: true,
		match: (p: string) => p === "/" || p.startsWith("/briefs"),
	},
	{
		href: "/stories",
		label: "Stories",
		public: true,
		match: (p: string) => p.startsWith("/stories"),
	},
	{
		href: "/learning",
		label: "Learning",
		public: false,
		match: (p: string) => p.startsWith("/learning"),
	},
	{ href: "/feeds", label: "Feeds", public: false, match: (p: string) => p.startsWith("/feeds") },
	{
		href: "/settings",
		label: "Settings",
		public: false,
		match: (p: string) => p.startsWith("/settings"),
	},
];

const active = "text-ink shadow-[0_2px_0_var(--color-accent)]";
const idle = "text-muted hover:text-ink";

/**
 * The app frame. Signed in, wide screens get the nav in the header and phones
 * get a tab bar at the bottom, within thumb reach. Visitors only have Brief
 * and Stories, so their nav fits in the header on every screen. Log in or
 * Log out sits at the right of the header; signed in, "Missed?" sits next to
 * it, so a story heard about elsewhere can be reported from any page.
 */
export function Shell({
	signedIn,
	onLogOut,
	children,
}: {
	signedIn: boolean;
	onLogOut: () => void;
	children: ComponentChildren;
}) {
	const { path } = useLocation();
	const nav = signedIn ? NAV : NAV.filter((n) => n.public);
	return (
		<div class={`min-h-screen ${signedIn ? "pb-tabbar sm:pb-0" : ""}`}>
			<header class="border-line border-b pt-[env(safe-area-inset-top)]">
				<div class="mx-auto flex h-14 max-w-[1280px] items-center gap-5 px-5 sm:h-[68px] sm:gap-9 sm:px-10">
					<a href="/" class="font-medium text-[17px] tracking-[-0.02em]">
						tempoBrief
					</a>
					<nav class={`gap-5 sm:flex sm:gap-6 ${signedIn ? "hidden" : "flex"}`}>
						{nav.map((n) => (
							<a key={n.href} href={n.href} class={n.match(path) ? active : idle}>
								{n.label}
							</a>
						))}
					</nav>
					{signedIn ? (
						<>
							<a
								href="/missed"
								class={`ml-auto flex min-h-11 items-center ${path === "/missed" ? active : idle}`}
							>
								Missed?
							</a>
							<button type="button" class={`min-h-11 ${idle}`} onClick={onLogOut}>
								Log out
							</button>
						</>
					) : (
						<a
							href="/login"
							class={`ml-auto flex min-h-11 items-center ${nav.some((n) => n.match(path)) ? idle : active}`}
						>
							Log in
						</a>
					)}
				</div>
			</header>
			<main class="mx-auto max-w-[720px] px-5 pb-16 sm:px-0">{children}</main>
			{signedIn && (
				<nav
					class="fixed inset-x-0 bottom-0 z-10 grid grid-cols-5 border-line border-t bg-bg pb-[env(safe-area-inset-bottom)] sm:hidden"
					aria-label="Sections"
				>
					{nav.map((n) => (
						<a
							key={n.href}
							href={n.href}
							class={`flex h-14 items-center justify-center text-[13px] ${n.match(path) ? "text-ink shadow-[inset_0_2px_0_var(--color-accent)]" : "text-muted"}`}
						>
							{n.label}
						</a>
					))}
				</nav>
			)}
		</div>
	);
}
