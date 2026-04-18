import { test } from "node:test";
import * as assert from "node:assert/strict";

import { normalizeBoardFilterQuery, ticketMatchesBoardFilter } from "./board-filter";

test("normalizeBoardFilterQuery trims and lowercases the query", () => {
	assert.equal(normalizeBoardFilterQuery("  TIC-12  "), "tic-12");
});

test("ticketMatchesBoardFilter matches id, title, priority, project, stage, and agent status", () => {
	const ticket = {
		id: "TIC-133",
		title: "Add board-level ticket filter",
		priority: "High",
		project: "PRJ-002",
		stage: "done",
		agent_status: "blocked",
	};

	assert.equal(ticketMatchesBoardFilter(ticket, "tic-133"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "BOARD-LEVEL"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "high"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "prj-002"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "done"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "blocked"), true);
});

test("ticketMatchesBoardFilter requires every query token to match some field", () => {
	const ticket = {
		id: "TIC-133",
		title: "Add board-level ticket filter",
		priority: "High",
		project: "PRJ-002",
	};

	assert.equal(ticketMatchesBoardFilter(ticket, "high board"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "high missing"), false);
	assert.equal(ticketMatchesBoardFilter(ticket, "tic 133"), true);
});

test("ticketMatchesBoardFilter ignores surrounding whitespace, collapses internal spaces, and rejects misses", () => {
	const ticket = {
		id: "TIC-133",
		title: "Add board-level ticket filter",
		priority: "High",
	};

	assert.equal(ticketMatchesBoardFilter(ticket, "  board  "), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "  high   board  "), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "missing"), false);
	assert.equal(ticketMatchesBoardFilter(ticket, "   "), true);
});
