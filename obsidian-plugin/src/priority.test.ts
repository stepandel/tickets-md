import * as assert from "node:assert/strict";
import { test } from "node:test";

import { lookupPriority, normalizePriorityName, priorityBadgeStyle } from "./priority";

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
