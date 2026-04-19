# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

- Every `tickets` subcommand except `tickets watch` now auto-detects
  the main repo ticket store when invoked from a linked git worktree,
  unless `-C` was passed explicitly. Ticket mutations (`move`, `edit`,
  `set`, `label`, `rm`, `archive`, `new`, `followup`, `run`), reads
  (`show`, `list`, `board`, `doctor`), and agent-state commands
  (`agents`, `crons`, `projects`, `link`, `cleanup`, `watch pause`,
  `worktree …`) all operate on the main repo store from either
  location. `tickets watch` is deliberately excluded so the daemon
  keeps owning its `.tickets/.terminal-server` socket, fsnotify
  watches, and PTYs in the main repo.
- `tickets watch` now refuses to start from a linked git worktree
  unless `-C` is passed explicitly. The error points at the main repo
  root and keeps the daemon from silently competing with the main-repo
  watcher over `.tickets/.terminal-server`, fsnotify watches, and PTY
  sessions.
- `tickets labels create <name>` adds a configured label to
  `.tickets/config.yml` with the default chip color `#6b7280`, so the
  CLI no longer has to edit the config file by hand before assigning a
  new label. Creation rejects empty names, the reserved label `none`,
  and case-insensitive duplicates by reporting the existing configured
  key. `tickets label` and `tickets new --label` remain strict and
  still fail on unknown labels instead of creating them implicitly.
- `tickets labels delete <name>` removes a configured label from
  `.tickets/config.yml`. By default it fails when any tickets still
  carry the label and prints the first few carrier IDs so you can scrub
  them with `tickets unlabel`. Pass `--force` to drop only the config
  entry and leave existing ticket frontmatter intact; those leftover
  labels remain visible on the board and in `tickets labels --on <id>`
  as unconfigured labels until you remove them or recreate the entry.
- `tickets watch` now detects when a configured stage directory is
  removed, renamed, or recreated on the filesystem without a matching
  `stages:` edit. The watcher drops the watch and clears the cached
  stage config when the directory disappears, and re-adds the watch,
  reloads `.stage.yml`, and re-seeds its known tickets when the
  directory reappears — no config edit or restart required.
- `tickets watch` now reconciles the set of watched stage directories
  when `stages:` is edited in `.tickets/config.yml`. Newly added
  stages are created with a default `.stage.yml` and start being
  watched; removed stages stop being watched. The change is applied
  on the next config-reload debounce, so no restart is required.
- The Obsidian board view now has a filter input in its header that
  narrows visible cards by ticket id, title, priority, project,
  labels, stage, agent status, and linked ticket ids. Queries are
  case-insensitive and tokenized on whitespace, with `"double quoted"`
  phrases matched as a single term so a title or label fragment that
  contains spaces can be filtered on directly. Every token must match
  somewhere across those fields. The query is persisted per board
  leaf across reopen/reload, and empty stages render a "No matching
  tickets" hint while a filter is active.
- `tickets crons stop <name>` terminates an active cron agent PTY
  session through the running watcher, marking the run `failed` with
  a `terminated by user` error. The Obsidian agents view exposes the
  same action as a "Stop session" menu entry on cron rows with a
  live session. Useful for killing an interactive cron run without
  attaching to the terminal.
- `tickets watch` now auto-reloads per-stage `.stage.yml` files while
  the watcher is running. Editing, creating, or removing a stage's
  `.stage.yml` is picked up on the next debounce without a restart;
  if the reloaded file fails to parse, the previous config is kept
  and the watcher keeps running.
- `tickets watch` now supports an optional `watch.idle_kill_after`
  threshold in `.tickets/config.yml`. When set, the monitor SIGTERMs
  the PTY once its output has been silent for at least that long and
  marks the run `failed` with a `session killed after Ns idle`
  error. Durations use Go's `time.ParseDuration` syntax, must be
  ≥ 1s, and must be at least as long as `idle_block_after` when both
  are set. The setting is hot-reloaded alongside the other `watch:`
  timings.
