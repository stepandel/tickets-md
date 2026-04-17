import { randomBytes } from "node:crypto";
import fs from "node:fs/promises";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { execFileSync, spawn } from "node:child_process";

import { chromium, expect, test } from "@playwright/test";

import { isolatedObsidianEnv } from "./obsidian-config.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "..", "..");
const fixtureVault = path.join(here, "fixtures", "vault-archive");
const ticketsBin = process.env.TICKETS_BIN || "tickets";
const obsidianBin = process.env.OBSIDIAN_BIN;

test.describe.configure({ mode: "serial" });

test("persists archived-stage visibility across renderer reloads", async () => {
	if (!obsidianBin) {
		throw new Error("OBSIDIAN_BIN is required for the Obsidian archive persistence test");
	}

	const tempRoot = await fs.mkdtemp(path.join(os.tmpdir(), "tickets-obsidian-e2e-"));
	const vaultPath = path.join(tempRoot, "vault");
	const { env: obsidianEnv, configDir } = isolatedObsidianEnv(path.join(tempRoot, "obsidian-home"));
	await fs.cp(fixtureVault, vaultPath, { recursive: true });

	let obsidianProcess;
	let browser;
	try {
		execFileSync(
			ticketsBin,
			["obsidian", "install", "--from", path.join(repoRoot, "obsidian-plugin"), "--vault", vaultPath],
			{ cwd: repoRoot, stdio: "inherit" },
		);

		await registerVault(vaultPath, configDir);

		const debugPort = await pickFreePort();
		const args = [`--remote-debugging-port=${debugPort}`];
		if (process.platform === "linux") {
			args.unshift("--no-sandbox");
		}
		obsidianProcess = spawn(obsidianBin, args, {
			stdio: ["ignore", "pipe", "pipe"],
			env: obsidianEnv,
		});

		const cdpEndpoint = `http://127.0.0.1:${debugPort}`;
		await waitForCdpEndpoint(cdpEndpoint, 120_000);

		browser = await chromium.connectOverCDP(cdpEndpoint);
		const context = browser.contexts()[0] ?? (await browser.newContext());
		const page = context.pages()[0] ?? (await context.waitForEvent("page", { timeout: 60_000 }));
		await page.waitForLoadState("domcontentloaded");

		await dismissTrustDialogIfPresent(page);
		await openBoard(page);

		await expect(page.locator(".tb-stage-name")).toContainText(["backlog", "doing"]);
		await expect(page.locator(".tb-card-list[data-stage='archive']")).toHaveCount(0);
		await expect(page.locator(".tb-ticket-id").filter({ hasText: "TIC-003" })).toHaveCount(0);

		await setArchivedVisibility(page, vaultPath, true);
		await expect(page.locator(".tb-stage-name")).toContainText(["backlog", "doing", "archive"]);
		await expect(page.locator(".tb-card-list[data-stage='archive']")).toHaveCount(1);
		await expect(page.locator(".tb-card-list[data-stage='archive'] .tb-ticket-id")).toContainText(["TIC-003"]);

		await reloadRenderer(page);
		await expect(page.locator(".tb-stage-name")).toContainText(["backlog", "doing", "archive"]);
		await expect(page.locator(".tb-card-list[data-stage='archive']")).toHaveCount(1);
		await expect(page.locator(".tb-card-list[data-stage='archive'] .tb-ticket-id")).toContainText(["TIC-003"]);

		await setArchivedVisibility(page, vaultPath, false);
		await expect(page.locator(".tb-card-list[data-stage='archive']")).toHaveCount(0);
		await expect(page.locator(".tb-ticket-id").filter({ hasText: "TIC-003" })).toHaveCount(0);

		await reloadRenderer(page);
		await expect(page.locator(".tb-stage-name")).toContainText(["backlog", "doing"]);
		await expect(page.locator(".tb-card-list[data-stage='archive']")).toHaveCount(0);
		await expect(page.locator(".tb-ticket-id").filter({ hasText: "TIC-003" })).toHaveCount(0);
	} finally {
		if (browser) {
			try {
				await browser.close();
			} catch {
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
				}
			}
		}
		await fs.rm(tempRoot, { recursive: true, force: true });
	}
});

async function openBoard(page) {
	await expect
		.poll(
			async () =>
				page.evaluate(() => Boolean(window.app?.commands?.commands?.["tickets-board:open-tickets-board"])),
			{ timeout: 60_000 },
		)
		.toBe(true);

	await page.evaluate(async () => {
		const leaves = window.app.workspace.getLeavesOfType("tickets-board");
		if (leaves.length === 0) {
			await window.app.commands.executeCommandById("tickets-board:open-tickets-board");
		}
	});

	await expect(page.locator(".tb-board")).toBeVisible();
}

async function setArchivedVisibility(page, vaultPath, visible) {
	const workspacePath = path.join(vaultPath, ".obsidian", "workspace.json");
	const previousMtime = await readMtimeMs(workspacePath);

	await page.locator('.tb-header-btn[aria-label="Board menu"]').click();
	await page.getByRole("menuitem", { name: visible ? "Show archived stage" : "Hide archived stage" }).click();

	await page.evaluate(async () => {
		const saveLayout = window.app.workspace?.requestSaveLayout?.bind(window.app.workspace);
		if (saveLayout) {
			saveLayout();
		}
		const saveCommand = window.app.commands?.commands?.["workspace:save-layout"];
		if (saveCommand) {
			await window.app.commands.executeCommandById("workspace:save-layout");
		}
	});

	await expect
		.poll(async () => (await readMtimeMs(workspacePath)) > previousMtime, { timeout: 30_000 })
		.toBe(true);
}

async function reloadRenderer(page) {
	await page.evaluate(() => {
		location.reload();
	});
	await page.waitForLoadState("domcontentloaded");
	await dismissTrustDialogIfPresent(page);
	await openBoard(page);
}

async function readMtimeMs(filePath) {
	try {
		const stat = await fs.stat(filePath);
		return stat.mtimeMs;
	} catch {
		return 0;
	}
}

async function registerVault(vaultPath, configDir) {
	await fs.mkdir(configDir, { recursive: true });
	const configPath = path.join(configDir, "obsidian.json");
	let existing = { vaults: {} };
	try {
		existing = JSON.parse(await fs.readFile(configPath, "utf8"));
		if (typeof existing !== "object" || existing === null) existing = { vaults: {} };
		if (typeof existing.vaults !== "object" || existing.vaults === null) existing.vaults = {};
		for (const entry of Object.values(existing.vaults)) {
			if (entry && typeof entry === "object") delete entry.open;
		}
	} catch {
		existing = { vaults: {} };
	}
	const vaultId = randomBytes(8).toString("hex");
	existing.vaults[vaultId] = {
		path: vaultPath,
		ts: Date.now(),
		open: true,
	};
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
	}
}
