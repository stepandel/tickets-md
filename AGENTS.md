# AGENTS.md

Rules for AI coding agents (Claude Code, Codex, Aider, …) working in
this repository. This file is the agent-facing counterpart of
`README.md`: it codifies the invariants, layer boundaries, and
canonical commands that keep the harness coherent. **Read it before
editing.** When you break a rule here, add a test that would have
caught it and update this file.

## Project map

- `cmd/tickets/main.go` — CLI entry point. Wires cobra commands; no
  business logic.
- `internal/config/` — loads `.tickets/config.yml`. Leaf package.
- `internal/userconfig/` — loads `~/.config/tickets/config.yml`. Leaf
  package.
- `internal/stage/` — loads per-stage `.stage.yml` and renders agent
  prompts. Leaf package.
- `internal/ticket/` — `Ticket` struct, markdown+frontmatter
  (de)serialize, and `Store` (FS-backed CRUD across stage dirs). Owns
  everything under `.tickets/<stage>/*.md`.
- `internal/agent/` — persistent run state under
  `.tickets/.agents/<id>/<run>.yml`, the `Status` state machine, the
  PTY runner, and the reconciliation monitor. **Owns everything under
  `.tickets/.agents/`**; ticket-scoped runs live under
  `.tickets/.agents/<ticket-id>/`, board cron runs under
  `.tickets/.agents/.cron/<name>/`. No other package may read or
  write there.
- `internal/worktree/` — git worktree creation/removal under
  `.worktrees/<id>`. **Owns everything under `.worktrees/`**; no other
  package may read or write there.
- `internal/terminal/` — WebSocket server that brokers live PTY
  access to the Obsidian plugin. Depends on `agent`.
- `internal/obsidian/` — embeds the companion Obsidian plugin's build
  artefacts (`assets/main.js`, `manifest.json`, `styles.css`) and
  installs them into a vault. Leaf package. The assets are regenerated
  by `make plugin-bundle`, which bundles `obsidian-plugin/` via npm +
  esbuild and copies the three files into `assets/`.
- `internal/cli/` — cobra subcommands (one file per command), the
  `watch` loop, board-level cron scheduler, and the glue between
  everything above.
- `internal/archtest/` — test-only package. Asserts the layer rules
  below with `go/packages`.

## Layer rules

Dependency direction is one-way:

```
cmd/tickets
    └── internal/cli
            ├── internal/ticket ── internal/config
            │                   └─ internal/stage
            ├── internal/agent  ── internal/config
            ├── internal/worktree
            ├── internal/terminal ── internal/agent
            ├── internal/obsidian
            ├── internal/stage
            ├── internal/config
            └── internal/userconfig
```

Concretely:

- `cmd/tickets` imports `internal/cli` only.
- `internal/cli` may import any other `internal/*` package, but no
  other `internal/*` package may import `internal/cli` — it is a leaf
  sink.
- `internal/terminal` may only be imported by `internal/cli`.
- `internal/ticket`, `internal/stage`, `internal/config`,
  `internal/userconfig` must not import `internal/cli`,
  `internal/terminal`, `internal/agent`, or `internal/worktree`.
  They are the shared, agent-free primitives.
- `internal/agent` must not import `internal/ticket`,
  `internal/worktree`, `internal/terminal`, or `internal/cli`. It
  sits alongside `internal/ticket`, not on top of it.
- `internal/worktree` is a leaf: no internal imports.
- `internal/obsidian` is a leaf: no internal imports. Only `internal/cli`
  may import it.

These rules are enforced mechanically by
`internal/archtest/arch_test.go`. If you need to cross a boundary,
fix the design — do not silence the test.

## Canonical invariants

- **Run YAMLs are truth.** `.tickets/.agents/<id>/<run>.yml` is the
  authoritative record of agent lifecycle state. The ticket
  frontmatter's `agent_status`, `agent_run`, `agent_session` fields
  are a cache projected from the latest run YAML by
  `syncAgentFrontmatter`. Never write the cache without also updating
  the YAML.
- **Status transitions go through `agent.Transition`.** The state
  machine is: `spawned → running|done|errored|failed`, `running →
  done|failed|blocked`, `blocked → running|done|failed`. Terminal
  states (`done`, `failed`, `errored`) have no outbound edges. Use
  `agent.Write`, which validates the transition; use
  `agent.SetPlanFile` only for schema backfill that does not change
  `status`.
- **Writes to run YAMLs are atomic.** Updates go through
  temp-file + `os.Rename`. New runs use `O_EXCL` so two spawners
  racing on the same run id fail loudly instead of overwriting.
- **Stage is derived from the directory**, never stored in frontmatter.
  `ticket.Store.Move` is a `rename(2)` between sibling stage dirs.
- **The store is a single hidden directory** (`.tickets/`), mirroring
  `.git/`. No daemon, no database.

## Canonical commands

Before declaring any task done, run:

```sh
make check
```

