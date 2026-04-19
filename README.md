```
████████╗██╗ ██████╗██╗  ██╗███████╗████████╗███████╗    ███╗   ███╗██████╗
╚══██╔══╝██║██╔════╝██║ ██╔╝██╔════╝╚══██╔══╝██╔════╝    ████╗ ████║██╔══██╗
   ██║   ██║██║     █████╔╝ █████╗     ██║   ███████╗    ██╔████╔██║██║  ██║
   ██║   ██║██║     ██╔═██╗ ██╔══╝     ██║   ╚════██║ ██╗██║╚██╔╝██║██║  ██║
   ██║   ██║╚██████╗██║  ██╗███████╗   ██║   ███████║ ╚═╝██║ ╚═╝ ██║██████╔╝
   ╚═╝   ╚═╝ ╚═════╝╚═╝  ╚═╝╚══════╝   ╚═╝   ╚══════╝    ╚═╝     ╚═╝╚═════╝
```

# tickets-md

A Linear-style ticket tracker that lives inside a single git repo —
every ticket is a markdown file, every stage a folder, kept right
next to the code. Store config under `.tickets/config.yml` and stage
config under `.tickets/*/.stage.yml` are meant to be committed; ticket
markdown and runtime state stay gitignored. The
companion Obsidian plugin is the primary UI (drag-and-drop Kanban,
live agent terminal); the `tickets` CLI drives the same files for
terminal-first users. No database, no background service; agent
automation is opt-in via `tickets watch`.

> **Scoped to one repo.** The CLI is global; `tickets init` is per-repo
> and creates `.tickets/` at the repo root (the way `.git/` does).
> Run it once per project.

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

The intended git policy is mixed:

- track `.tickets/config.yml` and `.tickets/*/.stage.yml` so board and stage automation are reviewable
- ignore `.tickets/<stage>/*.md`, `.tickets/.agents/`, and other local runtime state
- let `tickets init` maintain the repo-root `.gitignore` block for that policy

Board-level cron agents can also be defined in `.tickets/config.yml`
and are fired by `tickets watch` while it is running. Edits to
`stages:`, `cron_agents:`, `watch:` monitor timings, and per-stage
`.stage.yml` files are hot-reloaded by a running watcher; no restart
is required:

```yaml
cron_agents:
  - name: backlog-groomer
    schedule: "@every 5m"
    command: claude
    args: ["--dangerously-skip-permissions"]
    interactive: true
    prompt: |
      You are the backlog groomer for {{root}} at {{now}}.
      Review the backlog and clean up duplicates or outdated tickets.
```

Only the built-in `claude` cron integration auto-adds a one-shot flag
(`--print`). Other cron commands must supply their own non-interactive
or exit-on-complete flags in `cron_agents[].args` so each tick exits
cleanly. If a non-integrated cron agent is configured without cron-only
`args:`, `tickets watch` logs an advisory warning when it loads the
entry because later ticks may be skipped while the prior run is still
active.

Set `interactive: true` when you want to attach to a scheduled cron's
live PTY from the Obsidian agents view and steer it manually. In that
mode the watcher skips Claude's auto-`--print` injection, so the session
stays interactive until you close it. While that interactive run is
alive, subsequent ticks for the same cron are skipped; keep any required
permission-bypass flags such as `--dangerously-skip-permissions` in
`args:` or the cron may block on its first prompt. To stop an
interactive cron and let later ticks run again, use `tickets crons stop
<name>` or the Obsidian desktop Agents-view cron menu's `Stop session`
action.

Moving a ticket between stages is just a file rename, so if you choose
to commit ticket markdown in your own workflow, `git log` can serve as
an audit trail too. By default this repo keeps ticket markdown ignored
while still tracking stage config. When a stage has an agent configured,
`tickets watch` automatically spawns the agent whenever a ticket arrives.

<img width="2912" height="1524" alt="CleanShot 2026-04-13 at 20 11 04@2x" src="https://github.com/user-attachments/assets/9b094924-3924-4f32-b9a7-21dd17438d6b" />

_Obsidian plugin_

