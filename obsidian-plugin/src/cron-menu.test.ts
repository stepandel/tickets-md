import { test } from "node:test";
import * as assert from "node:assert/strict";

import { ACTIVE_AGENT_STATUSES, cronHasLiveSession } from "./agent-terminal.js";

test("cronHasLiveSession requires both a session and an active status", () => {
	for (const status of ACTIVE_AGENT_STATUSES) {
		assert.equal(cronHasLiveSession({ session: "cron-1", status }), true);
	}

	for (const status of ["done", "failed", "errored", undefined]) {
		assert.equal(cronHasLiveSession({ session: "cron-1", status }), false);
	}

	assert.equal(cronHasLiveSession({ status: "running" }), false);
	assert.equal(cronHasLiveSession(null), false);
});