- `tickets watch` gained `pause [reason]`, `resume`, and `status`
  subcommands. Pausing writes `.tickets/.watch-paused`, which causes
  the running watcher to skip agent spawns on stage arrivals, stage
  reruns, and cron ticks until `resume` clears the file. In-flight
  sessions are unaffected.
- The Obsidian plugin's Board and Agents views now surface the
  watcher pause state and a pause/resume toggle in their headers.
  The control is backed by new `GET /watch/status`,
  `POST /watch/pause`, and `POST /watch/resume` endpoints on the
  terminal server exposed by `tickets watch`, which read and write
  the same `.tickets/.watch-paused` sentinel the CLI uses so both
  control paths stay coherent.
- `cron_agents[]` gained an optional `interactive: true` flag. When
  set, the scheduler skips the cron-specific arg prep (so Claude's
  auto-`--print` is not injected) and the run's PTY stays alive until
  the user closes it. The Obsidian agents view exposes an "Open live
  terminal" menu entry and row click-through that attach to the
  running session via the existing terminal server; while the
  interactive run is alive, subsequent ticks for the same cron are
  skipped.

## [0.1.11] - 2026-04-16

- The `tickets watch` monitor's poll interval (default 5s) and the
  idle-pane threshold that promotes a run to `blocked` (default 30s)
  are now configurable via a new optional `watch:` block in
  `.tickets/config.yml` (`poll_interval`, `idle_block_after`).
  Durations parse with Go's `time.ParseDuration`; `idle_block_after`
  must be ≥ 1s. A running watcher now hot-reloads those timing edits
  from `.tickets/config.yml` without requiring a restart.

- Board priority styling is now config-driven via an optional
  `priorities:` map in `.tickets/config.yml`. Each key is a priority
  name matched case-insensitively; values accept `color` (hex) and
  optional `bold`. When the map is omitted, the built-in defaults for
  `critical`, `urgent`, `high`, `medium`, `med`, and `low` are used so
  existing boards render unchanged.
- The CLI board's `p` priority picker and the Obsidian board's "Set
  priority" picker now both read optional `priorities.<name>.order`
  values from `.tickets/config.yml`. Ordered entries appear first by
  ascending number, unordered entries follow by normalized name, and
  `none` stays last. Ticket sorting is unchanged.
- `priorities:` now rejects `none` as a key (matched case-insensitively
  with whitespace trimmed) because the picker reserves it for the
  cleared-priority option. Config load fails with a `priority "…" is
  reserved` error instead of silently shadowing the built-in entry.

- Per-ticket git worktrees are now configurable via an optional
  `worktrees:` block in `.tickets/config.yml`. `worktrees.dir` sets
  the directory where worktrees live (default `.worktrees`) and
  `worktrees.branch_prefix` sets the branch namespace (default
  `tickets/`, must end with `/`). Omit the block to keep the
  existing defaults. The Obsidian plugin's diff view reads the same
  block, so the worktree path it inspects and the branch range it
  diffs against follow the configured layout instead of the
  hardcoded `.worktrees/<id>` and `tickets/<id>` defaults.

