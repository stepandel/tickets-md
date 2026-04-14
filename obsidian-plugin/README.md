# Tickets Board (Obsidian plugin)

Companion plugin for [`tickets-md`](../README.md) — a Linear-style
ticketing system backed by markdown files. The plugin renders the
`.tickets/` directory as a Kanban board, with drag-and-drop between
stages, ticket creation, agent run controls, and a live terminal view
of the agent PTY served by `tickets watch`.

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

For everyday development against a real vault, the package script
builds and copies the plugin into the vault's plugin folder:

```sh
cd obsidian-plugin
npm ci
npm run install-plugin
```

`install-plugin` writes `main.js`, `manifest.json`, and `styles.css`
into `../.tickets/.obsidian/plugins/tickets-board/` — that path
assumes the repo's own `.tickets/` is the vault. Adjust the script in
`package.json` if your vault lives elsewhere, or copy the three
artefacts manually.

After installing, enable **Tickets Board** in Obsidian under
*Settings → Community plugins*.

## Develop

```sh
cd obsidian-plugin
npm ci
npm run dev      # esbuild in watch mode → main.js
npx tsc --noEmit # standalone typecheck (CI runs this too)
```

`npm run build` produces a production bundle. The plugin is a single
TypeScript source file (`src/main.ts`) bundled with esbuild; there is
no test harness yet.

## What it provides

- A board view (`Open: Tickets Board` command) that mirrors the
  stages defined in `.tickets/config.yml`, drag-and-drop included.
- Inline ticket creation, edits, priority and link controls.
- Per-ticket agent controls: spawn an adhoc run, re-run the stage
  agent, open a live terminal, view the diff a run produced.
- A diff view (powered by diff2html) for any agent run that touched
  files.

The terminal view talks to the WebSocket bridge `tickets watch`
exposes on `127.0.0.1`. The bridge accepts only the Obsidian origin,
so the panes do not work in other browsers.
