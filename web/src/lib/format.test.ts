import { describe, expect, it } from "vitest";
import { formatAgo, formatScore, formatSources, plural } from "./format";

describe("format", () => {
	it("shows a decimal only when the score has one", () => {
		expect(formatScore(8)).toBe("8");
		expect(formatScore(7.62)).toBe("7.6");
		expect(formatScore(null)).toBe("");
	});
	it("names the first source and counts the rest", () => {
		expect(formatSources(["NRK", "VG", "E24"])).toBe("NRK +2");
		expect(formatSources(["NRK"])).toBe("NRK");
		expect(formatSources([])).toBe("");
	});
	it("says how long ago", () => {
		const now = Date.parse("2026-11-25T12:00:00Z");
		expect(formatAgo("2026-11-25T11:55:00Z", now)).toBe("5 min ago");
		expect(formatAgo("2026-11-25T09:00:00Z", now)).toBe("3 h ago");
		expect(formatAgo("2026-11-24T12:00:00Z", now)).toBe("yesterday");
		expect(formatAgo("", now)).toBe("never");
	});
	it("pluralizes", () => {
		expect(plural(1, "story", "stories")).toBe("1 story");
		expect(plural(3, "story", "stories")).toBe("3 stories");
	});
});
