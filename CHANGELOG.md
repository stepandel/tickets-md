# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/stepandel/tickets-md/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/stepandel/tickets-md/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/stepandel/tickets-md/releases/tag/v0.1.0
