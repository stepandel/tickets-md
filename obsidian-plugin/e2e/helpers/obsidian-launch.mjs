import { randomBytes } from "node:crypto";
import { execFileSync, spawn } from "node:child_process";
import fs from "node:fs/promises";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { chromium } from "@playwright/test";

import { isolatedObsidianEnv } from "../obsidian-config.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "..", "..", "..");
const ticketsBin = process.env.TICKETS_BIN || "tickets";
const obsidianBin = process.env.OBSIDIAN_BIN;
const launchTimeoutMs = envInt("OBSIDIAN_LAUNCH_TIMEOUT_MS", 60_000);

function envInt(name, fallback) {
	const raw = process.env[name];
	if (!raw) return fallback;
	const parsed = Number.parseInt(raw, 10);
	return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}

async function registerVault(vaultPath, configDir) {
	await fs.mkdir(configDir, { recursive: true });
	const configPath = path.join(configDir, "obsidian.json");
	let existing = { vaults: {}, cli: true };
	try {
		existing = JSON.parse(await fs.readFile(configPath, "utf8"));
		if (typeof existing !== "object" || existing === null) existing = { vaults: {}, cli: true };
		if (typeof existing.vaults !== "object" || existing.vaults === null) existing.vaults = {};
		for (const entry of Object.values(existing.vaults)) {
			if (entry && typeof entry === "object") delete entry.open;
		}
	} catch {
		existing = { vaults: {}, cli: true };
	}
	const vaultId = randomBytes(8).toString("hex");
	existing.vaults[vaultId] = {
		path: vaultPath,
		ts: Date.now(),
		open: true,
	};
	existing.cli = true;
	await fs.writeFile(configPath, JSON.stringify(existing, null, 2));
}

async function pickFreePort() {
	return new Promise((resolve, reject) => {
		const server = net.createServer();
		server.unref();
		server.on("error", reject);
		server.listen(0, "127.0.0.1", () => {
			const address = server.address();
			const port = typeof address === "object" && address ? address.port : 0;
			server.close(() => resolve(port));
		});
	});
}

async function waitForCdpEndpoint(endpoint, timeoutMs, obsidianProcess, processOutput) {
	const deadline = Date.now() + timeoutMs;
	let lastError;
	while (Date.now() < deadline) {
		if (obsidianProcess?.exitCode !== null && obsidianProcess?.exitCode !== undefined) {
			const output = processOutput.join("").trim();
			const details = output ? `\n\nObsidian output:\n${output}` : "";
			throw new Error(`Obsidian exited with code ${obsidianProcess.exitCode} before CDP was ready.${details}`);
		}
		const output = processOutput.join("");
		const wsEndpoint = extractWebSocketDebuggerUrl(output);
		if (wsEndpoint) {
			return { webSocketDebuggerUrl: wsEndpoint };
		}
		try {
			const attemptTimeoutMs = Math.max(1_000, Math.min(2_000, deadline - Date.now()));
			const res = await fetch(`${endpoint}/json/version`, {
				signal: AbortSignal.timeout(attemptTimeoutMs),
			});
			if (res.ok) {
				return await res.json();
			}
			lastError = new Error(`HTTP ${res.status}`);
		} catch (err) {
			lastError = err;
		}
		await new Promise((r) => setTimeout(r, 500));
	}
	const output = processOutput.join("").trim();
	const details = output ? `\n\nObsidian output:\n${output}` : "";
	throw new Error(
		`Obsidian CDP endpoint ${endpoint} not reachable within ${timeoutMs}ms: ${lastError?.message ?? "unknown error"}${details}`,
	);
}

function extractWebSocketDebuggerUrl(output) {
	return output.match(/DevTools listening on\s+(ws:\/\/\S+)/)?.[1] ?? null;
}

async function connectOverCdp(wsEndpoint, timeoutMs) {
	let timer;
	try {
		return await Promise.race([
			chromium.connectOverCDP(wsEndpoint),
			new Promise((_, reject) => {
				timer = setTimeout(() => reject(new Error(`Timed out after ${timeoutMs}ms connecting over CDP`)), timeoutMs);
			}),
		]);
	} finally {
		clearTimeout(timer);
	}
}

async function waitForObsidianPage(browser, timeoutMs) {
	const deadline = Date.now() + timeoutMs;
	while (Date.now() < deadline) {
		for (const context of browser.contexts()) {
			for (const page of context.pages()) {
				try {
					const isObsidianPage = await page.evaluate(() => Boolean(window.app?.workspace));
					if (isObsidianPage) {
						return { context, page };
					}
				} catch {
				}
			}
		}
		await new Promise((r) => setTimeout(r, 500));
	}
	throw new Error(`Timed out after ${timeoutMs}ms waiting for an Obsidian renderer page`);
}

