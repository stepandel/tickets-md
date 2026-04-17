import test from "node:test";
import assert from "node:assert/strict";
import path from "node:path";

import { isolatedObsidianEnv, obsidianConfigDir } from "../e2e/obsidian-config.mjs";

test("obsidianConfigDir resolves Linux config under XDG_CONFIG_HOME", () => {
	assert.equal(
		obsidianConfigDir({
			platform: "linux",
			env: { XDG_CONFIG_HOME: "/tmp/xdg-config" },
			homeDir: "/unused",
		}),
		"/tmp/xdg-config/obsidian",
	);
});

test("isolatedObsidianEnv redirects Linux config into the temp root", () => {
	const tempRoot = "/tmp/tickets-obsidian-home";
	const { env, configDir } = isolatedObsidianEnv(tempRoot, { platform: "linux", env: { APPDIR: "/opt/Obsidian" } });
	assert.equal(env.XDG_CONFIG_HOME, path.join(tempRoot, ".config"));
	assert.equal(env.APPDIR, "/opt/Obsidian");
	assert.equal(configDir, path.join(tempRoot, ".config", "obsidian"));
});

test("isolatedObsidianEnv redirects macOS config via HOME", () => {
	const tempRoot = "/tmp/tickets-obsidian-home";
	const { env, configDir } = isolatedObsidianEnv(tempRoot, { platform: "darwin", env: {} });
	assert.equal(env.HOME, path.join(tempRoot, "home"));
	assert.equal(configDir, path.join(tempRoot, "home", "Library", "Application Support", "obsidian"));
});

test("isolatedObsidianEnv redirects Windows config via APPDATA", () => {
	const tempRoot = "C:\\temp\\tickets-obsidian-home";
	const { env, configDir } = isolatedObsidianEnv(tempRoot, { platform: "win32", env: {} });
	assert.equal(env.APPDATA, path.join(tempRoot, "AppData", "Roaming"));
	assert.equal(configDir, path.join(tempRoot, "AppData", "Roaming", "obsidian"));
});

test("isolatedObsidianEnv preserves unrelated env vars like APPDIR", () => {
	const tempRoot = "/tmp/tickets-obsidian-home";
	const { env, configDir } = isolatedObsidianEnv(tempRoot, {
		platform: "darwin",
		env: { APPDIR: "/Applications/Obsidian.app" },
	});
	assert.equal(env.APPDIR, "/Applications/Obsidian.app");
	assert.equal(env.HOME, path.join(tempRoot, "home"));
	assert.equal(configDir, path.join(tempRoot, "home", "Library", "Application Support", "obsidian"));
});
