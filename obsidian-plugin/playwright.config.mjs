import { defineConfig } from "@playwright/test";

export default defineConfig({
	testDir: "./e2e",
	fullyParallel: false,
	workers: 1,
	// macOS CI runners need extra headroom: Obsidian/Electron can take 60–90s
	// just to boot + attach its inspector before the vault is ready.
	timeout: 300000,
	reporter: "list",
	use: {
		trace: "retain-on-failure",
	},
});
