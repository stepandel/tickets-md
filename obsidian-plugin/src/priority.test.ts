import * as assert from "node:assert/strict";
import { test } from "node:test";

import {
	lookupPriority,
	normalizePriorityName,
	orderedPriorityNames,
	priorityBadgeStyle,
} from "./priority";

test("lookupPriority returns the built-in critical priority", () => {
	assert.deepEqual(lookupPriority(undefined, "critical"), {
		color: "#FF5F5F",
		bold: true,
	});
});

test("normalizePriorityName trims and lowercases values", () => {
	assert.equal(normalizePriorityName("  MeD  "), "med");
});

test("priorityBadgeStyle uses the fallback for unknown priorities", () => {
	assert.deepEqual(priorityBadgeStyle(undefined, "custom"), {
		backgroundColor: "#FFD700",
		color: "#111111",
		fontWeight: "600",
	});
});

test("lookupPriority uses custom overrides", () => {
	assert.deepEqual(
		lookupPriority(
			{
				" P0 ": { color: "#123456", bold: true },
			},
			"p0",
		),
		{ color: "#123456", bold: true },
	);
});

test("lookupPriority is override-only when priorities are configured", () => {
	assert.equal(
		lookupPriority(
			{
				custom: { color: "#123456" },
			},
			"critical",
		),
		null,
	);
});

test("priorityBadgeStyle preserves bold and non-bold weights", () => {
	assert.equal(
		priorityBadgeStyle({ urgent: { color: "#101010", bold: true } }, "urgent").fontWeight,
		"700",
	);
	assert.equal(
		priorityBadgeStyle({ minor: { color: "#EFEFEF" } }, "minor").fontWeight,
		"600",
	);
});

test("orderedPriorityNames uses built-in defaults when priorities are absent", () => {
	assert.deepEqual(orderedPriorityNames(undefined), ["critical", "high", "medium", "low"]);
});

test("orderedPriorityNames keeps an empty configured map empty", () => {
	assert.deepEqual(orderedPriorityNames({}), []);
});

test("orderedPriorityNames treats order zero as explicit", () => {
	assert.deepEqual(
		orderedPriorityNames({
			P2: { color: "#222222" },
			P0: { color: "#000000", order: 0 },
		}),
		["P0", "P2"],
	);
});

test("orderedPriorityNames sorts negative orders before larger values", () => {
	assert.deepEqual(
		orderedPriorityNames({
			later: { color: "#333333", order: 5 },
			first: { color: "#111111", order: -10 },
			middle: { color: "#222222", order: 0 },
		}),
		["first", "middle", "later"],
	);
});

test("orderedPriorityNames sorts ordered entries before unordered ones", () => {
	assert.deepEqual(
		orderedPriorityNames({
			Medium: { color: "#333333", order: 10 },
			P0: { color: "#111111", order: 0 },
			"z-last": { color: "#999999" },
			"A first": { color: "#aaaaaa" },
		}),
		["P0", "Medium", "A first", "z-last"],
	);
});

test("orderedPriorityNames sorts unordered entries by normalized name", () => {
	assert.deepEqual(
		orderedPriorityNames({
			" zeta ": { color: "#111111" },
			Alpha: { color: "#222222" },
			beta: { color: "#333333" },
		}),
		["Alpha", "beta", " zeta "],
	);
});
