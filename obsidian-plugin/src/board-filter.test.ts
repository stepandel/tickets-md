import { test } from "node:test";
import * as assert from "node:assert/strict";

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
		stage: "execute",
		agent_status: "running",
		related: ["TIC-130"],
		blocked_by: ["TIC-131"],
		blocks: ["TIC-132"],
		parent: "TIC-120",
		children: ["TIC-134"],
	};

	assert.equal(ticketMatchesBoardFilter(ticket, "tic-133"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "BOARD-LEVEL"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "high"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "prj-002"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "execute"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "running"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "tic-130"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "tic-131"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "tic-132"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "tic-120"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "tic-134"), true);
});

test("ticketMatchesBoardFilter applies tokenized AND semantics across fields", () => {
	const ticket = {
		id: "TIC-138",
		title: "Add board-level ticket filter",
		priority: "High",
		project: "PRJ-002",
		stage: "execute",
	};

	assert.equal(ticketMatchesBoardFilter(ticket, "high tic-138"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "execute prj-002"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "board high"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "high missing"), false);
	assert.equal(ticketMatchesBoardFilter(ticket, "tic-138 archive"), false);
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