<img width="1984" height="1548" alt="CleanShot 2026-04-13 at 20 20 11@2x" src="https://github.com/user-attachments/assets/e55509ce-1769-4f46-991a-c5099cc04f0b" />

_Terminal_ _board_ _`tickets board`_



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

Building from a local checkout:

```sh
git clone https://github.com/stepandel/tickets-md.git
cd tickets-md
make install
```

Development verification from a local checkout:

```sh
make check
make plugin-test
make qa-cli
make qa-plugin   # requires Obsidian desktop CLI support; set OBSIDIAN_BIN if needed
```

`make qa` runs both QA harnesses together.

The Obsidian plugin is no longer embedded in the CLI binary — it is
fetched on demand by `tickets obsidian install` from the GitHub
release matching your CLI version. See the [Obsidian plugin](#obsidian-plugin)
section for the dev/offline flow (`--from <dir>`).

On interactive runs, the CLI checks GitHub for a newer release at
most once per 24 hours and prints a one-line stderr reminder when
you are behind. Set `TICKETS_NO_UPDATE_CHECK=1` to disable that nag.

## Upgrade

### Homebrew

```sh
brew update && brew upgrade tickets
```

If `brew upgrade` still shows the old version, force-refresh the tap
and retry:

```sh
brew tap --force stepandel/tap
brew upgrade tickets
```

### From source

```sh
go install github.com/stepandel/tickets-md/cmd/tickets@latest
```

After upgrading the CLI, re-run `tickets obsidian install` in each
project vault to sync the companion plugin to the new version.

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
repo root — same way `.git/` does — and `tickets init` also writes the
repo-root `.gitignore` block that tracks `.tickets/config.yml` and
`.tickets/*/.stage.yml` while keeping ticket markdown and runtime state
ignored.

```sh
cd ~/code/my-app              # a git repo with code you work on
tickets init                  # creates ./.tickets/ + stage folders
git add .gitignore .tickets/config.yml .tickets/*/.stage.yml
git commit -m "chore: add tickets-md board config"
```

That default policy means:

- `.tickets/config.yml` is shared in Git
- `.tickets/*/.stage.yml` is shared in Git
- `.tickets/<stage>/*.md` stays local and ignored
- `.tickets/.agents/` stays local and ignored

Running `tickets init` outside a git repo works — `.tickets/` is just
a directory — but you lose two useful properties:

- **Scoping of agents.** When a stage has `worktree: true`, each
  agent run is spawned inside a fresh `git worktree` under
  the configured `worktrees.dir` (default `.worktrees/<ticket-id>`)
  on a dedicated branch under `worktrees.branch_prefix` (default
  `tickets/<ticket-id>`), so concurrent agents can't trample each other and
  experimental changes stay isolated from `main` until you merge
  them. Without a git repo, agents run directly against the
  directory with no isolation.
- **Audit trail.** Ticket moves are file renames, so `git log --stat
  .tickets/` shows who moved what when and from which stage to which
  in setups that choose to commit ticket markdown.

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

Prefer a board UI? Run `tickets obsidian install` and jump to the
[Obsidian plugin](#obsidian-plugin) section for the (manual) vault
setup steps.

## Commands

| Command                                 | What it does                                       |
| --------------------------------------- | -------------------------------------------------- |
| `tickets init`                          | Create `.tickets/config.yml` + stage folders       |
| `tickets new <title...> [--priority P] [--project ID] [--parent ID] [--blocked-by ID...] [--blocks ID...] [--related ID...] [--label L...] [--body MD]` | Create a ticket in the default stage |
| `tickets projects <subcommand>`         | Create, list, show, update, assign, and delete projects |
| `tickets list [--stage X] [--project P] [--archived]`| List tickets, grouped by stage (alias: `ls`)       |
| `tickets labels [--on <id>]`            | List configured labels, or the labels on a ticket  |
| `tickets labels create <name>`          | Add a configured label with the default chip color |
| `tickets labels edit <name> [--color C] [--bold\|--no-bold] [--order N\| - ]` | Edit configured label styling fields |
| `tickets labels rename <old> <new>`    | Change a configured label's casing and rewrite matching ticket labels |
| `tickets labels delete <name> [--force]`| Remove a configured label entry from config        |
| `tickets archive <id> [--from <stage>] [--older-than D] [--dry-run]` | Move a ticket, or older tickets from a stage, into the configured archive stage |
| `tickets show <id>`                     | Print a ticket's contents                          |
| `tickets move <id> <stage>`             | Move a ticket to another stage (alias: `mv`)       |
| `tickets edit <id>`                     | Open the ticket file in your editor                |
| `tickets set <id> <field> <value...>`   | Update a scalar field (`priority`, `project`, `title`) |
| `tickets label <id> <label...>`         | Add one or more configured labels to a ticket      |
| `tickets unlabel <id> <label...>`       | Remove one or more labels from a ticket            |
| `tickets rm <id> [--force]`             | Delete a ticket                                    |
| `tickets link <a> <b> [--blocks\|--parent]`       | Link two tickets (related, blocks, or parent/child) |
| `tickets unlink <a> <b> [--blocks\|--parent]`     | Remove a link                                      |
| `tickets cleanup [--dry-run]`           | Remove orphaned or archived-stage agent artifacts  |
| `tickets doctor [--dry-run]`            | Scan for drift across tickets, runs, worktrees     |
| `tickets board [--project P] [--archived]` | Interactive kanban board TUI (alias: `tui`)     |
| `tickets watch`                         | Watch for ticket movements and spawn agents        |
| `tickets watch pause [reason]`          | Pause watcher-managed agent spawns (including crons) |
| `tickets watch resume`                  | Resume watcher-managed agent spawns                |
| `tickets watch status`                  | Show whether the watcher is paused                 |
| `tickets agents [-a] [--history]`       | List agent runs                                    |
| `tickets agents log <id> [run]`         | Print the captured output for a run                |
| `tickets agents plan <id> [run]`        | Open the plan file written by a Claude Code run    |
| `tickets agents followup <id> [--run R] [--message M]` | Spawn a followup session with prior run's context |
| `tickets agents run <id>`               | Start an interactive agent session for a ticket    |
| `tickets crons list`                    | List configured cron agents and their last run     |
| `tickets crons run <name>`              | Manually fire a cron agent through the watcher     |
| `tickets crons stop <name>`             | Terminate the active PTY session for a cron agent  |
| `tickets crons log <name> [run-id]`     | Print output for a cron agent run                  |
| `tickets crons add --name N --schedule S --command C --prompt P [--arg A...] [--disabled]` | Add a new cron agent entry |
| `tickets crons rm <name>`               | Remove a configured cron agent                     |
| `tickets crons enable <name>`           | Enable a configured cron agent                     |
| `tickets crons disable <name>`          | Disable a configured cron agent                    |
| `tickets crons set <name> <field> <value...>` | Set a field on a cron agent (schedule, command, prompt, args) |
| `tickets worktree list`                 | List active per-ticket git worktrees (alias: `wt`) |
| `tickets worktree open <id>`            | Open a ticket's worktree in your editor            |
| `tickets worktree clean [ids...\|--all]`| Remove worktrees                                   |
| `tickets completion <shell>`            | Emit a shell completion script                     |
| `tickets hooks install [--force]`       | Install a pre-commit hook that runs `make check` (and `make plugin-test` when plugin files are staged) |
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

## Archive

If older finished tickets are piling up, add a normal configured stage
and mark it as the archive stage in `.tickets/config.yml`:

```yaml
stages:
  - backlog
  - prep
  - execute
  - review
  - done
  - archive
archive_stage: archive
```

Archived tickets stay as ordinary markdown files under
`.tickets/<stage>/`, so `tickets show`, `tickets move`, agent history,
and `tickets doctor` still work exactly the same way. What changes is
default visibility: `tickets list` and `tickets board` hide the
configured archive stage unless you pass `--archived`, while
`tickets list --stage archive` still shows it directly.

Use `tickets archive` as the convenience wrapper around `tickets move`:

```sh
tickets archive TIC-042
tickets archive --from done --older-than 720h --dry-run
tickets archive --from done --older-than 720h
```

Bulk archive mode uses each ticket's `updated_at` timestamp, so a
ticket moved into `done` recently will not be archived just because it
was created long ago. Tickets with a live non-terminal agent run are
skipped. If you also list the archive stage in `complete_stages`, moves
into archive still count as completion transitions.

## Agents

<img width="3088" height="1484" alt="CleanShot 2026-04-13 at 20 18 08@2x" src="https://github.com/user-attachments/assets/a896b225-b9ad-4dd1-8e08-491fd824b80e" />

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
  args: ["--dangerously-skip-permissions"]
  worktree: true              # isolate work in worktrees.dir/<id> on branch worktrees.branch_prefix<id>
  base_branch: main           # branch to create the worktree from (default: HEAD)
  max_concurrent: 2           # cap on simultaneously-active agents in this stage (0 = unlimited)
  prompt: |
    You are working in {{worktree}} on the ticket branch.
    Read the ticket at {{path}} and implement what it describes.
```

- **command** — the CLI binary to invoke (`claude`, `codex`, `aider`,
  etc.)
- **args** — extra flags placed before the prompt (e.g.
  `["--dangerously-skip-permissions"]` to let the agent run without
  approval prompts, or `["--print"]` for non-interactive mode)
- **worktree** — when true, each run gets its own git worktree under
  `worktrees.dir/<ticket-id>` on a
  `worktrees.branch_prefix<ticket-id>` branch. The defaults are
  `.worktrees/<ticket-id>` and `tickets/<ticket-id>`, so concurrent
  agents don't trample one another's changes
- **base_branch** — the branch the worktree is cut from
- **max_concurrent** — optional cap on how many non-terminal agent
  runs may be active in this stage at once. Zero (the default) means
  unlimited. When the cap is reached, additional tickets that arrive
  in the stage are queued: their frontmatter gets a `queued_at`
  timestamp and they're admitted in FIFO order (oldest `queued_at`
  first, ticket id as tiebreaker) as soon as an active run completes
  or the cap is raised by editing `.stage.yml`. `tickets agents`
  continues to show the active runs; queued tickets sit in the stage
  directory with `queued_at` set until their turn comes up
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
  worktree: true    # remove worktrees.dir/<id> (default .worktrees/<id>)
  branch: true      # delete worktrees.branch_prefix<id> (default tickets/<id>)
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

Multiple agents can run concurrently for different tickets, optionally
capped per stage via `agent.max_concurrent` (see above). The watcher
also picks up manual file moves (`mv`, Finder, git) — it watches the
filesystem directly, not just the `tickets move` command.

The watcher's monitor polls every 5s by default and flips a session
to `blocked` after 30s of pane silence. Both thresholds are
configurable in `.tickets/config.yml`:

```yaml
watch:
  poll_interval: 10s      # how often the monitor reconciles state
  idle_block_after: 60s   # pane silence before a run flips to blocked
  idle_kill_after: 10m    # optional: SIGTERM the PTY after prolonged silence
```

Durations use Go's `time.ParseDuration` syntax (`500ms`, `2s`,
`1m`, …). `idle_block_after` and `idle_kill_after` must be ≥ `1s`,
and when both are set `idle_kill_after` must be at least as long as
`idle_block_after`. `idle_kill_after` is opt-in; it measures the same
PTY output silence as `idle_block_after`, sends SIGTERM to the idle
session, and marks the run `failed`. Editing these values while
`tickets watch` is already running updates the live monitor on the
next config reload debounce; no watcher restart is required.

Edits to any stage's `.stage.yml` while the watcher is running are
also picked up without a restart — the watcher reloads the affected
stage config on the next debounce and logs the new status. If the
reloaded file fails to parse, the previous config is kept so the
watcher stays running.

Adding or removing entries in `stages:` while the watcher is running
is likewise reconciled on the next debounce: new stage directories
are created (with a default `.stage.yml`) and start being watched,
and removed stages stop being watched. No restart is required.

Direct filesystem changes to a configured stage directory (a `mv` or
`rm -rf` of the directory itself, or recreating it) are detected the
same way: the watcher drops the watch and clears the cached stage
config when the directory disappears, and re-adds the watch, reloads
`.stage.yml`, and re-seeds its known tickets when the directory
reappears. No config edit or restart is required.

### Pausing the watcher

Use `tickets watch pause` to temporarily stop the watcher from
spawning new agents when tickets arrive in a stage, when a stage
rerun is requested, or when a board-level cron fires. Already-running
sessions keep running; only new spawns are gated.

```sh
tickets watch pause "release freeze"   # optional reason is recorded
tickets watch status                   # "watch is paused since …"
tickets watch resume                   # clears the pause
```

Pause state is tracked in `.tickets/.watch-paused`, so it survives
restarts and is picked up immediately by any `tickets watch` process
without reconfiguration.

The Obsidian plugin's Board and Agents views both expose the same
pause/resume toggle (with a status pill) in their headers while
`tickets watch` is running, so operators can pause without dropping
to a shell.

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
might miss — including stale complete-stage blocks left behind by moves
that happened while `tickets watch` was down — plus link integrity,
stale agent runs, orphan worktrees, and ticket frontmatter that
disagrees with the authoritative run YAMLs.

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
- **Stale blocks** — `blocks` / `blocked_by` entries left over on a
  ticket already sitting in a configured `complete_stages` stage;
  cleared on both sides. This is the offline counterpart to the
  automatic unblocking that `tickets move` and `tickets watch` perform
  at move time, so moves that bypassed both still converge.
- **Stale runs** — non-terminal run YAMLs whose `updated_at` is older
  than `--stale-after`; flipped to `failed`.
- **Orphan agent dirs** — `.tickets/.agents/<id>/` directories whose
  ticket no longer exists, and `.tickets/.agents/.cron/<name>/`
  directories whose cron agent is no longer in `cron_agents:`;
  removed. These cron owner dirs are user-config territory, so the
  watcher's monitor never prunes them — only `tickets doctor --fix`
  does.
- **Orphan `.tmp` files** — leftover `<run>.yml.tmp` from an
  interrupted atomic rename; removed.
- **Orphan worktrees** — directories under `worktrees.dir/` (default
  `.worktrees/<id>/`) whose ticket no longer exists; removed.
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

Any string is accepted (`low`, `high`, `P0`, …). Board styling uses
built-in defaults for `critical`, `urgent`, `high`, `medium`/`med`,
and `low` (matched case-insensitively) unless you override them in
`.tickets/config.yml`:

```yaml
priorities:
  P0:
    color: "#ff4d4f"
    bold: true
    order: 0
  P1:
    color: "#fa8c16"
    order: 1
```

`order` is optional and affects the priority pickers in both the CLI
board (`p`) and the Obsidian board (right-click → "Set priority").
When `priorities:` is omitted, those pickers keep the built-in
`critical`, `high`, `medium`, `low`, `none` order. When `priorities:`
is present, ordered entries come first by ascending `order`, unordered
entries follow sorted case-insensitively by name, and `none` is still
appended last. `priorities: {}` is an explicit opt-out that leaves the
pickers with only `none`.

`none` is reserved for the picker's cleared-priority option, so it
cannot be used as a key in `priorities:` (the check is
case-insensitive and trims whitespace). Config load fails with a
`priority "…" is reserved` error if you try.

Omit `priorities:` to keep the current default styling and picker
ordering.

## Labels

Tickets can also carry an optional `labels` list. Labels are configured
in `.tickets/config.yml`, shown as chips on the Obsidian board, and can
be added or removed from a ticket from the card context menu:

```yaml
labels:
  backend:
    color: "#0f766e"
    order: 0
  customer:
    color: "#dc2626"
    bold: true
```

Like priorities, label names are matched case-insensitively for config
lookup, `order` controls picker ordering, and `none` is reserved so it
cannot be used as a label key. Unlike priorities, there are no built-in
defaults: omitting `labels:` simply means there are no configured label
choices yet.

In the Obsidian board, right-click a card to:

- `Add label` from configured labels not already on the ticket
- `Remove label` from any label already on the ticket
- `Create label` to add a new config entry and immediately assign it

If a ticket already contains a label that is not currently configured,
the board still renders it with fallback styling and lets you remove it
without editing YAML by hand.

The CLI exposes the same ticket-level workflows:

```sh
tickets labels
tickets labels --on TIC-001
tickets labels create backend
tickets labels edit backend --color '#0f766e' --bold --order 10
tickets labels edit backend --order -
tickets labels rename backend Backend
tickets labels delete backend
tickets label TIC-001 backend customer
tickets unlabel TIC-001 customer
```

CLI label assignment is strict: `tickets label` and `tickets new --label`
only accept labels that already exist in `.tickets/config.yml`. The
CLI can create configured labels explicitly with `tickets labels create`,
which writes the new config entry using the default chip color `#6b7280`.
Creation rejects empty names, the reserved label `none`, and
case-insensitive duplicates by reporting the existing configured key.
The CLI can also edit configured label fields with `tickets labels edit`:
`--color` changes the chip color, `--bold` / `--no-bold` toggles bold
rendering, and `--order` sets picker ordering (`--order -` clears it).
`tickets labels rename` supports casing-only renames such as `backend`
to `Backend`, and rewrites matching ticket frontmatter labels to keep
ticket casing aligned with config.
`tickets labels delete` removes a configured label entry. By default it
fails if any tickets still carry that label and prints carrier IDs so
you can scrub them first with `tickets unlabel`; `--force` removes only
the config entry and leaves existing ticket frontmatter intact.
Semantic renames across different normalized label names are
intentionally not supported yet.
Assignment stays strict: `tickets label` and `tickets new --label` still
fail on unknown labels instead of creating them implicitly. The board's
`t` action can also create a new configured label on the fly with the
same default color, then assign it immediately.

## Create-Time Metadata

`tickets new` can also set existing ticket metadata up front instead of
requiring follow-up `set` or `link` commands:

```sh
tickets new "Fix login bug on Safari" --project PRJ-001 --blocked-by TIC-003
tickets new "Split auth UI" --parent TIC-001 --related TIC-004 --related TIC-005
tickets new "Ship migration" --blocks TIC-010 --priority critical
tickets new "Triage bug report" --label customer --label backend
tickets new "Document auth flow" --body "## Description\n\nCapture the login states."
```

`--blocked-by`, `--blocks`, and `--related` accept multiple ticket IDs,
either by repeating the flag or by passing a comma-separated list.
`--label` works the same way, but only for preconfigured labels.

`--body` recognizes a small fixed escape set in the flag value:
`\n` becomes a newline, `\r` a carriage return, `\t` a tab, and `\\`
a literal backslash. Any other `\X` sequence is left unchanged, so
regex and markdown escapes like `\d+` or `\*literal\*` still save
literally. Real newlines passed in (e.g. via `"$(printf ...)"` or a
quoted multi-line string) are preserved unchanged, and `\\n` is the
escape hatch for saving a literal two-character `\n`.

All relation targets (`--parent`, `--blocked-by`, `--blocks`,
`--related`) and `--project` are validated against the store before
the ticket is created: if any ID is unknown, empty, duplicated within
a flag, or reused across conflicting relation roles, `tickets new`
fails with an error and leaves nothing on disk.

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
- stale `blocks` entries on tickets that have entered a configured
  `complete_stages` stage (the offline repair path for moves missed by
  `tickets move` or `tickets watch`, cleared on both sides)

## Sub-tickets

Tickets can also form a single-parent tree:

- child tickets store `parent: TIC-001`
- parent tickets store `children: [TIC-042, TIC-043]`

Use either `tickets new --parent` when creating the child or
`tickets link --parent` afterwards:

```sh
tickets new "Split auth UI" --parent TIC-001
tickets link TIC-042 TIC-001 --parent
tickets unlink TIC-042 TIC-001 --parent
```

Parent and child stages are independent: moving a parent does not move
its children. Deleting a parent orphans its children by clearing their
`parent` field. `tickets doctor` repairs one-sided parent/child links
and removes dangling parent/child references, but it does not yet scan
for non-trivial cycles introduced by manual edits.

## Cleanup

`tickets cleanup` removes leftover agent artifacts that the normal
watcher flow does not always touch:

- orphan `.tickets/.agents/<id>/` directories for tickets that no longer exist
- orphan worktree directories under `worktrees.dir/` (default `.worktrees/<id>/`)
- orphan branches under `worktrees.branch_prefix` (default `tickets/<id>`)
- optionally, agent data/worktrees/branches for tickets that are still
  sitting in configured archive stages such as `done`

Top-level cleanup config lives in `.tickets/config.yml`:

```yaml
cleanup:
  stages:
    - name: done
      agent_data: true
      worktree: true
      branch: true
```

Useful modes:

```sh
tickets cleanup
tickets cleanup --dry-run
tickets cleanup --orphans-only
tickets cleanup --stages-only
```

The command force-deletes branches under `worktrees.branch_prefix`
(default `tickets/<id>`), so run it deliberately and preferably while
`tickets watch` is idle.

## Worktrees

When a stage agent sets `worktree: true`, each run gets its own git
worktree under `worktrees.dir/<ticket-id>` on a
`worktrees.branch_prefix<ticket-id>` branch. The defaults are
`.worktrees/<ticket-id>` and `tickets/<ticket-id>`. Manage them
directly:

```sh
tickets worktree list               # or: tickets wt ls
tickets worktree open TIC-001       # open the worktree in your editor
tickets worktree clean TIC-001      # remove one worktree
tickets worktree clean --all        # remove every worktree
```

A stage can release worktrees automatically with `cleanup: { worktree:
true, branch: true }` (see the Agents section above).

## Obsidian plugin

<img width="2848" height="1454" alt="CleanShot 2026-04-13 at 20 11 48@2x" src="https://github.com/user-attachments/assets/92d2136e-69ea-4f3c-85f4-bed2bfe9fc61" />


The companion Obsidian plugin renders `.tickets/` as a drag-and-drop
Kanban board with inline ticket editing, per-ticket agent controls, a
live terminal pane wired to `tickets watch`, a projects view over
`.tickets/projects/`, and a diff view for agent runs. Source lives
under [`obsidian-plugin/`](obsidian-plugin/README.md).

### Install (one command)

From the repo root where you ran `tickets init`:

```sh
tickets obsidian install
```

That single command does three things:

1. Bootstraps an Obsidian vault at `.tickets/` (by creating
   `.tickets/.obsidian/`). The plugin's Kanban view reads the stage
   folders under `.tickets/` as its columns, so the ticket store
   *is* the vault — Obsidian shouldn't see the rest of your code.
   If you already opened the repo as a vault elsewhere (a
   `.obsidian/` at or above the project root), that vault is reused
   instead.
2. Downloads `tickets-board-plugin.zip` from the GitHub release
   matching your CLI version (cached under the user cache dir so
   reinstalls are offline) and writes `main.js`, `manifest.json`,
   and `styles.css` into `<vault>/.obsidian/plugins/tickets-board/`.
3. Appends `tickets-board` to `<vault>/.obsidian/community-plugins.json`
   so Obsidian marks the plugin as enabled once you turn community
   plugins on.

Obsidian has no CLI to register a vault, so the remaining steps are
manual (the install command prints them too):

1. Open Obsidian → **Open folder as vault** → **pick `.tickets/`
   specifically, not the repo root**. The plugin renders the stage
   folders under `.tickets/` as Kanban columns, so the vault root
   has to be `.tickets/`.
2. **Settings → Community plugins → Turn on community plugins**
   (confirm the safety prompt).
3. Under **Installed plugins**, toggle **Tickets Board** on.
4. `Cmd+P` (or `Ctrl+P`) → **Tickets Board: Open Tickets Board**.

### Upgrades and housekeeping

```sh
tickets obsidian install           # re-run after upgrading the CLI to sync the vault
tickets obsidian status            # installed plugin version vs. this CLI's expected version
tickets obsidian uninstall         # removes plugin dir and community-plugins.json entry
tickets obsidian install --no-enable   # copy files but don't touch community-plugins.json
tickets obsidian install --vault ~/Vaults/Work   # install into a specific vault
tickets obsidian install --from ./obsidian-plugin  # install from a local build (dev flow)
```

The plugin version is locked to the CLI version — `brew upgrade
tickets` (or `go install …@latest`) and rerun
`tickets obsidian install` to keep them in sync. The download is
cached under the user cache directory (`$XDG_CACHE_HOME/tickets/plugin/<version>/`
on Linux, `~/Library/Caches/tickets/plugin/<version>/` on macOS), so
a second install of the same version is offline.

## Ticket file format

Each ticket is a markdown file with a YAML frontmatter block:

```markdown
---
id: TIC-001
title: Fix login bug on Safari
priority: high
labels: [backend, customer]
related: [TIC-004]
blocked_by: [TIC-002]
blocks: [TIC-009]
parent: TIC-000
children: [TIC-010]
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
column on the next `list`. `tickets watch` also applies configured
complete-stage unblocking on filesystem moves, so Obsidian drag/drop
and raw `mv` renames clear `blocks` / `blocked_by` the same way
`tickets move` does — as long as the watcher is running.

The `agent_*` fields are a cache written by `tickets watch`; the
authoritative run state lives in `.tickets/.agents/<id>/<run>.yml`. If
the two ever drift (e.g. the watcher was killed mid-write), the YAML
is truth and the frontmatter is rewritten from it on the next run
transition.

## Configuration

`tickets init` writes `.tickets/config.yml`:

```yaml
prefix: TIC
project_prefix: PRJ
stages:
  - backlog
  - prep
  - execute
  - review
  - done
# Optional — where per-ticket git worktrees live.
# worktrees:
#   dir: .worktrees
#   branch_prefix: tickets/
# Optional — the agent used by `tickets agents run`.
# default_agent:
#   command: claude
#   args: []
# Optional — when a ticket enters one of these stages (via tickets move,
# tickets watch, or doctor), it stops blocking its dependents.
# complete_stages:
#   - done
```

- **prefix** — alphabetic prefix for ticket IDs (`TIC-001`, `TIC-002`, ...)
- **project_prefix** — alphabetic prefix for project IDs (`PRJ-001`, `PRJ-002`, ...)
- **stages** — ordered list of stage folder names. The first entry is
  the default stage for newly created tickets. Reorder, rename, or add
  stages by editing this file; the CLI picks the changes up on the next
  command invocation, and a running `tickets watch` reconciles its
  watch set on the next debounce (scaffolding added stages, dropping
  removed ones). The name `projects` is reserved for the project store
  and cannot be used as a stage.
- **worktrees.dir** — optional relative path under the repo root where
  per-ticket git worktrees are created. Defaults to `.worktrees`.
- **worktrees.branch_prefix** — optional branch namespace used for
  per-ticket worktree branches. Must end with `/`. Defaults to
  `tickets/`.
- **complete_stages** — optional subset of `stages`. When a ticket
  enters one of these stages — via `tickets move`, a filesystem
  move picked up by `tickets watch`, or a `tickets doctor` sweep —
  its `blocks` links are cleared and the peer tickets lose the
  matching `blocked_by` entry.
- **default_agent** — optional. The command `tickets agents run` uses
  to launch an interactive session for any ticket.

The store always lives at `<project>/.tickets/`, the same way `.git/`
always lives at the repo root. From a linked git worktree, every
command except `tickets watch` auto-detects that main repo store
unless you pass `-C` explicitly. `tickets watch` refuses to start from
the linked worktree by default; run it from the main repo root so one
daemon owns `.tickets/.terminal-server`, fsnotify watches, and PTY
sessions, or pass `-C <path>` to opt into a per-worktree daemon.

ID numbers are assigned by scanning every stage directory for the
highest existing `<PREFIX>-NNN`, so deletions and manual edits never
desync a counter.

## For agents working on this repo

See [`AGENTS.md`](AGENTS.md) at the repo root for the layer rules,
invariants, and canonical commands that AI coding agents (Claude
Code, Codex, Aider, …) must follow. `make check` is the canonical Go
verification command — build, vet, and `go test ./...` (including the
`internal/archtest` layer enforcement). Changes under
`obsidian-plugin/` should also run `make plugin-test`.

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
