import { defineConfig } from "@playwright/test";

function envInt(name, fallback) {
	const raw = process.env[name];
	if (!raw) return fallback;
	const parsed = Number.parseInt(raw, 10);
	return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}

export default defineConfig({
	testDir: "./e2e",
	fullyParallel: false,
	workers: 1,
	timeout: envInt("PLAYWRIGHT_TEST_TIMEOUT_MS", 90_000),
	reporter: "list",
	use: {
		trace: "retain-on-failure",
	},
});
