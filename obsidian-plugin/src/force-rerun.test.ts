import { test } from "node:test";
import * as assert from "node:assert/strict";

import { formatForceRerunDescription } from "./force-rerun";

test("formatForceRerunDescription includes the active session when present", () => {
	assert.equal(
		formatForceRerunDescription("TIC-097", "TIC-097-3"),
		"Forcing a re-run will terminate the active agent session for TIC-097 (TIC-097-3) and start a new run. Progress in the running session will be lost.",
	);
});

test("formatForceRerunDescription omits the session suffix when missing", () => {
	assert.equal(
		formatForceRerunDescription("TIC-097"),
		"Forcing a re-run will terminate the active agent session for TIC-097 and start a new run. Progress in the running session will be lost.",
	);
});
