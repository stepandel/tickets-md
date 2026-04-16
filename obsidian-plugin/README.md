# Tickets Board (Obsidian plugin)

Companion plugin for [`tickets-md`](../README.md) — a Linear-style
ticketing system backed by markdown files. The plugin renders the
`.tickets/` directory as a Kanban board, with drag-and-drop between
stages, ticket creation, agent run controls, a live terminal view
of the agent PTY served by `tickets watch`, and read-only terminal
replay for completed agent runs.

The plugin is optional: every operation it exposes is also available
from the `tickets` CLI. It exists to give Obsidian users a board view
without leaving the editor.

## Requirements

- Obsidian 1.1.0 or newer (desktop and mobile; mobile is best-effort).
- A vault that contains a `.tickets/` directory created with
  `tickets init`.
- `tickets watch` running in the project root if you want live agent
  status, the terminal view, or "rerun stage agent" actions. Without
  it the board still works for ticket CRUD; agent panes are inert.

## Install

End users install via the `tickets` CLI, which fetches the matching
plugin build from the GitHub release:

```sh
tickets obsidian install
```

See the root [README](../README.md#obsidian-plugin) for the full
flow.

For plugin development against a local build, from the repo root:

```sh
make plugin-install                       # installs into ./.tickets or enclosing vault
make plugin-install VAULT=~/Vaults/Work   # or target a specific vault
```

That bundles the plugin with esbuild and runs
`tickets obsidian install --from obsidian-plugin`, which reads
`main.js`, `manifest.json`, and `styles.css` from the directory and
copies them into the vault's `.obsidian/plugins/tickets-board/` — no
network, no cache, no release tag required. Works with any CLI
version (including `dev` builds).

After installing, enable **Tickets Board** in Obsidian under
*Settings → Community plugins*.

## Develop

```sh
cd obsidian-plugin
npm ci
npm run dev      # esbuild in watch mode → main.js
npx tsc --noEmit # standalone typecheck (CI runs this too)
```

`npm run build` produces a production bundle. `npm test` runs the
Node.js test runner against `src/**/*.test.ts` (via tsx) and
`test/**/*.test.mjs`. From the repo root, `make plugin-test` runs
the same suite after a fresh `npm ci`.

## What it provides

- A board view (`Open: Tickets Board` command) that mirrors the
  stages defined in `.tickets/config.yml`, drag-and-drop included.
- Inline ticket creation, edits, priority and link controls.
- Board card project assignment controls: assign, change, or remove
  `project:` from the ticket context menu.
- Board cards surface the assigned project as a compact chip, with a
  muted fallback for dangling `project:` references.
- Per-ticket agent controls: spawn an adhoc run, re-run the stage
  agent, open a live terminal, replay a completed run's terminal
  output, view the diff a run produced.
- Cron agents from `config.yml` in the Agents view, including edit,
  enable/disable, run now (desktop), and last-log actions.
- A projects view (`Open: Tickets Projects` command) listing
  `.tickets/projects/` alongside a tickets sidebar that shows the
  selected project's assigned tickets, with create, rename, set
  status, assign tickets, and delete (which unassigns `project:` on
  member tickets first).
- A diff view (powered by diff2html) for any agent run that touched
  files.

The terminal view talks to the WebSocket bridge `tickets watch`
exposes on `127.0.0.1`. The bridge accepts only the Obsidian origin,
so the panes do not work in other browsers.
