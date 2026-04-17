import os from "node:os";
import path from "node:path";

export function obsidianConfigDir({ platform = process.platform, env = process.env, homeDir = os.homedir() } = {}) {
	switch (platform) {
		case "darwin":
			return path.join(homeDir, "Library", "Application Support", "obsidian");
		case "win32":
			return path.join(env.APPDATA ?? path.join(homeDir, "AppData", "Roaming"), "obsidian");
		default:
			return path.join(env.XDG_CONFIG_HOME ?? path.join(homeDir, ".config"), "obsidian");
	}
}

export function isolatedObsidianEnv(tempRoot, { platform = process.platform, env = process.env } = {}) {
	const nextEnv = { ...env };
	switch (platform) {
		case "darwin": {
			const homeDir = path.join(tempRoot, "home");
			nextEnv.HOME = homeDir;
			return { env: nextEnv, configDir: obsidianConfigDir({ platform, env: nextEnv, homeDir }) };
		}
		case "win32": {
			nextEnv.APPDATA = path.join(tempRoot, "AppData", "Roaming");
			return { env: nextEnv, configDir: obsidianConfigDir({ platform, env: nextEnv }) };
		}
		default: {
			nextEnv.XDG_CONFIG_HOME = path.join(tempRoot, ".config");
			return { env: nextEnv, configDir: obsidianConfigDir({ platform, env: nextEnv }) };
		}
	}
}

export async function prepareObsidianConfigIsolation(tempRoot, { platform = process.platform, env = process.env } = {}) {
	return {
		...isolatedObsidianEnv(tempRoot, { platform, env }),
		restore: async () => {},
	};
}
