import { test } from "node:test";
import * as assert from "node:assert/strict";

import { readBoardViewState } from "./board-view-state";

test("readBoardViewState restores saved true", () => {
	assert.deepEqual(readBoardViewState({ showArchived: true }), { showArchived: true, query: "" });
});

test("readBoardViewState defaults missing state to false", () => {
	assert.deepEqual(readBoardViewState(undefined), { showArchived: false, query: "" });
	assert.deepEqual(readBoardViewState(null), { showArchived: false, query: "" });
});

test("readBoardViewState rejects non-boolean showArchived values", () => {
	assert.deepEqual(readBoardViewState({ showArchived: "true" }), { showArchived: false, query: "" });
	assert.deepEqual(readBoardViewState({ showArchived: 1 }), { showArchived: false, query: "" });
	assert.deepEqual(readBoardViewState({ showArchived: false }), { showArchived: false, query: "" });
});

test("readBoardViewState restores saved query", () => {
	assert.deepEqual(readBoardViewState({ query: "tic-135" }), { showArchived: false, query: "tic-135" });
});

test("readBoardViewState rejects non-string query values", () => {
	assert.deepEqual(readBoardViewState({ query: 42 }), { showArchived: false, query: "" });
	assert.deepEqual(readBoardViewState({ query: true }), { showArchived: false, query: "" });
});
