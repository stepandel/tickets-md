import path from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";

import { dismissTrustDialogIfPresent, launchObsidianWithVault } from "./helpers/obsidian-launch.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const fixtureVault = path.join(here, "fixtures", "vault-queued");

test.describe.configure({ mode: "serial" });

test("renders queued tickets in board and agents views from seeded frontmatter", async () => {
	let cleanup = async () => {};
	try {
		const launched = await launchObsidianWithVault({ fixtureVault });
		const { page } = launched;
		cleanup = launched.cleanup;
		await dismissTrustDialogIfPresent(page);

		await openBoard(page);

		await expect(page.locator(".tb-card-list[data-stage='execute'] .tb-ticket-id")).toContainText(["TIC-001", "TIC-002"]);

		const queuedCard = (id) =>
			page.locator(".tb-card", {
				has: page.locator(".tb-ticket-id", { hasText: id }),
			});
		const tic001Card = queuedCard("TIC-001");
		const tic002Card = queuedCard("TIC-002");
		const tic003Card = queuedCard("TIC-003");

		await expect(tic001Card.locator(".tb-agent-badge.tb-agent-queued[aria-label='queued']")).toHaveCount(1);
		await expect(tic002Card.locator(".tb-agent-badge.tb-agent-queued[aria-label='queued']")).toHaveCount(1);
		await expect(tic002Card.locator(".tb-agent-badge.tb-agent-done")).toHaveCount(0);
		await expect(tic003Card.locator(".tb-agent-badge.tb-agent-queued")).toHaveCount(0);
		await expect(tic003Card.locator(".tb-agent-badge.tb-agent-done[aria-label='done']")).toHaveCount(1);

		await openAgents(page);

		const rows = page.locator(".tb-agents-list .tb-agent-row");
		await expect(rows).toHaveCount(2);

		const queuedRow = (id) => rows.filter({ has: page.locator(".tb-ticket-id", { hasText: id }) });
		const tic001Row = queuedRow("TIC-001");
		const tic002Row = queuedRow("TIC-002");
		const tic003Row = queuedRow("TIC-003");

		await expect(tic001Row).toHaveCount(1);
		await expect(tic002Row).toHaveCount(1);
		await expect(tic003Row).toHaveCount(0);

		await expect(tic001Row.locator(".tb-agent-badge.tb-agent-queued[aria-label='queued']")).toHaveCount(1);
		await expect(tic002Row.locator(".tb-agent-badge.tb-agent-queued[aria-label='queued']")).toHaveCount(1);
		await expect(tic001Row.locator(".tb-agent-session")).toContainText("Queued ");
		await expect(tic002Row.locator(".tb-agent-session")).toContainText("Queued ");
		await expect(tic002Row).not.toContainText("001-execute");
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

async function openAgents(page) {
	await expect
		.poll(
			async () =>
				page.evaluate(() => Boolean(window.app?.commands?.commands?.["tickets-board:open-tickets-agents"])),
			{ timeout: 60_000 },
		)
		.toBe(true);

	await page.evaluate(async () => {
		const leaves = window.app.workspace.getLeavesOfType("tickets-agents");
		if (leaves.length === 0) {
			await window.app.commands.executeCommandById("tickets-board:open-tickets-agents");
		}
	});

	await expect(page.locator(".tb-agents-container")).toBeVisible();
}
