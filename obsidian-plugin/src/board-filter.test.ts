import { test } from "node:test";
import * as assert from "node:assert/strict";

import { normalizeBoardFilterQuery, ticketMatchesBoardFilter } from "./board-filter";

test("normalizeBoardFilterQuery trims and lowercases the query", () => {
	assert.equal(normalizeBoardFilterQuery("  TIC-12  "), "tic-12");
});

test("ticketMatchesBoardFilter matches searchable ticket metadata", () => {
	const ticket = {
		id: "TIC-133",
		title: "Add board-level ticket filter",
		priority: "High",
		project: "PRJ-002",
		labels: ["backend", "customer"],
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
	assert.equal(ticketMatchesBoardFilter(ticket, "customer"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "execute"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "running"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "tic-130"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "tic-131"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "tic-132"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "tic-120"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "tic-134"), true);
});

test("ticketMatchesBoardFilter requires every query token to match some field", () => {
	const ticket = {
		id: "TIC-138",
		title: "Add board-level ticket filter",
		priority: "High",
		project: "PRJ-002",
		labels: ["backend", "customer"],
		stage: "execute",
		agent_status: "running",
		parent: "TIC-120",
	};

	assert.equal(ticketMatchesBoardFilter(ticket, "high tic-138"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "execute prj-002"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "board high"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "customer running"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "tic-120 execute"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "high missing"), false);
	assert.equal(ticketMatchesBoardFilter(ticket, "tic-138 archive"), false);
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

test("ticketMatchesBoardFilter supports quoted phrases as contiguous substrings", () => {
	const ticket = {
		id: "TIC-153",
		title: "Add board-level ticket filter",
		priority: "High",
		project: "PRJ-002",
		labels: ["customer"],
	};

	assert.equal(ticketMatchesBoardFilter(ticket, "\"board-level ticket\""), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "\"board filter\""), false);
	assert.equal(ticketMatchesBoardFilter(ticket, "high \"board-level ticket\""), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "\"board-level ticket\" \"prj-002\""), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "\"BOARD-LEVEL TICKET\""), true);
});

test("ticketMatchesBoardFilter treats unterminated quotes as phrase-to-end-of-input", () => {
	const ticket = {
		id: "TIC-153",
		title: "Add board-level ticket filter",
		priority: "High",
	};

	assert.equal(ticketMatchesBoardFilter(ticket, "high \"board-level ticket"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "high \"board filter"), false);
});

test("ticketMatchesBoardFilter skips empty and whitespace-only quoted phrases", () => {
	const ticket = {
		id: "TIC-153",
		title: "Add board-level ticket filter",
		priority: "High",
	};

	assert.equal(ticketMatchesBoardFilter(ticket, "\"\""), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "\"   \""), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "\"\" high"), true);
});

test("ticketMatchesBoardFilter handles adjacent quoted and bare terms", () => {
	const ticket = {
		id: "foobaz",
		title: "bar baz",
		labels: ["qux"],
	};

	assert.equal(ticketMatchesBoardFilter(ticket, "foo\"bar baz\"qux"), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "foo\"bar quux\"qux"), false);
	assert.equal(ticketMatchesBoardFilter(ticket, "zzz\"r b\"qux"), false);
});

test("ticketMatchesBoardFilter preserves internal phrase whitespace literally", () => {
	const ticket = {
		id: "TIC-153",
		title: "Alpha beta gamma",
		labels: ["a  b"],
	};

	assert.equal(ticketMatchesBoardFilter(ticket, "\"alpha beta\""), true);
	assert.equal(ticketMatchesBoardFilter(ticket, "\"alpha  beta\""), false);
	assert.equal(ticketMatchesBoardFilter(ticket, "\"a  b\""), true);
});
