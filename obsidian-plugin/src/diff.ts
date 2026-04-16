export type RunGit = (args: string[], cwd: string) => string;

export function resolveDefaultBranch(run: RunGit, cwd: string): string {
	try {
		return run(["rev-parse", "--abbrev-ref", "origin/HEAD"], cwd).trim();
	} catch {
		try {
			run(["rev-parse", "--verify", "main"], cwd);
			return "main";
		} catch {
			return "master";
		}
	}
}

export type DiffPlan =
	| {
		kind: "worktree";
		cwd: string;
		primary: {
			mergeBase: string[];
			diff: (base: string) => string[];
		};
		fallback: {
			diff: string[];
		};
	}
	| {
		kind: "branch";
		cwd: string;
		diff: string[];
	};

export function planDiffCommand(input: {
	ticketId: string;
	basePath: string;
	worktreePath: string;
	worktreeExists: boolean;
	defaultBranch: string;
}): DiffPlan {
	if (input.worktreeExists) {
		return {
			kind: "worktree",
			cwd: input.worktreePath,
			primary: {
				mergeBase: ["merge-base", "HEAD", input.defaultBranch],
				diff: (base: string) => ["diff", `${base}...HEAD`],
			},
			fallback: {
				diff: ["diff", `${input.defaultBranch}...HEAD`],
			},
		};
	}

	return {
		kind: "branch",
		cwd: input.basePath,
		diff: ["diff", `${input.defaultBranch}...tickets/${input.ticketId}`],
	};
}
