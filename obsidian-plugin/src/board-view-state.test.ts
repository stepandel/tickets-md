import { test } from "node:test";
import * as assert from "node:assert/strict";

import { readBoardViewState } from "./board-view-state";

test("readBoardViewState restores saved true", () => {
	assert.deepEqual(readBoardViewState({ showArchived: true }), { showArchived: true });
});

test("readBoardViewState defaults missing state to false", () => {
	assert.deepEqual(readBoardViewState(undefined), { showArchived: false });
});

test("readBoardViewState rejects non-boolean showArchived values", () => {
	assert.deepEqual(readBoardViewState({ showArchived: "true" }), { showArchived: false });
	assert.deepEqual(readBoardViewState({ showArchived: 1 }), { showArchived: false });
	assert.deepEqual(readBoardViewState({ showArchived: false }), { showArchived: false });
});
