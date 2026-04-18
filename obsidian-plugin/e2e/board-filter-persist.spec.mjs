import path from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";

import { dismissTrustDialogIfPresent, launchObsidianWithVault } from "./helpers/obsidian-launch.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const fixtureVault = path.join(here, "fixtures", "vault-archive");

test.describe.configure({ mode: "serial" });

test("persists board filter query across renderer reloads", async () => {
	let cleanup = async () => {};
	try {
		const launched = await launchObsidianWithVault({ fixtureVault });
		const { page } = launched;
		cleanup = launched.cleanup;
		await dismissTrustDialogIfPresent(page);
		await openBoard(page);

		await expect(page.locator(".tb-ticket-id")).toContainText(["TIC-001", "TIC-002"]);
		await expect(page.locator(".tb-ticket-id").filter({ hasText: "TIC-003" })).toHaveCount(0);

		await setFilterQuery(page, "tic-002");
		await expect(page.locator(".tb-ticket-id").filter({ hasText: "TIC-002" })).toHaveCount(1);
		await expect(page.locator(".tb-ticket-id").filter({ hasText: "TIC-001" })).toHaveCount(0);
		await expect(page.locator(".tb-empty")).toContainText(["No matches"]);

		await reloadRenderer(page);
		await expect(page.locator(".tb-header-search")).toHaveValue("tic-002");
		await expect(page.locator(".tb-ticket-id").filter({ hasText: "TIC-002" })).toHaveCount(1);
		await expect(page.locator(".tb-ticket-id").filter({ hasText: "TIC-001" })).toHaveCount(0);

		await setFilterQuery(page, "");
		await expect(page.locator(".tb-header-search")).toHaveValue("");
		await expect(page.locator(".tb-ticket-id")).toContainText(["TIC-001", "TIC-002"]);

		await reloadRenderer(page);
		await expect(page.locator(".tb-header-search")).toHaveValue("");
		await expect(page.locator(".tb-ticket-id")).toContainText(["TIC-001", "TIC-002"]);
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

async function setFilterQuery(page, query) {
	const input = page.locator(".tb-header-search");
	await input.fill(query);

	await expect
		.poll(
			async () =>
				page.evaluate(() => {
					const leaf = window.app.workspace.getLeavesOfType("tickets-board")[0];
					const state =
						leaf?.view?.getState?.() ??
						leaf?.getViewState?.().state ??
						null;
					return state?.filterQuery ?? null;
				}),
			{ timeout: 30_000 },
		)
		.toBe(query);

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
}

async function reloadRenderer(page) {
	await page.evaluate(() => {
		location.reload();
	});
	await expect
		.poll(
			async () => {
				try {
					return await page.evaluate(() => document.readyState);
				} catch {
					return "loading";
				}
			},
			{ timeout: 60_000 },
		)
		.toBe("complete");
	await dismissTrustDialogIfPresent(page);
	await openBoard(page);
}
