```
████████╗██╗ ██████╗██╗  ██╗███████╗████████╗███████╗    ███╗   ███╗██████╗
╚══██╔══╝██║██╔════╝██║ ██╔╝██╔════╝╚══██╔══╝██╔════╝    ████╗ ████║██╔══██╗
   ██║   ██║██║     █████╔╝ █████╗     ██║   ███████╗    ██╔████╔██║██║  ██║
   ██║   ██║██║     ██╔═██╗ ██╔══╝     ██║   ╚════██║ ██╗██║╚██╔╝██║██║  ██║
   ██║   ██║╚██████╗██║  ██╗███████╗   ██║   ███████║ ╚═╝██║ ╚═╝ ██║██████╔╝
   ╚═╝   ╚═╝ ╚═════╝╚═╝  ╚═╝╚══════╝   ╚═╝   ╚══════╝    ╚═╝     ╚═╝╚═════╝
```

# tickets-md

A Linear-style ticket tracker where every ticket is a markdown file and
every stage is a folder. No database; just files you can read, grep,
edit, and commit alongside your code. Agent automation is opt-in —
`tickets watch` runs in a foreground terminal when you want stages
to trigger agents, and there's no background service otherwise.

> **Scoped to a single repository.** `tickets-md` is designed to live
> inside one git repo, right next to the code it describes. The CLI is
> installed globally, but `tickets init` is per-repo: it creates a
> `.tickets/` directory at the repo root (the way `.git/` does), and
> any agents you wire up run only against that repo's checkouts. Use a
> separate `tickets init` for each project.

```
.tickets/
├── config.yml
├── backlog/
│   └── .stage.yml
├── prep/
│   └── .stage.yml
├── execute/
│   ├── .stage.yml        ← agent configured here
│   └── TIC-001.md
├── review/
│   └── .stage.yml
└── done/
```

The whole store lives under a single hidden `.tickets/` directory at
the project root, the same way `.git/` works.

Moving a ticket between stages is just `mv`, and `git log` is your
audit trail. When a stage has an agent configured, `tickets watch`
automatically spawns the agent whenever a ticket arrives.

## Install