- Board-level cron runs of the `claude` agent now auto-inject
  `--print` (unless the user already passed `--print`/`-p` in the
  cron's `args:`) so Claude exits after producing its response instead
  of hanging in interactive mode. Stage-agent runs are unaffected.
  Other agents are unaffected; only the Claude integration opts into
  the new optional `CronIntegration` hook in `internal/agent/`.
- `tickets watch` now logs an advisory warning when a configured cron
  command has neither cron-specific integration support nor cron-only
  `args:`. Non-Claude cron agents already support one-shot mode via
  `cron_agents[].args`; the warning makes that requirement visible
  before a long-running command causes later ticks to be skipped.

- Added optional `archive_stage` config, a `tickets archive` command,
  and `--archived` toggles for the default CLI board/list views while
  keeping archived tickets in the normal filesystem-backed store.
- The Obsidian board now mirrors the CLI's archive visibility: when
  `archive_stage` is configured, the stage is hidden by default and a
  "Show archived stage" / "Hide archived stage" toggle is available
  from the board menu. The choice is persisted per board leaf across
  reopen/reload.

- `tickets board` and the Obsidian plugin can now force a stage-agent
  re-run while a previous run is still active. In the TUI, `F` opens a
  confirm overlay naming the active session and only kills it on `y`;
  the Obsidian board and Agents views expose a "Force re-run stage
  agent" context-menu entry that opens a confirmation modal before
  superseding the run. The superseded run is marked `failed` with
  `superseded by force re-run`. The underlying
  `POST /rerun-stage-agent` endpoint gains an optional `"force": true`
  flag; without it the old "agent already running" error still wins.
- Board cards in the Obsidian plugin now expose a "View run log"
  context-menu action for tickets with a completed agent run
  (done/failed/errored), mirroring the Agents view affordance. The
  live "Open terminal" item stays desktop-only and unchanged; replay
  works on desktop and mobile.
- The pre-commit hook installed by `tickets hooks install` now also
  runs `make plugin-test` when staged files include `obsidian-plugin/`,
  encoding the AGENTS.md rule that plugin changes require both the Go
  and plugin suites. Commits that don't touch `obsidian-plugin/` still
  run only `make check`. Users with the previous hook installed need
  to re-run `tickets hooks install --force` to pick up the new script.
- Fixed a second PTY-session exit-status race where `PTYRunner.Shutdown`
  could win the lookup→delete race against the owning CLI waiter and
  cause a cleanly-exited session to be persisted as `failed`. Shutdown
  now waits on the session's internal done channel instead of calling
  `Wait`, leaving the map entry for the owning waiter to consume.
  Complements the 0.1.9 fast-exit fix.

## [0.1.10] - 2026-04-16

### Fixed

- The Obsidian plugin manifest is now stamped with the release version
  at build time, so `tickets watch` no longer logs a spurious
  `obsidian plugin updated 0.1.0 → 0.1.0` reinstall on every startup
  after upgrading the CLI.

## [0.1.9] - 2026-04-15

### Added

- `tickets watch` now auto-updates the Obsidian plugin at startup when
  the installed version doesn't match the CLI version, removing the
  need to manually run `tickets obsidian install` after upgrading.

### Fixed

- Fixed a race condition where fast-exiting PTY sessions (e.g. cron
  agents running `exit 0`) could be incorrectly marked as `failed`
  instead of `done`. The cleanup goroutine was removing the session
  from the map before `Wait()` could read the exit status.

## [0.1.8] - 2026-04-15

- `.tickets/config.yml` is now tracked in Git (previously ignored).
  `tickets init` writes a `.gitignore` block that whitelists the store
  config alongside stage configs. Existing repos with the older
  TIC-084 gitignore block are upgraded automatically by
  `EnsureGitignored`.

- `tickets watch` now hot-reloads `cron_agents:` when
  `.tickets/config.yml` changes, so cron schedule edits take effect
  without restarting the watcher.

- `tickets crons run <name>` manually fires a configured cron agent
  through the running watcher. The Obsidian plugin's cron agent menu
  also gains a "Run now" action (desktop only).

- `tickets crons add` and `tickets crons rm` let you create and remove
  cron agent entries from the CLI. `add` accepts `--name`, `--schedule`,
  `--command`, `--prompt` (all required), plus optional `--arg` (repeatable)
  and `--disabled`. `rm` removes the config entry and notes any kept run
  history that `tickets doctor --fix` can prune.

- `tickets crons` now has `enable`, `disable`, and `set` subcommands for
  managing cron agent configuration from the CLI without editing
  `config.yml` by hand. `set` supports the `schedule`, `command`,
  `prompt`, and `args` fields; pass `-` as the value to clear `args`.

- Cron agents no longer race the monitor into false `failed` states
  while starting, and fast-exiting runs now complete as `done`
  instead of getting stuck at `spawned` or being misreported as
  disappeared.

- `tickets new --project` is now validated before the ticket is created,
  so an unknown project ID no longer leaves an orphaned ticket on disk.

- `tickets new --body` now recognizes `\n`, `\r`, `\t`, and `\\` escape
  sequences in the flag value, so shell-friendly one-line invocations
  save as multi-line markdown bodies. Unknown `\X` sequences are left
  unchanged; `\\n` is the escape hatch for a literal two-character `\n`.

- Fixed the Obsidian plugin's diff view overcounting changed files on
  ticket branches. It now compares against the remote-tracking
  `origin/main` (not a possibly stale local `main`) and uses
  three-dot branch-diff semantics so commits already merged into the
  base no longer show up in the ticket's diff.

- `tickets new` now validates `--parent`, `--blocked-by`, `--blocks`,
  and `--related` targets against the store before creating the
  ticket, so a typo or unknown ID no longer leaves an orphaned ticket
  on disk. Empty IDs, intra-flag duplicates, and the same peer reused
  across conflicting relation roles are also rejected.

- Clicking an agent row in the Obsidian plugin's Agents view now opens
  the live PTY terminal when the ticket has an active agent session,
  instead of opening the ticket file.

- The Obsidian plugin's Agents view now supports terminal replay for
  completed agent runs. Clicking a finished (done/failed/errored) run
  opens its PTY log in a read-only terminal pane; the context menu also
  gains a "View run log" action.

- Board cards in the Obsidian plugin no longer show a project chip;
  use the card context menu for project assignment.

- The Obsidian board card menu can now assign a ticket to a project,
  change its project, or remove `project:` without editing
  frontmatter by hand.

- The Obsidian plugin's Projects view row menu can now assign
  tickets to a project via a fuzzy picker, complementing the
  board-card project actions.

- The Obsidian plugin's Projects view now splits into two panes:
  the existing project list on the left and a tickets sidebar on
  the right that shows the selected project's assigned tickets.
  Clicking a project selects it (instead of opening its file);
  clicking a ticket opens it in the preview leaf. The "Open
  project" button in the sidebar header keeps the old open-file
  affordance. Narrow windows stack the panes vertically.

- The Obsidian plugin's Agents view now lists cron agents defined in
  `.tickets/config.yml` alongside stage agents, with actions to edit
  the cron entry, enable/disable it, and open the latest run log.
  Config edits round-trip previously untouched keys
  (`complete_stages`, `default_agent`, `cleanup`, `cron_agents`) so
  stage operations no longer drop them.

- The Obsidian plugin now has a Projects view: a ribbon icon,
  `Open Tickets Projects` command, and row context menu for
  creating, renaming, setting status, listing assigned tickets, and
  deleting projects under `.tickets/projects/`. Deleting a project
  unassigns `project:` on its member tickets first, matching the
  CLI's `DeleteProject`.

- Boards can now opt into `complete_stages` in `.tickets/config.yml`.
  When a ticket enters one of those stages — via `tickets move` or a
  filesystem move picked up by `tickets watch` — it automatically
  stops blocking its dependent tickets.

- `tickets doctor` now clears stale `blocks` / `blocked_by` entries on
  tickets already sitting in a configured `complete_stages` stage, the
  offline repair path for stores where those moves happened without a
  running watcher.

- The CLI now checks for a newer GitHub release at most once per 24
  hours on interactive runs, caches the result in the per-user
  config, and prints a one-line stderr upgrade reminder. Set
  `TICKETS_NO_UPDATE_CHECK=1` to disable it.

## [0.1.7] - 2026-04-14

### Changed

- The `.stage.yml` scaffold and `tickets watch --help` example now
  default `agent.args` to `["--dangerously-skip-permissions"]` instead
  of `["--print"]`, so a freshly-uncommented Claude agent runs without
  being blocked on per-tool approval prompts.

### Fixed

- `make install` (local `go install`) now stamps the binary version as
  `dev`, so `tickets obsidian install` hits the dev-build guidance
  path instead of failing with an opaque "not a tagged release" error
  on pseudo-versions derived from `runtime/debug.ReadBuildInfo`.

### Added

- `make plugin-install` — one-shot target that bundles the Obsidian
  plugin and installs it from the local build, skipping the GitHub
  release fetch. Accepts `VAULT=/path` to target a specific vault.

## [0.1.6] - 2026-04-14

### Changed

- The Obsidian plugin is no longer embedded in the `tickets` binary.
  `tickets obsidian install` now downloads the matching
  `tickets-board-plugin.zip` from the GitHub release for your CLI
  version and caches it under the user cache directory
  (`$XDG_CACHE_HOME/tickets/plugin/<tag>/` on Linux,
  `~/Library/Caches/tickets/plugin/<tag>/` on macOS), so repeat
  installs are offline.
- `tickets obsidian status` now reports the installed plugin version
  against the CLI's expected version (previously reported against an
  embedded bundle version).

