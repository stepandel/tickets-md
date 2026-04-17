import { test } from "node:test";
import * as assert from "node:assert/strict";

import {
	pauseWatch,
	readWatchPauseStatus,
	resumeWatch,
	summarizeWatchPause,
} from "./watch-pause";

test("summarizeWatchPause reports offline state", () => {
	assert.deepEqual(summarizeWatchPause(null, "offline"), {
		kind: "offline",
		label: "watch offline",
		detail: "No terminal server running. Start `tickets watch`.",
		actionLabel: "Pause",
		actionDisabled: true,
	});
});

test("summarizeWatchPause reports paused detail", () => {
	assert.deepEqual(
		summarizeWatchPause({
			paused: true,
			paused_at: "2026-04-17T18:00:00Z",
			reason: "release freeze",
			warning: "metadata unreadable",
		}),
		{
			kind: "paused",
			label: "watch paused",
			detail: "since 2026-04-17T18:00:00Z • release freeze • warning: metadata unreadable",
			actionLabel: "Resume",
			actionDisabled: false,
		},
	);
});

test("readWatchPauseStatus validates the response body", async () => {
	await assert.rejects(
		readWatchPauseStatus(4100, async () => ({
			ok: true,
			statusText: "OK",
			async json() {
				return { paused: "yes" };
			},
			async text() {
				return "";
			},
		})),
		/watch status response is missing paused/,
	);
});

test("pauseWatch posts the reason and parses the response", async () => {
	let requestURL = "";
	let requestInit: RequestInit | undefined;
	const status = await pauseWatch(4100, "release freeze", async (input, init) => {
		requestURL = input;
		requestInit = init;
		return {
			ok: true,
			statusText: "OK",
			async json() {
				return { paused: true, reason: "release freeze" };
			},
			async text() {
				return "";
			},
		};
	});

	assert.equal(requestURL, "http://127.0.0.1:4100/watch/pause");
	assert.equal(requestInit?.method, "POST");
	assert.equal(requestInit?.body, JSON.stringify({ reason: "release freeze" }));
	assert.equal(status.paused, true);
	assert.equal(status.reason, "release freeze");
});

test("resumeWatch reports endpoint failures", async () => {
	await assert.rejects(
		resumeWatch(4100, async () => ({
			ok: false,
			statusText: "Service Unavailable",
			async json() {
				return {};
			},
			async text() {
				return "watch resume not available";
			},
		})),
		/watch resume: watch resume not available/,
	);
});
