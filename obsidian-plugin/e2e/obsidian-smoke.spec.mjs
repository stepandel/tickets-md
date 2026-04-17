import fs from "node:fs/promises";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { execFileSync, spawn } from "node:child_process";

import { chromium, expect, test } from "@playwright/test";

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "..", "..");
const fixtureVault = path.join(here, "fixtures", "vault");
const ticketsBin = process.env.TICKETS_BIN || "tickets";
const obsidianBin = process.env.OBSIDIAN_BIN;

test.describe.configure({ mode: "serial" });

test("opens the board and creates a ticket from the fixture vault", async () => {
	if (!obsidianBin) {
		throw new Error("OBSIDIAN_BIN is required for the Obsidian smoke test");
	}

	const tempRoot = await fs.mkdtemp(path.join(os.tmpdir(), "tickets-obsidian-e2e-"));
	const vaultPath = path.join(tempRoot, "vault");
	await fs.cp(fixtureVault, vaultPath, { recursive: true });

	let obsidianProcess;
	let browser;
	try {
		execFileSync(
			ticketsBin,
			["obsidian", "install", "--from", path.join(repoRoot, "obsidian-plugin"), "--vault", vaultPath],
			{ cwd: repoRoot, stdio: "inherit" },
		);

		// Obsidian ships with the Electron `enableNodeCliInspectArguments`
		// fuse disabled, which strips `--inspect=0` before Node can honor
		// it. That means Playwright's `_electron.launch` — which greps for
		// a "Debugger listening" line from the main-process Node inspector
		// — never attaches and hangs until its 180s internal timeout. The
		// renderer-side DevTools endpoint (opened by `--remote-debugging-
		// port`) does come up though, so we spawn Obsidian ourselves on a
		// fixed port and attach over CDP instead.
		const debugPort = await pickFreePort();
		const args = [
			`--remote-debugging-port=${debugPort}`,
			"--vault",
			vaultPath,
		];
		if (process.platform === "linux") {
			args.unshift("--no-sandbox");
		}
		obsidianProcess = spawn(obsidianBin, args, {
			stdio: ["ignore", "pipe", "pipe"],
			// Surface any AppImage APPDIR env the CI workflow exported.
			env: { ...process.env },
		});

		const cdpEndpoint = `http://127.0.0.1:${debugPort}`;
		await waitForCdpEndpoint(cdpEndpoint, 120_000);

		browser = await chromium.connectOverCDP(cdpEndpoint);
		const context = browser.contexts()[0] ?? (await browser.newContext());

		const page = context.pages()[0] ?? (await context.waitForEvent("page", { timeout: 60_000 }));
		await page.waitForLoadState("domcontentloaded");

		await dismissTrustDialogIfPresent(page);

		await expect
			.poll(
				async () =>
					page.evaluate(() => Boolean(window.app?.commands?.commands?.["tickets-board:open-tickets-board"])),
				{ timeout: 60_000 },
			)
			.toBe(true);

		await page.evaluate(async () => {
			await window.app.commands.executeCommandById("tickets-board:open-tickets-board");
		});

		await expect(page.locator(".tb-board")).toBeVisible();
		await expect(page.locator(".tb-board .tb-card-title")).toContainText(["Seeded backlog ticket"]);

		await page.locator('.tb-board .tb-card-list[data-stage="backlog"] > .tb-add-ticket-btn').click();

		await expect
			.poll(async () => {
				try {
					await fs.access(path.join(vaultPath, "backlog", "TIC-002.md"));
					return true;
				} catch {
					return false;
				}
			})
			.toBe(true);

		await expect(page.locator(".tb-board .tb-ticket-id")).toContainText(["TIC-001", "TIC-002"]);
	} finally {
		if (browser) {
			try {
				await browser.close();
			} catch {
				// Browser disconnect races with Obsidian tearing down; swallow.
			}
		}
		if (obsidianProcess && obsidianProcess.exitCode === null) {
			obsidianProcess.kill("SIGTERM");
			const exited = await new Promise((resolve) => {
				const timer = setTimeout(() => resolve(false), 3_000);
				obsidianProcess.once("exit", () => {
					clearTimeout(timer);
					resolve(true);
				});
			});
			if (!exited) {
				try {
					obsidianProcess.kill("SIGKILL");
				} catch {
					// Already gone.
				}
			}
		}
		await fs.rm(tempRoot, { recursive: true, force: true });
	}
});

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

async function waitForCdpEndpoint(endpoint, timeoutMs) {
	const deadline = Date.now() + timeoutMs;
	let lastError;
	while (Date.now() < deadline) {
		try {
			const res = await fetch(`${endpoint}/json/version`);
			if (res.ok) return;
			lastError = new Error(`HTTP ${res.status}`);
		} catch (err) {
			lastError = err;
		}
		await new Promise((r) => setTimeout(r, 500));
	}
	throw new Error(`Obsidian CDP endpoint ${endpoint} not reachable within ${timeoutMs}ms: ${lastError?.message ?? "unknown error"}`);
}

async function dismissTrustDialogIfPresent(page) {
	const trustButton = page.getByRole("button", { name: /trust author/i });
	try {
		await trustButton.waitFor({ state: "visible", timeout: 3000 });
		await trustButton.click();
	} catch {
		// No trust dialog — either the vault is already trusted or this build
		// skips the prompt. Either way, proceed to command polling.
	}
}