The `tickets` binary is installed **once, globally** on your machine.
Everything else — ticket store, stage agents, per-ticket worktrees —
is scoped to the individual repository you run `tickets init` inside.
See [Set up in a project](#set-up-in-a-project) below.

### Homebrew (macOS or Linux)

```sh
brew install stepandel/tap/tickets
```

### Prebuilt binary

Download the archive for your OS/arch from the
[latest release](https://github.com/stepandel/tickets-md/releases/latest),
unpack it, and drop `tickets` somewhere on your `$PATH`
(`/usr/local/bin` or `~/.local/bin` are common choices).

### From source

Requires Go 1.25+.

```sh
go install github.com/stepandel/tickets-md/cmd/tickets@latest
```

`go install` drops the binary in `$(go env GOPATH)/bin` (usually
`~/go/bin`). Add that directory to your `$PATH` if it isn't already:

```sh
echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.zshrc && source ~/.zshrc
```

Building from a local checkout (rebuilds the embedded Obsidian plugin
bundle first):

```sh
git clone https://github.com/stepandel/tickets-md.git
cd tickets-md
make install
```

### Shell completions

`tickets completion <bash|zsh|fish|powershell>` emits a completion
script on stdout. To load it for the current session:

```sh
source <(tickets completion zsh)   # or bash / fish
```

To load it for every new shell, redirect the output to the location
your shell reads on startup. For example, on zsh:

```sh
tickets completion zsh > "${fpath[1]}/_tickets"
```

On bash:

```sh
tickets completion bash > /etc/bash_completion.d/tickets
```

On fish:

```sh
tickets completion fish > ~/.config/fish/completions/tickets.fish
```

## Set up in a project

`tickets init` is meant to run inside the **git repository you're
actually working on**. The whole store lives under `.tickets/` at the
repo root — same way `.git/` does — so ticket files get committed
alongside the code they describe, and agents you wire up operate
only on checkouts of that repo.

```sh
cd ~/code/my-app              # a git repo with code you work on
tickets init                  # creates ./.tickets/ + stage folders
git add .tickets && git commit -m "chore: add tickets-md store"
```

Running `tickets init` outside a git repo works — `.tickets/` is just
a directory — but you lose two useful properties:

- **Scoping of agents.** When a stage has `worktree: true`, each
  agent run is spawned inside a fresh `git worktree` under
  `.worktrees/<ticket-id>` on a dedicated `tickets/<ticket-id>`
  branch, so concurrent agents can't trample each other and
  experimental changes stay isolated from `main` until you merge
  them. Without a git repo, agents run directly against the
  directory with no isolation.
- **Audit trail.** Ticket moves are file renames, so `git log --stat
  .tickets/` shows who moved what when and from which stage to which.

A second, unrelated project gets its own `.tickets/` store — they
don't share state, and an agent configured under one project's
stage directory will never fire on a ticket in another project.

## Quick start

```sh
cd ~/code/my-app              # inside a git repo
tickets init
tickets new "Fix login bug on Safari"
tickets new "Add dark mode toggle"
tickets list
tickets move TIC-001 execute
tickets show TIC-001
tickets edit TIC-001          # opens your editor (see "Editor" below)
tickets rm TIC-002            # prompts for confirmation
```

Use `-C <path>` to operate on a store that isn't the current directory:

```sh
tickets -C ~/code/acme list
```

## Commands

| Command                                 | What it does                                       |
| --------------------------------------- | -------------------------------------------------- |
| `tickets init`                          | Create `.tickets/config.yml` + stage folders       |
| `tickets new <title...> [--priority P]` | Create a ticket in the default stage               |
| `tickets list [--stage X]`              | List tickets, grouped by stage (alias: `ls`)       |
| `tickets show <id>`                     | Print a ticket's contents                          |
| `tickets move <id> <stage>`             | Move a ticket to another stage (alias: `mv`)       |
| `tickets edit <id>`                     | Open the ticket file in your editor                |
| `tickets set <id> <field> <value...>`   | Update a scalar field (`priority`, `title`)        |
| `tickets rm <id> [--force]`             | Delete a ticket                                    |
| `tickets link <a> <b> [--blocks]`       | Link two tickets (related, or directional blocks)  |
| `tickets unlink <a> <b> [--blocks]`     | Remove a link                                      |
| `tickets doctor [--dry-run]`            | Scan for drift across tickets, runs, worktrees     |
| `tickets board`                         | Interactive kanban board TUI (alias: `tui`)        |
| `tickets watch`                         | Watch for ticket movements and spawn agents        |
| `tickets agents [-a] [--history]`       | List agent runs                                    |
| `tickets agents log <id> [run]`         | Print the captured output for a run                |
| `tickets agents plan <id> [run]`        | Open the plan file written by a Claude Code run    |
| `tickets agents followup <id> [--run R] [--message M]` | Spawn a followup session with prior run's context |
| `tickets agents run <id>`               | Start an interactive agent session for a ticket    |
| `tickets worktree list`                 | List active per-ticket git worktrees (alias: `wt`) |
| `tickets worktree open <id>`            | Open a ticket's worktree in your editor            |
| `tickets worktree clean [ids...\|--all]`| Remove worktrees                                   |
| `tickets completion <shell>`            | Emit a shell completion script                     |
| `tickets hooks install [--force]`       | Install a pre-commit hook that runs `make check`   |
| `tickets obsidian <install\|status\|uninstall>` | Manage the bundled Obsidian companion plugin |

`init` accepts `--prefix` and `--stages` to override the defaults at
creation time. When run interactively without `--stages`, it walks
you through naming the stage folders:

```
$ tickets init
Set up the stages for your ticket store.
Defaults: backlog, prep, execute, review, done
Use defaults? [Y/n]: n

Enter stage names one at a time. The first stage is the
default for new tickets. Submit a blank line when done.

  Stage 1: backlog
  Stage 2: triage
  Stage 3: in-progress
  Stage 4: review
  Stage 5: shipped
  Stage 6:

5 stages: backlog → triage → in-progress → review → shipped
```

Pass `--stages new,doing,done` (or pipe stdin from a script) to skip
the wizard.

## Agents

Stages can be configured to automatically spawn a CLI agent (Claude
Code, Codex, Aider, etc.) when a ticket arrives. This turns your
ticket board into an orchestration layer: move a ticket to `execute`
and an AI agent picks it up.

### Setup

`tickets init` scaffolds a `.stage.yml` in every stage directory with
a commented-out example. To activate an agent, open the file and
uncomment:

```yaml
# .tickets/execute/.stage.yml
agent:
  command: claude
  args: ["--print"]
  worktree: true              # isolate work in .worktrees/<id> on branch tickets/<id>
  base_branch: main           # branch to create the worktree from (default: HEAD)
  prompt: |
    You are working in {{worktree}} on branch tickets/{{id}}.
    Read the ticket at {{path}} and implement what it describes.
```

- **command** — the CLI binary to invoke (`claude`, `codex`, `aider`,
  etc.)
- **args** — extra flags placed before the prompt (e.g. `["--print"]`
  for non-interactive mode)
- **worktree** — when true, each run gets its own git worktree under
  `.worktrees/<ticket-id>` on a `tickets/<ticket-id>` branch, so
  concurrent agents don't trample one another's changes
- **base_branch** — the branch the worktree is cut from
- **prompt** — a template string rendered with ticket metadata and
  passed as the final argument

Template variables available in the prompt:

| Variable        | Value                                                |
| --------------- | ---------------------------------------------------- |
| `{{path}}`      | Absolute path to the ticket file                     |
| `{{id}}`        | Ticket ID (e.g. `TIC-001`)                           |
| `{{title}}`     | Ticket title from frontmatter                        |
| `{{stage}}`     | Destination stage name                               |
| `{{body}}`      | Ticket body (markdown after frontmatter)             |
| `{{worktree}}`  | Absolute path to the worktree (empty if disabled)    |
| `{{links}}`     | Human-readable summary of the ticket's links         |

A stage can also be configured for automatic **cleanup** on ticket
arrival — useful for a "done" stage that should release git artifacts
without manual `tickets worktree clean`:

```yaml
# .tickets/done/.stage.yml
cleanup:
  worktree: true    # remove .worktrees/<id>
  branch: true      # delete tickets/<id>
```

### Running the watcher

Start the watcher in a dedicated terminal:

```sh
tickets watch
```

```
2026/04/09 18:15:20 watching backlog/ (no agent)
2026/04/09 18:15:20 watching prep/ (no agent)
2026/04/09 18:15:20 watching execute/ (agent: claude)
2026/04/09 18:15:20 watching review/ (no agent)
2026/04/09 18:15:20 watching done/ (no agent)
2026/04/09 18:15:20 ready — move tickets between stages to trigger agents (ctrl+c to stop)
```

Then in another terminal:

```sh
tickets move TIC-001 execute
```

The watcher detects the arrival, spawns the agent in a PTY, and
streams its output to a per-run log:

```
2026/04/09 18:15:21 TIC-001 → execute: agent running (view with: tickets agents log TIC-001) [worktree: tickets/TIC-001]
2026/04/09 18:15:45 TIC-001/001-execute: agent claude finished (session TIC-001-1 closed)
```

Run artifacts live under `.tickets/.agents/<ticket>/`:

```
.tickets/.agents/TIC-001/
├── 001-execute.yml          # run status: spawned/running/blocked/done/failed
└── runs/
    └── 001-execute.log      # captured PTY output
```

The ticket's frontmatter is also updated with `agent_status`,
`agent_run`, and `agent_session` so the Obsidian view always reflects
the latest run without re-reading the YAML.

Multiple agents can run concurrently for different tickets. The
watcher also picks up manual file moves (`mv`, Finder, git) — it
watches the filesystem directly, not just the `tickets move` command.

### Monitoring agents

List currently active agent runs:

```sh
tickets agents              # non-terminal runs only
tickets agents -a            # include completed and failed
tickets agents --history     # one row per run (not just latest per ticket)
```

Print a run's captured output:

```sh
tickets agents log TIC-001                # latest run
tickets agents log TIC-001 002-execute    # specific run
```

Or tail the raw log file directly:

```sh
tail -f .tickets/.agents/TIC-001/runs/001-execute.log
```

If the agent was Claude Code running in plan mode, open the plan file
it produced:

```sh
tickets agents plan TIC-001
```

### Follow-up sessions

Spawn a fresh agent session enriched with the previous run's git diff,
PTY log, and ticket body — useful for "one more tweak" iterations:

```sh
tickets agents followup TIC-001 --message "also add tests"
tickets agents followup TIC-001 --run 002-execute
tickets agents followup TIC-001                       # interactive, context only
```

### On-demand agents

For tickets that aren't wired to a stage, you can launch an agent
manually in the current terminal:

```sh
tickets agents run TIC-001
```

This reads the full ticket into the prompt and tells the agent to wait
for your first message before acting. Configure the command in
`.tickets/config.yml`:

```yaml
default_agent:
  command: claude
  args: []
```

## Doctor

`tickets doctor` is the offline sweep that catches drift the watcher
might miss — link integrity, stale agent runs, orphan worktrees, and
ticket frontmatter that disagrees with the authoritative run YAMLs.

By default it fixes every issue it finds. Pass `--dry-run` to preview,
or `--stale-after=<duration>` to change the age at which a
non-terminal run is considered abandoned (default `24h`). `--auto`
runs the non-destructive subset (frontmatter drift, orphan `.tmp`
files) silently — the same pass `tickets watch` runs at startup.

```sh
tickets doctor              # fix everything
tickets doctor --dry-run    # preview
tickets doctor --auto       # safe subset, no output
tickets doctor --stale-after=6h
```

The checks are:

- **Link integrity** — dangling, one-sided, or self-referential links
  between tickets.
- **Stale runs** — non-terminal run YAMLs whose `updated_at` is older
  than `--stale-after`; flipped to `failed`.
- **Orphan agent dirs** — `.tickets/.agents/<id>/` directories whose
  ticket no longer exists; removed.
- **Orphan `.tmp` files** — leftover `<run>.yml.tmp` from an
  interrupted atomic rename; removed.
- **Orphan worktrees** — `.worktrees/<id>/` directories whose ticket
  no longer exists; removed.
- **Frontmatter drift** — ticket `agent_status` / `agent_run` /
  `agent_session` that disagrees with the latest run YAML; rewritten.

## Editor

`tickets edit` resolves which editor to launch in this order:

1. `$VISUAL` if set
2. `$EDITOR` if set
3. The `editor:` field in your user config at
   `~/.config/tickets/config.yml` (or `$XDG_CONFIG_HOME/tickets/config.yml`)
4. If none of the above is set and you're in a terminal, `tickets`
   asks you once, saves your choice to the user config, and uses it
   from then on
5. If you're not in a terminal (script, pipe), `tickets edit` errors
   out and asks you to set `$EDITOR`

The first-run prompt only shows editors actually present on your
`PATH`, so every option will work. You can also type a custom command
like `subl -w` instead of picking from the list.

## Priority

Tickets carry an optional `priority` field, rendered on the board and
`list` views. Set it at creation or change it later:

```sh
tickets new --priority high "Fix login bug on Safari"
tickets set TIC-001 priority critical
tickets set TIC-001 priority -          # clear the field
```

Any string is accepted (`low`, `high`, `P0`, …) but the board styling
knows about `critical`, `high`, `medium`, `low`.

## Links

Tickets can reference each other via symmetric `related` links or
directional `blocks`/`blocked_by` links:

```sh
tickets link TIC-001 TIC-002            # related (both sides)
tickets link TIC-001 TIC-002 --blocks   # TIC-001 blocks TIC-002
tickets unlink TIC-001 TIC-002
```

`tickets doctor` scans the whole store for link integrity issues and
fixes them by default (or reports with `--dry-run`):

- dangling references to tickets that no longer exist
- one-sided links where the reciprocal is missing
- self-referential links

## Worktrees

When a stage agent sets `worktree: true`, each run gets its own git
worktree under `.worktrees/<ticket-id>` on a `tickets/<ticket-id>`
branch. Manage them directly:

```sh
tickets worktree list               # or: tickets wt ls
tickets worktree open TIC-001       # open the worktree in your editor
tickets worktree clean TIC-001      # remove one worktree
tickets worktree clean --all        # remove every worktree
```

A stage can release worktrees automatically with `cleanup: { worktree:
true, branch: true }` (see the Agents section above).

## Obsidian plugin

An optional companion Obsidian plugin lives under
[`obsidian-plugin/`](obsidian-plugin/README.md). It renders `.tickets/`
as a drag-and-drop Kanban board inside Obsidian with inline ticket
editing, per-ticket agent controls, a live terminal pane wired to
`tickets watch`, and a diff view for agent runs.

The CLI embeds the plugin bundle so you can install it without npm:

```sh
tickets obsidian install                # auto-detect vault from cwd
tickets obsidian install --vault ~/Vaults/Work
tickets obsidian status                 # installed vs. bundled version
tickets obsidian uninstall
```

`install` walks up from `--vault` (or `--root`) looking for a
`.obsidian/` directory, writes `main.js` / `manifest.json` /
`styles.css` into `<vault>/.obsidian/plugins/tickets-board/`, and
appends the plugin id to `community-plugins.json` so Obsidian picks
it up on next launch. Pass `--no-enable` to skip the
`community-plugins.json` edit.

The bundled plugin version is locked to the CLI version — upgrade
the CLI and rerun `tickets obsidian install` to keep them in sync.

Everything the plugin does is also available from the CLI — it exists
to give Obsidian users a board view without leaving the editor. See
the plugin's README for manual install and development instructions.

## Ticket file format

Each ticket is a markdown file with a YAML frontmatter block:

```markdown
---
id: TIC-001
title: Fix login bug on Safari
priority: high
related: [TIC-004]
blocked_by: [TIC-002]
blocks: [TIC-009]
created_at: 2026-04-09T22:08:14Z
updated_at: 2026-04-09T22:08:14Z
agent_status: running
agent_run: 001-execute
agent_session: TIC-001-1
---

## Description

The login button doesn't respond on Safari 17...

## Acceptance criteria

- [ ] Works on Safari 16+
- [ ] Regression test added
```

Most fields are optional. The **stage is not stored in the
frontmatter** — it's the parent directory's name. That means you can
`mv` ticket files in Finder and the CLI will see them in the right
column on the next `list`.

The `agent_*` fields are a cache written by `tickets watch`; the
authoritative run state lives in `.tickets/.agents/<id>/<run>.yml`. If
the two ever drift (e.g. the watcher was killed mid-write), the YAML
is truth and the frontmatter is rewritten from it on the next run
transition.

## Configuration

`tickets init` writes `.tickets/config.yml`:

```yaml
prefix: TIC
stages:
  - backlog
  - prep
  - execute
  - review
  - done
# Optional — the agent used by `tickets agents run`.
# default_agent:
#   command: claude
#   args: []
```

- **prefix** — alphabetic prefix for ticket IDs (`TIC-001`, `TIC-002`, ...)
- **stages** — ordered list of stage folder names. The first entry is
  the default stage for newly created tickets. Reorder, rename, or add
  stages by editing this file; the CLI picks the changes up on the next
  invocation.
- **default_agent** — optional. The command `tickets agents run` uses
  to launch an interactive session for any ticket.

The store always lives at `<project>/.tickets/`, the same way `.git/`
always lives at the repo root.

ID numbers are assigned by scanning every stage directory for the
highest existing `<PREFIX>-NNN`, so deletions and manual edits never
desync a counter.

## For agents working on this repo

See [`AGENTS.md`](AGENTS.md) at the repo root for the layer rules,
invariants, and canonical commands that AI coding agents (Claude
Code, Codex, Aider, …) must follow. `make check` runs the full
verification suite — build, vet, and tests (including the
`internal/archtest` layer enforcement).

## Project layout

```
cmd/tickets/main.go           # CLI entry point
internal/config/              # .tickets/config.yml loader
internal/stage/               # per-stage .stage.yml loader (agent + cleanup)
internal/userconfig/          # per-user ~/.config/tickets/config.yml
internal/ticket/
  ├── ticket.go               # Ticket struct + frontmatter (de)serialize
  └── store.go                # FS-backed CRUD: List/Get/Create/Move/Link/Doctor/…
internal/agent/               # PTY runner, run status files, monitor, claude helpers
internal/terminal/            # WebSocket bridge to live PTY sessions (for Obsidian)
internal/worktree/            # per-ticket git worktree management
internal/cli/                 # cobra subcommands (one file per command)
obsidian-plugin/              # companion Obsidian plugin (TypeScript)
```

Both `tickets board` (a Bubble Tea TUI) and the CLI drive the same
`internal/ticket.Store` API — no business logic lives in the command
files.
