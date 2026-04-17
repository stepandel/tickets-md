import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";

import { dismissTrustDialogIfPresent, launchObsidianWithVault } from "./helpers/obsidian-launch.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const fixtureVault = path.join(here, "fixtures", "vault-archive");

test.describe.configure({ mode: "serial" });

test("persists archived-stage visibility across renderer reloads", async () => {
	let cleanup = async () => {};
	try {
		const launched = await launchObsidianWithVault({ fixtureVault });
		const { page, vaultPath } = launched;
		cleanup = launched.cleanup;
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
		await cleanup();
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