This runs `go build ./...`, `go vet ./...`, and `go test ./...`.
The archtest layer check runs as part of `go test ./...`.

For behavior changes, also run the specific test file:

```sh
go test ./internal/<pkg>/... -run <TestName> -v
```

If you change `obsidian-plugin/`, also run:

```sh
make plugin-test
```

CI additionally exercises the plugin end-to-end via `make qa-plugin`
against a pinned Obsidian AppImage (`OBSIDIAN_VERSION` in
`.github/workflows/ci.yml`). Bump that env var when the pin goes stale.

For the QA harness itself, run:

```sh
make qa-cli
make qa-plugin   # requires Obsidian; set OBSIDIAN_BIN if auto-detect misses it
```

`make qa` runs both harnesses together. `make qa-plugin` exits clearly
when Obsidian is unavailable so the skip can be recorded instead of
quietly passing.
CI always runs `make qa-cli`, and also runs `make qa-plugin` under
`xvfb-run` against the Obsidian AppImage pinned in
`.github/workflows/ci.yml` via `OBSIDIAN_VERSION`. Bump that workflow
env var when the CI Obsidian pin needs to change.

For a full rebuild of the installed binary:

```sh
make install
```

## Conventions

- Go module path is `github.com/stepandel/tickets-md`. All internal
  imports are `github.com/stepandel/tickets-md/internal/<pkg>`.
- Keep command files (`internal/cli/*.go`) thin wrappers around
  `ticket.Store` and the other internal packages. Business logic
  belongs in the internal packages, not in cobra handlers.
- Do not add features that require a daemon or a database. The
  filesystem is the database; `tickets watch` is the only long-lived
  process, and it is optional.
- Markdown frontmatter schema lives on `ticket.Ticket` — extend it
  there, not by parsing the body or inventing sidecar files.
- New subcommands go in their own file under `internal/cli/` and
  are registered in `internal/cli/root.go`.
- Default to writing no comments in Go code. Only add a comment when
  the *why* is non-obvious (a hidden constraint, a workaround, a
  surprising invariant). Do not restate what well-named code already
  says.
- When moving tickets between stages, prefer the CLI (`tickets move
  <id> <stage>` or `go run ./cmd/tickets -C ../.. move <id> <stage>`
  from a worktree) instead of raw `mv`. It preserves the store-level
  invariant that stage changes go through `ticket.Store.Move` and
  avoids repeated per-file `mv` approval churn.

## Agent integrations

`internal/agent/integration.go` defines an optional per-agent hook
(`PrepareArgs` before spawn, `ExtractPlan` after exit). Agents with no
integration still work — they run as plain subprocesses configured via
`.stage.yml`. The interface exists so core code never hardcodes a
specific agent's name.

Only **Claude Code** has an integration today. It fits because Claude
lets the caller pre-generate a session UUID (`--session-id`), persists
its transcript at a path derivable from cwd + session id, and has a
first-class plan mode that writes to `~/.claude/plans/` — so we can
link a run back to its plan file deterministically.

**Codex CLI does not have an integration, and that is intentional.**
Codex auto-generates its own thread ids (not injectable), has no plan
file concept, and its rollout filenames are not predictable from
inputs. Forcing it through the current interface would require
inventing conventions Codex does not have. Codex runs fine as a plain
subprocess; add an integration only when there is a concrete feature
we want Codex runs to gain (e.g. capturing its thread id from stdout
for `thread/resume`-style followups) — and expect the interface to
grow a new hook at that point rather than shoehorning into the two
existing ones.

Rule of thumb: an integration must deliver a user-visible feature
(like `tickets agents plan`). An empty or speculative integration is
worse than none — it advertises capability that does not exist.

## Agent lifecycle (for orientation)

1. `tickets watch` observes a `Create` event in a stage directory.
2. If the stage's `.stage.yml` has `agent:`, the watcher calls
   `agent.NextRun` to pick the next `<seq>-<stage>` run id.
3. A `StatusSpawned` run YAML is written, then the PTY session is
   started (optionally inside a per-ticket worktree).
4. The monitor (`internal/agent/monitor.go`) polls every 5s,
   promoting `spawned → running`, demoting long-idle `running →
   blocked`, and marking disappeared sessions `failed`.
5. On session exit, the `waitForSession` goroutine writes the
   terminal status and `syncAgentFrontmatter` projects it onto the
   ticket file.
6. `tickets doctor` is the offline counterpart: it can be run
   without `watch` and catches drift (stale non-terminal runs,
   orphan agent dirs, orphan worktrees, frontmatter drift).

## When you break a rule

If you had to reach across a layer boundary, modify a run YAML
without going through the transition, or special-case an invariant,
either:

1. The rule is wrong — update this file and justify the change in
   the commit message. **Or:**
2. The rule is right — add a test that would have caught the
   violation, then undo the shortcut.

Silent exceptions will cause later agents to repeat the mistake.
