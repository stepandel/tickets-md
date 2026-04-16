# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
  tickets already sitting in a configured `complete_stages` stage,
  complementing the move-time behavior.

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

[Unreleased]: https://github.com/stepandel/tickets-md/compare/v0.1.5...HEAD
[0.1.5]: https://github.com/stepandel/tickets-md/compare/v0.1.4...v0.1.5
[0.1.4]: https://github.com/stepandel/tickets-md/compare/v0.1.3...v0.1.4
[0.1.3]: https://github.com/stepandel/tickets-md/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/stepandel/tickets-md/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/stepandel/tickets-md/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/stepandel/tickets-md/releases/tag/v0.1.0