### Added

- `tickets obsidian install --from <dir>` installs the plugin from a
  local build directory — the supported flow for plugin development
  and for dev / pseudo-version CLI builds that have no matching
  GitHub release.

## [0.1.5] - 2026-04-13

### Changed

- `tickets obsidian install` now defaults the vault to `.tickets/`
  instead of the repo root, matching the convention that the
  `.tickets/` directory *is* the Obsidian vault.
- `tickets obsidian install` bootstraps the vault directory
  (including `.obsidian/`) if it does not yet exist, so a fresh
  `tickets init` + `tickets obsidian install` pair works end-to-end
  without manual setup.

## [0.1.4] - 2026-04-13

### Added

- Homebrew tap at `stepandel/homebrew-tap`. Install with
  `brew install stepandel/tap/tickets` on macOS or Linux. GoReleaser
  regenerates the formula on every `v*` tag push.

## [0.1.3] - 2026-04-13

### Fixed

- `go.mod` tidy: `github.com/charmbracelet/x/ansi` is a direct
  dependency (used by the TUI) and is now listed as such. CI's
  `go mod tidy -diff` hook caught the drift.
- GoReleaser config migrated from the deprecated
  `archives.format_overrides.format` key to the `formats` array form
  required by GoReleaser v2.

Note: v0.1.2's release run failed on both issues above; v0.1.3 is the
first tag to produce a full GitHub release with binaries.

