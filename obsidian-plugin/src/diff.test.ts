import { test } from "node:test";
import * as assert from "node:assert/strict";

import { planDiffCommand, resolveDefaultBranch, type RunGit } from "./diff";

const cwd = "/repo";

test("resolveDefaultBranch keeps origin/main from origin/HEAD", () => {
	const calls: Array<{ args: string[]; cwd: string }> = [];
	const run: RunGit = (args, callCwd) => {
		calls.push({ args, cwd: callCwd });
		return "origin/main\n";
	};

	assert.equal(resolveDefaultBranch(run, cwd), "origin/main");
	assert.deepEqual(calls, [
		{ args: ["rev-parse", "--abbrev-ref", "origin/HEAD"], cwd },
	]);
});

test("resolveDefaultBranch keeps origin/master from origin/HEAD", () => {
	const run: RunGit = () => "origin/master\n";

	assert.equal(resolveDefaultBranch(run, cwd), "origin/master");
});

test("resolveDefaultBranch falls back to main when origin/HEAD is unavailable", () => {
	const calls: Array<{ args: string[]; cwd: string }> = [];
	const run: RunGit = (args, callCwd) => {
		calls.push({ args, cwd: callCwd });
		if (args[2] === "origin/HEAD") {
			throw new Error("missing origin/HEAD");
		}
		return "deadbeef\n";
	};

	assert.equal(resolveDefaultBranch(run, cwd), "main");
	assert.deepEqual(calls, [
		{ args: ["rev-parse", "--abbrev-ref", "origin/HEAD"], cwd },
		{ args: ["rev-parse", "--verify", "main"], cwd },
	]);
});

test("resolveDefaultBranch falls through to master when main is unavailable", () => {
	const calls: Array<{ args: string[]; cwd: string }> = [];
	const run: RunGit = (args, callCwd) => {
		calls.push({ args, cwd: callCwd });
		throw new Error(`missing ${args.at(-1)}`);
	};

	assert.equal(resolveDefaultBranch(run, cwd), "master");
	assert.deepEqual(calls, [
		{ args: ["rev-parse", "--abbrev-ref", "origin/HEAD"], cwd },
		{ args: ["rev-parse", "--verify", "main"], cwd },
	]);
});

test("planDiffCommand uses merge-base and fallback diff in a worktree with origin/main", () => {
	const plan = planDiffCommand({
		ticketId: "TIC-123",
		basePath: "/repo/.tickets",
		worktreePath: "/repo/.worktrees/TIC-123",
		worktreeExists: true,
		defaultBranch: "origin/main",
	});

	assert.equal(plan.kind, "worktree");
	if (plan.kind !== "worktree") {
		throw new Error("expected worktree diff plan");
	}
	assert.equal(plan.cwd, "/repo/.worktrees/TIC-123");
	assert.deepEqual(plan.primary.mergeBase, ["merge-base", "HEAD", "origin/main"]);
	assert.deepEqual(plan.primary.diff("abc123"), ["diff", "abc123...HEAD"]);
	assert.deepEqual(plan.fallback.diff, ["diff", "origin/main...HEAD"]);
});

test("planDiffCommand preserves local main fallback in worktree mode", () => {
	const plan = planDiffCommand({
		ticketId: "TIC-123",
		basePath: "/repo/.tickets",
		worktreePath: "/repo/.worktrees/TIC-123",
		worktreeExists: true,
		defaultBranch: "main",
	});

	assert.equal(plan.kind, "worktree");
	if (plan.kind !== "worktree") {
		throw new Error("expected worktree diff plan");
	}
	assert.deepEqual(plan.primary.mergeBase, ["merge-base", "HEAD", "main"]);
	assert.deepEqual(plan.fallback.diff, ["diff", "main...HEAD"]);
});

test("planDiffCommand uses tickets/<id> branch diff when no worktree exists", () => {
	const plan = planDiffCommand({
		ticketId: "TIC-123",
		basePath: "/repo/.tickets",
		worktreePath: "/repo/.worktrees/TIC-123",
		worktreeExists: false,
		defaultBranch: "origin/main",
	});

	assert.equal(plan.kind, "branch");
	if (plan.kind !== "branch") {
		throw new Error("expected branch diff plan");
	}
	assert.equal(plan.cwd, "/repo/.tickets");
	assert.deepEqual(plan.diff, ["diff", "origin/main...tickets/TIC-123"]);
});

test("planDiffCommand uses local main fallback when no worktree exists", () => {
	const plan = planDiffCommand({
		ticketId: "TIC-123",
		basePath: "/repo/.tickets",
		worktreePath: "/repo/.worktrees/TIC-123",
		worktreeExists: false,
		defaultBranch: "main",
	});

	assert.equal(plan.kind, "branch");
	if (plan.kind !== "branch") {
		throw new Error("expected branch diff plan");
	}
	assert.deepEqual(plan.diff, ["diff", "main...tickets/TIC-123"]);
});

test("planDiffCommand always emits a three-dot worktree diff range", () => {
	const plan = planDiffCommand({
		ticketId: "TIC-123",
		basePath: "/repo/.tickets",
		worktreePath: "/repo/.worktrees/TIC-123",
		worktreeExists: true,
		defaultBranch: "origin/main",
	});

	assert.equal(plan.kind, "worktree");
	if (plan.kind !== "worktree") {
		throw new Error("expected worktree diff plan");
	}
	assert.equal(plan.primary.diff("base-sha")[1], "base-sha...HEAD");
});
