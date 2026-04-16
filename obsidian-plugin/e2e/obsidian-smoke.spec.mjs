import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { execFileSync } from "node:child_process";

import { _electron as electron, expect, test } from "@playwright/test";

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

	try {
		execFileSync(
			ticketsBin,
			["obsidian", "install", "--from", path.join(repoRoot, "obsidian-plugin"), "--vault", vaultPath],
			{ cwd: repoRoot, stdio: "inherit" },
		);

		const app = await electron.launch({
			executablePath: obsidianBin,
			args: ["--vault", vaultPath],
		});

		try {
			const page = await app.firstWindow();
			await page.waitForLoadState("domcontentloaded");

			await dismissTrustDialogIfPresent(page);

			await expect
				.poll(async () =>
					page.evaluate(() => Boolean(window.app?.commands?.commands?.["tickets-board:open-tickets-board"])),
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
			await app.close();
		}
	} finally {
		await fs.rm(tempRoot, { recursive: true, force: true });
	}
});

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
