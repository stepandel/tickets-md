import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";

import { dismissTrustDialogIfPresent, launchObsidianWithVault } from "./helpers/obsidian-launch.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const fixtureVault = path.join(here, "fixtures", "vault");

test.describe.configure({ mode: "serial" });

test("opens the board and creates a ticket from the fixture vault", async () => {
	let cleanup = async () => {};
	try {
		const launched = await launchObsidianWithVault({ fixtureVault });
		const { page, vaultPath } = launched;
		cleanup = launched.cleanup;
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
		await cleanup();
	}
});
