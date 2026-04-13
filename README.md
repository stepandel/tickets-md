# tickets-md

A Linear-style ticket tracker where every ticket is a markdown file and
every stage is a folder. No database, no daemon — just files you can
read, grep, edit, and commit alongside your code.

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

Requires Go 1.22+.

```sh
make install
# or manually:
go install ./cmd/tickets
```

`go install` drops the binary in `$(go env GOPATH)/bin` (usually
`~/go/bin`). Add that directory to your `$PATH` if it isn't already:

```sh
echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.zshrc && source ~/.zshrc
```

## Quick start

```sh
mkdir my-project && cd my-project
tickets init
tickets new "Fix login bug on Safari"
tickets new "Add dark mode toggle"
tickets list
tickets move TIC-001 execute
tickets show TIC-001
tickets edit TIC-001        # opens your editor (see "Editor" below)
tickets rm TIC-002          # prompts for confirmation
```

Use `-C <path>` to operate on a store that isn't the current directory:

```sh
tickets -C ~/work/acme list
```

## Commands

| Command                       | What it does                                   |
| ----------------------------- | ---------------------------------------------- |
| `tickets init`                | Create `.tickets/config.yml` + stage folders   |
| `tickets new <title...>`      | Create a ticket in the default stage           |
| `tickets list [--stage X]`    | List tickets, grouped by stage (alias: `ls`)   |
| `tickets show <id>`           | Print a ticket's contents                      |
| `tickets move <id> <stage>`   | Move a ticket to another stage (alias: `mv`)   |
| `tickets edit <id>`           | Open the ticket file in your editor            |
| `tickets rm <id> [--force]`   | Delete a ticket                                |
| `tickets board`               | Interactive kanban board TUI (alias: `tui`)    |
| `tickets watch`               | Watch for ticket movements and spawn agents    |
| `tickets agents`              | List active agent runs                         |
| `tickets agents monitor <id>` | Follow one agent's status and output           |

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
  prompt: |
    Read the ticket at {{path}} and implement what it describes.
    Update the ticket with your progress when done.
```

- **command** — the CLI binary to invoke (`claude`, `codex`, `aider`,
  etc.)
- **args** — extra flags placed before the prompt (e.g. `["--print"]`
  for non-interactive mode)
- **prompt** — a template string rendered with ticket metadata and
  passed as the final argument

Template variables available in the prompt:

| Variable      | Value                                       |
| ------------- | ------------------------------------------- |
| `{{path}}`    | Absolute path to the ticket file            |
| `{{id}}`      | Ticket ID (e.g. `TIC-001`)                  |
| `{{title}}`   | Ticket title from frontmatter               |
| `{{stage}}`   | Destination stage name                      |
| `{{body}}`    | Ticket body (markdown after frontmatter)    |

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

The watcher detects the arrival, spawns the agent in the background,
and appends the agent's output to the ticket file when it finishes:

```
2026/04/09 18:15:21 TIC-001 → execute: spawning claude (pid 4821)
2026/04/09 18:15:45 TIC-001: agent claude finished
2026/04/09 18:15:45 TIC-001: output appended to TIC-001.md
```

The ticket file will have a new section at the end:

```markdown
## Agent Output (claude, 2026-04-09 18:15:45)

<agent's response here>
```

Multiple agents can run concurrently for different tickets. The
watcher also picks up manual file moves (`mv`, Finder, git) — it
watches the filesystem directly, not just the `tickets move` command.

### Monitoring agents

List currently active agent runs:

```sh
tickets agents
```

Follow one agent's status changes and streamed output until it exits:

```sh
tickets agents monitor TIC-001
```

That gives you a read-only progress view. You can also tail the raw
log file directly:

```sh
tail -f .tickets/.agents/TIC-001/runs/001-execute.log
```

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

## Ticket file format

Each ticket is a markdown file with a YAML frontmatter block:

```markdown
---
id: TIC-001
title: Fix login bug on Safari
priority: high
created_at: 2026-04-09T22:08:14Z
updated_at: 2026-04-09T22:08:14Z
---

## Description

The login button doesn't respond on Safari 17...

## Acceptance criteria

- [ ] Works on Safari 16+
- [ ] Regression test added
```

The **stage is not stored in the frontmatter** — it's the parent
directory's name. That means you can `mv` ticket files in Finder and
the CLI will see them in the right column on the next `list`.

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
```

- **prefix** — alphabetic prefix for ticket IDs (`TIC-001`, `TIC-002`, ...)
- **stages** — ordered list of stage folder names. The first entry is
  the default stage for newly created tickets. Reorder, rename, or add
  stages by editing this file; the CLI picks the changes up on the next
  invocation.

The store always lives at `<project>/.tickets/`, the same way `.git/`
always lives at the repo root.

ID numbers are assigned by scanning every stage directory for the
highest existing `<PREFIX>-NNN`, so deletions and manual edits never
desync a counter.

## Project layout

```
cmd/tickets/main.go           # CLI entry point
internal/config/              # config.yml loader
internal/stage/               # per-stage .stage.yml loader
internal/userconfig/          # per-user ~/.config/tickets/config.yml
internal/ticket/
  ├── ticket.go               # Ticket struct + frontmatter (de)serialize
  └── store.go                # FS-backed CRUD: List/Get/Create/Move/Save/Delete
internal/cli/                 # cobra subcommands (one file per command)
```

The CLI is a thin shell over `internal/ticket.Store`. A TUI (Bubble
Tea, tview) can be added later by driving the same `Store` API
directly — no business logic lives in the command files.