## [0.1.2] - 2026-04-13

### Fixed

- Release pipeline no longer rebuilds `internal/obsidian/assets/` in
  CI. The esbuild toolchain on GitHub runners produced byte-different
  output from the committed bundle, which left the worktree dirty and
  caused GoReleaser to abort. The committed bundle is the source of
  truth; `make plugin-bundle` still refreshes it locally.

Note: v0.1.1 was tagged but never produced a GitHub release or
binaries because of the CI bug fixed here. `go install …@v0.1.1` does
work — the source is correct, only the release step broke.

## [0.1.1] - 2026-04-13

### Added

- `tickets obsidian install|uninstall|status` — the CLI now embeds
  the companion Obsidian plugin bundle and drops it into a vault's
  `.obsidian/plugins/tickets-board/` directory. Walks up from
  `--vault` (or `--root`) to auto-detect the vault, and appends the
  plugin id to `community-plugins.json` unless `--no-enable` is passed.
  Locking the plugin version to the CLI version it shipped with means
  the WebSocket protocol shared by `tickets watch` and the board can
  evolve without mismatched-pair bugs.
- `make plugin-bundle` rebuilds `obsidian-plugin/` via npm + esbuild
  and copies the artefacts into `internal/obsidian/assets/`, which is
  where `go:embed` picks them up. `make install` and `make release`
  now depend on it.
- GoReleaser pipeline (`.goreleaser.yaml` + `.github/workflows/release.yml`)
  that fires on `v*` tags to cross-compile darwin/linux/windows
  (amd64 + arm64) binaries and attach them to a GitHub release.

### Changed

- Go module path moved from `tickets-md` to
  `github.com/stepandel/tickets-md`, which is a prerequisite for
  `go install github.com/stepandel/tickets-md/cmd/tickets@latest`.
- `tickets --version` now falls back to
  `runtime/debug.ReadBuildInfo` when no `-ldflags` stamp is present,
  so `go install …@vX.Y.Z` binaries report the tag instead of "dev".

