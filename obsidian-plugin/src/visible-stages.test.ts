import { test } from "node:test";
import * as assert from "node:assert/strict";

import { visibleStages } from "./visible-stages";

test("visibleStages hides the configured archive stage by default", () => {
	assert.deepEqual(
		visibleStages({
			stages: ["backlog", "doing", "archive"],
			archive_stage: "archive",
		}),
		["backlog", "doing"],
	);
});

test("visibleStages includes the configured archive stage when requested", () => {
	assert.deepEqual(
		visibleStages(
			{
				stages: ["backlog", "doing", "archive"],
				archive_stage: "archive",
			},
			true,
		),
		["backlog", "doing", "archive"],
	);
});

test("visibleStages returns all stages when no archive stage is configured", () => {
	assert.deepEqual(
		visibleStages({
			stages: ["backlog", "doing", "done"],
		}),
		["backlog", "doing", "done"],
	);
});
