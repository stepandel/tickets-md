import * as assert from "node:assert/strict";
import { test } from "node:test";

import {
	labelBadgeStyle,
	lookupLabel,
	normalizeLabelName,
	orderedLabelNames,
} from "./labels";

test("normalizeLabelName trims and lowercases values", () => {
	assert.equal(normalizeLabelName("  Backend  "), "backend");
	assert.equal(normalizeLabelName(undefined), "");
});

test("lookupLabel returns null when no config is provided", () => {
	assert.equal(lookupLabel(undefined, "backend"), null);
});

test("lookupLabel matches keys case-insensitively and preserves styling", () => {
	assert.deepEqual(
		lookupLabel(
			{
				" Backend ": { color: "#123456", bold: true },
			},
			"backend",
		),
		{ color: "#123456", bold: true },
	);
});

test("lookupLabel returns null for unknown labels", () => {
	assert.equal(lookupLabel({ backend: { color: "#111" } }, "missing"), null);
});

test("labelBadgeStyle uses the fallback color for unconfigured labels", () => {
	const style = labelBadgeStyle(undefined, "backend");
	assert.equal(style.backgroundColor, "#6b7280");
	assert.equal(style.fontWeight, "600");
});

test("labelBadgeStyle preserves bold and non-bold weights", () => {
	assert.equal(
		labelBadgeStyle({ urgent: { color: "#101010", bold: true } }, "urgent").fontWeight,
		"700",
	);
	assert.equal(
		labelBadgeStyle({ minor: { color: "#EFEFEF" } }, "minor").fontWeight,
		"600",
	);
});

test("labelBadgeStyle picks a readable text color by luminance", () => {
	assert.equal(labelBadgeStyle({ light: { color: "#FFFFFF" } }, "light").color, "#111111");
	assert.equal(labelBadgeStyle({ dark: { color: "#000000" } }, "dark").color, "#FFFFFF");
});

test("orderedLabelNames returns empty for nullish configs", () => {
	assert.deepEqual(orderedLabelNames(undefined), []);
	assert.deepEqual(orderedLabelNames(null as unknown as undefined), []);
	assert.deepEqual(orderedLabelNames({}), []);
});

test("orderedLabelNames treats order zero as explicit", () => {
	assert.deepEqual(
		orderedLabelNames({
			later: { color: "#222" },
			first: { color: "#111", order: 0 },
		}),
		["first", "later"],
	);
});

test("orderedLabelNames sorts ordered entries before unordered ones", () => {
	assert.deepEqual(
		orderedLabelNames({
			Medium: { color: "#333", order: 10 },
			P0: { color: "#111", order: 0 },
			"z-last": { color: "#999" },
			"A first": { color: "#aaa" },
		}),
		["P0", "Medium", "A first", "z-last"],
	);
});

test("orderedLabelNames sorts unordered entries by normalized name", () => {
	assert.deepEqual(
		orderedLabelNames({
			" zeta ": { color: "#111" },
			Alpha: { color: "#222" },
			beta: { color: "#333" },
		}),
		["Alpha", "beta", " zeta "],
	);
});
