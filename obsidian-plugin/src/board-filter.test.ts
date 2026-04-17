import { test } from "node:test";
import * as assert from "node:assert/strict";

import { matchesFilter } from "./board-filter";

test("matchesFilter treats blank query as a match-all", () => {
	assert.equal(matchesFilter({ id: "TIC-135", title: "Add board-level ticket filter" }, ""), true);
	assert.equal(matchesFilter({ id: "TIC-135", title: "Add board-level ticket filter" }, "   "), true);
});

test("matchesFilter matches ticket id", () => {
	assert.equal(matchesFilter({ id: "TIC-135", title: "Add board-level ticket filter" }, "135"), true);
});

test("matchesFilter matches ticket title", () => {
	assert.equal(matchesFilter({ id: "TIC-135", title: "Add board-level ticket filter" }, "board-level"), true);
});

test("matchesFilter is case-insensitive", () => {
	assert.equal(matchesFilter({ id: "TIC-135", title: "Add Board-Level Ticket Filter" }, "board-level"), true);
	assert.equal(matchesFilter({ id: "tic-135", title: "Add board-level ticket filter" }, "TIC-135"), true);
});

test("matchesFilter supports multi-word queries", () => {
	assert.equal(matchesFilter({ id: "TIC-135", title: "Add board-level ticket filter" }, "ticket filter"), true);
});

test("matchesFilter handles missing or blank ticket fields", () => {
	assert.equal(matchesFilter({ id: "", title: "" }, "tic"), false);
	assert.equal(matchesFilter({}, "tic"), false);
});