## [0.1.0] - 2026-04-13

First public release. Everything below has shipped on `main` over the
course of development.

### CLI (`tickets`)

- Markdown-backed ticket store: every ticket is a `.md` file with YAML
  frontmatter under `.tickets/<stage>/`. Moves between stages are
  ordinary file renames, so `git log` is the audit trail.
- Core commands: `init`, `new` (with `--priority`), `list`, `show`,
  `move`, `edit`, `set`, `rm`, `link` / `unlink` (related,
  blocked_by, blocks), `doctor` (link integrity scan + repair),
  `worktree` (list/open/clean per-ticket git worktrees).
- `tickets board` — a Bubble Tea TUI Kanban board with mouse support,
  inline ticket creation, status badges, and parity with the Obsidian
  card menu.
- `tickets watch` — a long-running file-watcher that spawns the
  configured agent when a ticket arrives in a stage.
- `tickets agents` family: `agents` (list runs), `agents log`,
  `agents plan` (open Claude Code's plan file), `agents run`
  (interactive ad-hoc agent on a ticket), `agents followup`
  (agent-agnostic followup with diff/log replay).
- Persistent run state under `.tickets/.agents/<id>/runs/<run>.yml`
  with a `Status` state machine reconciled on watcher startup.
- `tickets edit` — lazy editor wizard, user-level config at
  `~/.config/tickets/config.yml`.

### Agent harness

- In-process PTY runner (replaces the earlier tmux dependency) with
  fan-out to multiple subscribers and a 64 KB replay buffer for
  late-joining clients.
- Per-ticket git worktrees under `.worktrees/<id>` with a `cleanup`
  stage block that removes the worktree and branch when a ticket
  reaches a terminal stage.
- Stage-level `.stage.yml` describing the agent command, args,
  prompt template, worktree policy, and cleanup rules; templates
  expand `{{id}}`, `{{path}}`, `{{title}}`, and `{{worktree}}`.
- Plan-file resolution through Claude Code's session transcript.

### Obsidian plugin (`tickets-board`)

- Kanban board view that mirrors `.tickets/` stages with
  drag-and-drop, inline ticket creation, priority controls, and a
  delete-confirmation modal.
- Per-ticket context menu: open in editor, set priority, manage
  links, spawn ad-hoc agent, re-run stage agent, view diff.
- Live terminal view backed by a localhost WebSocket bridge from
  `tickets watch`. The bridge accepts only the Obsidian origin.
- Diff view powered by diff2html that compares the worktree against
  the detected default branch.
- Mobile support (best-effort; child-process integrations are gated
  to desktop).

### Project hygiene

- `AGENTS.md` documenting layer rules, invariants, and canonical
  commands; enforced in tests via the `internal/archtest` package.
- MIT License.
- `make check` runs `go vet`, `go build`, `go test`.
- `make release VERSION=x.y.z` stamps the binary version via
  `-ldflags`; `tickets --version` reports it.

[Unreleased]: https://github.com/stepandel/tickets-md/compare/v0.1.11...HEAD
[0.1.11]: https://github.com/stepandel/tickets-md/compare/v0.1.10...v0.1.11
[0.1.10]: https://github.com/stepandel/tickets-md/compare/v0.1.9...v0.1.10
[0.1.9]: https://github.com/stepandel/tickets-md/compare/v0.1.8...v0.1.9
[0.1.8]: https://github.com/stepandel/tickets-md/compare/v0.1.7...v0.1.8
[0.1.7]: https://github.com/stepandel/tickets-md/compare/v0.1.6...v0.1.7
[0.1.6]: https://github.com/stepandel/tickets-md/compare/v0.1.5...v0.1.6
[0.1.5]: https://github.com/stepandel/tickets-md/compare/v0.1.4...v0.1.5
[0.1.4]: https://github.com/stepandel/tickets-md/compare/v0.1.3...v0.1.4
[0.1.3]: https://github.com/stepandel/tickets-md/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/stepandel/tickets-md/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/stepandel/tickets-md/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/stepandel/tickets-md/releases/tag/v0.1.0
