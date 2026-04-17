import { test } from "node:test";
import * as assert from "node:assert/strict";

<<<<<<< HEAD
import { normalizeBoardFilterQuery, ticketMatchesBoardFilter } from "./board-filter";

test("normalizeBoardFilterQuery trims and lowercases the query", () => {
	assert.equal(normalizeBoardFilterQuery("  TIC-12  "), "tic-12");
});

test("ticketMatchesBoardFilter matches id, title, priority, and project", () => {
	const ticket = {
		id: "TIC-133",
		title: "Add board-level ticket filter",
		priority: "High",
		project: "PRJ-002",
	};

	assert.equal(ticketMatchesBoardFilter(ticket, "tic-133"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "BOARD-LEVEL"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "high"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "prj-002"), true);
});

test("ticketMatchesBoardFilter ignores surrounding whitespace and rejects misses", () => {
	const ticket = {
		id: "TIC-133",
		title: "Add board-level ticket filter",
	};

	assert.equal(ticketMatchesBoardFilter(ticket, "  board  "), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "missing"), false);
	assert.equal(ticketMatchesBoardFilter(ticket, "   "), true);
=======
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
>>>>>>> tickets/TIC-135
});