async function terminateProcess(processHandle) {
	if (!processHandle || processHandle.exitCode !== null) {
		return;
	}
	processHandle.kill("SIGTERM");
	const exited = await new Promise((resolve) => {
		const timer = setTimeout(() => resolve(false), 3_000);
		processHandle.once("exit", () => {
			clearTimeout(timer);
			resolve(true);
		});
	});
	if (!exited) {
		try {
			processHandle.kill("SIGKILL");
		} catch {
		}
	}
}

export async function dismissTrustDialogIfPresent(page) {
	const trustButton = page.getByRole("button", { name: /trust author/i });
	try {
		await trustButton.waitFor({ state: "visible", timeout: 3000 });
		await trustButton.click();
	} catch {
		// No trust dialog — either the vault is already trusted or this build
		// skips the prompt. Either way, proceed to command polling.
	}
}

export async function launchObsidianWithVault({ fixtureVault }) {
	if (!obsidianBin) {
		throw new Error("OBSIDIAN_BIN is required for the Obsidian e2e tests");
	}

	const tempRoot = await fs.mkdtemp(path.join(os.tmpdir(), "tickets-obsidian-e2e-"));
	const vaultPath = path.join(tempRoot, "vault");
	const { env: obsidianEnv, configDir } = isolatedObsidianEnv(path.join(tempRoot, "obsidian-home"));
	const processOutput = [];
	await fs.cp(fixtureVault, vaultPath, { recursive: true });

	let obsidianProcess;
	let browser;

	try {
		execFileSync(
			ticketsBin,
			["obsidian", "install", "--from", path.join(repoRoot, "obsidian-plugin"), "--vault", vaultPath],
			{ cwd: repoRoot, stdio: "inherit" },
		);

		// Obsidian's `--vault <path>` CLI flag is gated on the "Command
		// line interface" setting, which is off by default and can't be
		// toggled from outside Obsidian. Without it, Obsidian launches
		// straight into the vault picker and no plugin ever loads. Writing
		// an entry into an isolated obsidian.json with `open: true` tells
		// Obsidian to open this vault on startup without touching the
		// user-global config.
		await registerVault(vaultPath, configDir);

		// Obsidian ships with the Electron `enableNodeCliInspectArguments`
		// fuse disabled, which strips `--inspect=0` before Node can honor
		// it. That means Playwright's `_electron.launch` — which greps for
		// a "Debugger listening" line from the main-process Node inspector
		// — never attaches and hangs until its 180s internal timeout. The
		// renderer-side DevTools endpoint (opened by `--remote-debugging-
		// port`) does come up though, so we spawn Obsidian ourselves on a
		// fixed port and attach over CDP instead.
		const debugPort = await pickFreePort();
		const args = [`--remote-debugging-port=${debugPort}`];
		if (process.platform === "linux") {
			args.unshift("--no-sandbox");
		}
		obsidianProcess = spawn(obsidianBin, args, {
			stdio: ["ignore", "pipe", "pipe"],
			// Isolated env keeps config/cache/logs out of the user-global
			// Obsidian home while preserving vars like APPDIR that the CI
			// workflow needs.
			env: obsidianEnv,
		});
		obsidianProcess.stdout?.on("data", (chunk) => {
			processOutput.push(chunk.toString());
		});
		obsidianProcess.stderr?.on("data", (chunk) => {
			processOutput.push(chunk.toString());
		});

		const cdpEndpoint = `http://127.0.0.1:${debugPort}`;
		const launchDeadline = Date.now() + launchTimeoutMs;
		const remaining = () => Math.max(1_000, launchDeadline - Date.now());

		const versionInfo = await waitForCdpEndpoint(cdpEndpoint, Math.min(30_000, remaining()), obsidianProcess, processOutput);
		const wsEndpoint = versionInfo?.webSocketDebuggerUrl || cdpEndpoint;

		browser = await connectOverCdp(wsEndpoint, Math.min(10_000, remaining()));
		const { context, page } = await waitForObsidianPage(browser, remaining());

		return {
			page,
			browser,
			context,
			obsidianProcess,
			vaultPath,
			tempRoot,
			cleanup: async () => {
				if (browser) {
					try {
						await browser.close();
					} catch {
						// Browser disconnect races with Obsidian tearing down; swallow.
					}
				}
				await terminateProcess(obsidianProcess);
				await fs.rm(tempRoot, { recursive: true, force: true });
			},
		};
	} catch (err) {
		if (browser) {
			try {
				await browser.close();
			} catch {
				// Browser disconnect races with Obsidian tearing down; swallow.
			}
		}
		await terminateProcess(obsidianProcess);
		await fs.rm(tempRoot, { recursive: true, force: true });
		throw err;
	}
}
