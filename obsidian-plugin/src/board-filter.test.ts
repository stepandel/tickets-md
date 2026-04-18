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
		stage: "execute",
		agent_status: "running",
	};

	assert.equal(ticketMatchesBoardFilter(ticket, "tic-133"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "BOARD-LEVEL"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "high"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "prj-002"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "execute"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "running"), true);
});

test("ticketMatchesBoardFilter ignores surrounding whitespace and rejects misses", () => {
	const ticket = {
		id: "TIC-133",
		title: "Add board-level ticket filter",
	};

	assert.equal(ticketMatchesBoardFilter(ticket, "  board  "), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "missing"), false);
	assert.equal(ticketMatchesBoardFilter(ticket, "   "), true);
});
