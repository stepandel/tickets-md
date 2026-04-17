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
		const { page } = launched;
		cleanup = launched.cleanup;
		await dismissTrustDialogIfPresent(page);
		await openBoard(page);

		await expect(page.locator(".tb-stage-name")).toContainText(["backlog", "doing"]);
		await expect(page.locator(".tb-card-list[data-stage='archive']")).toHaveCount(0);
		await expect(page.locator(".tb-ticket-id").filter({ hasText: "TIC-003" })).toHaveCount(0);

		await setArchivedVisibility(page, true);
		await expect(page.locator(".tb-stage-name")).toContainText(["backlog", "doing", "archive"]);
		await expect(page.locator(".tb-card-list[data-stage='archive']")).toHaveCount(1);
		await expect(page.locator(".tb-card-list[data-stage='archive'] .tb-ticket-id")).toContainText(["TIC-003"]);

		await reloadRenderer(page);
		await expect(page.locator(".tb-stage-name")).toContainText(["backlog", "doing", "archive"]);
		await expect(page.locator(".tb-card-list[data-stage='archive']")).toHaveCount(1);
		await expect(page.locator(".tb-card-list[data-stage='archive'] .tb-ticket-id")).toContainText(["TIC-003"]);

		await setArchivedVisibility(page, false);
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

async function setArchivedVisibility(page, visible) {
	await page.evaluate(async (nextVisible) => {
		const leaf = window.app.workspace.getLeavesOfType("tickets-board")[0];
		const current = leaf?.getViewState?.();
		if (!leaf || !current) {
			throw new Error("tickets-board leaf is not available");
		}

		await leaf.setViewState({
			...current,
			state: {
				...(current.state ?? {}),
				showArchived: nextVisible,
			},
		});

		const saveLayout = window.app.workspace?.requestSaveLayout?.bind(window.app.workspace);
		if (saveLayout) {
			saveLayout();
		}
		const saveCommand = window.app.commands?.commands?.["workspace:save-layout"];
		if (saveCommand) {
			await window.app.commands.executeCommandById("workspace:save-layout");
		}
	}, visible);

	await expect
		.poll(
			async () =>
				page.evaluate(() => {
					const leaf = window.app.workspace.getLeavesOfType("tickets-board")[0];
					const state =
						leaf?.view?.getState?.() ??
						leaf?.getViewState?.().state ??
						null;
					return state?.showArchived ?? null;
				}),
			{ timeout: 30_000 },
		)
		.toBe(visible);
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
