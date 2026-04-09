# tickets-md

A Linear-style ticket tracker where every ticket is a markdown file and
every stage is a folder. No database, no daemon — just files you can
read, grep, edit, and commit alongside your code.

```
.tickets/
├── config.yml
├── backlog/
├── todo/
│   └── TIC-002.md
├── in-progress/
│   └── TIC-001.md
└── done/
```

The whole store lives under a single hidden `.tickets/` directory at
the project root, the same way `.git/` works.

Moving a ticket between stages is just `mv`, and `git log` is your
audit trail.

## Install

Requires Go 1.22+.

```sh
go install ./cmd/tickets
# or build a local binary without installing:
go build -o tickets ./cmd/tickets
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
tickets move TIC-001 in-progress
tickets show TIC-001
tickets edit TIC-001        # opens $EDITOR
tickets rm TIC-002          # prompts for confirmation
```

Use `-C <path>` to operate on a store that isn't the current directory:

```sh
tickets -C ~/work/acme list
```

## Commands

| Command                       | What it does                                 |
| ----------------------------- | -------------------------------------------- |
| `tickets init`                | Create `.tickets/config.yml` + stage folders |
| `tickets new <title...>`      | Create a ticket in the default stage         |
| `tickets list [--stage X]`    | List tickets, grouped by stage (alias: `ls`) |
| `tickets show <id>`           | Print a ticket's contents                    |
| `tickets move <id> <stage>`   | Move a ticket to another stage (alias: `mv`) |
| `tickets edit <id>`           | Open the ticket file in `$EDITOR`            |
| `tickets rm <id> [--force]`   | Delete a ticket                              |

`init` accepts `--prefix` and `--stages` to override the defaults at
creation time.

## Ticket file format

Each ticket is a markdown file with a YAML frontmatter block:

```markdown
---
id: TIC-001
title: Fix login bug on Safari
priority: high
labels: [bug, auth]
assignee: alice
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
  - todo
  - in-progress
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
internal/ticket/
  ├── ticket.go               # Ticket struct + frontmatter (de)serialize
  └── store.go                # FS-backed CRUD: List/Get/Create/Move/Save/Delete
internal/cli/                 # cobra subcommands (one file per command)
```

The CLI is a thin shell over `internal/ticket.Store`. A TUI (Bubble
Tea, tview) can be added later by driving the same `Store` API
directly — no business logic lives in the command files.
