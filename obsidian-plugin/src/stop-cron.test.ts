import { test } from "node:test";
import * as assert from "node:assert/strict";

import { formatStopCronDescription } from "./stop-cron";

test("formatStopCronDescription includes run id and session when present", () => {
	assert.equal(
		formatStopCronDescription("nightly-triage", { run_id: "004-cron", session: "cron-17" }),
		"Stopping cron nightly-triage (run 004-cron, session cron-17) will terminate the live process. The run will be marked failed and this cannot be undone.",
	);
});

test("formatStopCronDescription includes only the run id when the session is missing", () => {
	assert.equal(
		formatStopCronDescription("nightly-triage", { run_id: "004-cron" }),
		"Stopping cron nightly-triage (run 004-cron) will terminate the live process. The run will be marked failed and this cannot be undone.",
	);
});

test("formatStopCronDescription includes only the session when the run id is missing", () => {
	assert.equal(
		formatStopCronDescription("nightly-triage", { session: "cron-17" }),
		"Stopping cron nightly-triage (session cron-17) will terminate the live process. The run will be marked failed and this cannot be undone.",
	);
});

test("formatStopCronDescription omits run details when neither field is present", () => {
	assert.equal(
		formatStopCronDescription("nightly-triage", {}),
		"Stopping cron nightly-triage will terminate the live process. The run will be marked failed and this cannot be undone.",
	);
});

test("formatStopCronDescription omits run details for null runs", () => {
	assert.equal(
		formatStopCronDescription("nightly-triage", null),
		"Stopping cron nightly-triage will terminate the live process. The run will be marked failed and this cannot be undone.",
	);
});
