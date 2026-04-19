import test from "node:test";
import assert from "node:assert/strict";

import {
	effectiveAgentStatus,
	canReplayTerminal,
	hasLiveTerminal,
	isQueued,
	QUEUED_STATUS,
	ticketRunLogPath,
} from "../src/agent-terminal.js";

test("ticketRunLogPath builds the saved ticket run log path", () => {
	assert.equal(ticketRunLogPath("TIC-077", "003-execute"), ".agents/TIC-077/runs/003-execute.log");
});

test("hasLiveTerminal only depends on agent_session", () => {
	assert.equal(hasLiveTerminal({ agent_session: "TIC-077-3" }), true);
	assert.equal(hasLiveTerminal({ agent_run: "003-execute", agent_status: "failed" }), false);
});

test("canReplayTerminal only allows terminal runs with a run id", () => {
	assert.equal(canReplayTerminal({ agent_run: "003-execute", agent_status: "failed" }), true);
	assert.equal(canReplayTerminal({ agent_run: "003-execute", agent_status: "errored" }), true);
	assert.equal(canReplayTerminal({ agent_run: "003-execute", agent_status: "done" }), true);
	assert.equal(canReplayTerminal({ agent_run: "003-execute", agent_status: "running" }), false);
	assert.equal(canReplayTerminal({ agent_status: "failed" }), false);
});

test("isQueued prefers queued_at when the cached agent status is absent or terminal", () => {
	assert.equal(isQueued({ queued_at: "2026-04-19T06:00:00Z" }), true);
	assert.equal(isQueued({ queued_at: "2026-04-19T06:00:00Z", agent_status: "done" }), true);
	assert.equal(isQueued({ queued_at: "2026-04-19T06:00:00Z", agent_status: "failed" }), true);
	assert.equal(isQueued({ queued_at: "2026-04-19T06:00:00Z", agent_status: "running" }), false);
	assert.equal(isQueued({ agent_status: "done" }), false);
});

test("effectiveAgentStatus shows queued unless an active status is already present", () => {
	assert.equal(effectiveAgentStatus({ queued_at: "2026-04-19T06:00:00Z" }), QUEUED_STATUS);
	assert.equal(
		effectiveAgentStatus({ queued_at: "2026-04-19T06:00:00Z", agent_status: "done" }),
		QUEUED_STATUS,
	);
	assert.equal(
		effectiveAgentStatus({ queued_at: "2026-04-19T06:00:00Z", agent_status: "running" }),
		"running",
	);
	assert.equal(effectiveAgentStatus({ agent_status: "failed" }), "failed");
});
