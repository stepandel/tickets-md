import { test } from "node:test";
import * as assert from "node:assert/strict";

import { loadWatchStateSnapshot } from "./watch-state";

test("loadWatchStateSnapshot reports offline when the terminal server file is absent", async () => {
	assert.deepEqual(
		await loadWatchStateSnapshot(
			async () => null,
			async () => ({ paused: false }),
		),
		{
			status: null,
			error: "offline",
			port: null,
		},
	);
});

test("loadWatchStateSnapshot returns watch status and port when the server responds", async () => {
	assert.deepEqual(
		await loadWatchStateSnapshot(
			async () => ({ port: 4100, pid: 1234 }),
			async (port) => {
				assert.equal(port, 4100);
				return { paused: true, reason: "release freeze" };
			},
		),
		{
			status: { paused: true, reason: "release freeze" },
			error: null,
			port: 4100,
		},
	);
});

test("loadWatchStateSnapshot preserves the server port when status loading fails", async () => {
	assert.deepEqual(
		await loadWatchStateSnapshot(
			async () => ({ port: 4100, pid: 1234 }),
			async () => {
				throw new Error("watch status: connection refused");
			},
		),
		{
			status: null,
			error: "watch status: connection refused",
			port: 4100,
		},
	);
});
