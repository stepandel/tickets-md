import { test } from "node:test";
import * as assert from "node:assert/strict";

import { readBoardViewState } from "./board-view-state";

test("readBoardViewState restores saved state", () => {
	assert.deepEqual(readBoardViewState({ showArchived: true, filterQuery: "tic-133" }), {
		showArchived: true,
		filterQuery: "tic-133",
	});
});

test("readBoardViewState accepts legacy query state", () => {
	assert.deepEqual(readBoardViewState({ showArchived: true, query: "tic-135" }), {
		showArchived: true,
		filterQuery: "tic-135",
	});
});

test("readBoardViewState defaults missing state to false", () => {
	assert.deepEqual(readBoardViewState(undefined), { showArchived: false, filterQuery: "" });
	assert.deepEqual(readBoardViewState(null), { showArchived: false, filterQuery: "" });
});

test("readBoardViewState rejects non-boolean showArchived values", () => {
	assert.deepEqual(readBoardViewState({ showArchived: "true" }), { showArchived: false, filterQuery: "" });
	assert.deepEqual(readBoardViewState({ showArchived: 1 }), { showArchived: false, filterQuery: "" });
	assert.deepEqual(readBoardViewState({ showArchived: false }), { showArchived: false, filterQuery: "" });
});

test("readBoardViewState rejects non-string filter values", () => {
	assert.deepEqual(readBoardViewState({ filterQuery: 123 }), { showArchived: false, filterQuery: "" });
	assert.deepEqual(readBoardViewState({ filterQuery: true }), { showArchived: false, filterQuery: "" });
	assert.deepEqual(readBoardViewState({ query: 42 }), { showArchived: false, filterQuery: "" });
});
