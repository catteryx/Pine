# Pine

A desktop client for the [Pi coding agent](https://github.com/earendil-works/pi), built with
[MyGo](https://github.com/egoist/mygo) native UI in pure Go.

Pine drives `pi --mode rpc` as a subprocess, one per session, and shows the conversation
beside a full-height sidebar of sessions.

## Features

- **Sessions** in a resizable, searchable sidebar, grouped by working directory; they persist
  across launches and reopen where you left them. Rename or delete from the sidebar.
- **Transcript** with a Markdown renderer (headings, lists, tables, quotes, code with syntax
  highlighting, inline styles, autolinks) and shared multi-line selection. Tool calls show
  diffs, image results and path actions; thinking and tool calls fold into a "Worked for …"
  disclosure once a run finishes.
- **Slash commands**: a menu of client commands (`/new`, `/resume`, `/settings`, `/rename`,
  `/compact`, `/clear`, `/stop`, `/bash`, `/skills`, `/mcp`, `/model`, `/thinking`, `/export`,
  `/auto-compact`, `/auto-retry`, `/abort-retry`, `/steering`, `/follow-up`, `/help`) and Pi's
  own prompt templates, skills and extension commands.
- **Composer** with model and thinking pickers, drag-and-drop files (native macOS icons for
  non-images), clipboard image paste, and Enter to send.
- **MCP** and **Skills** pages in the sidebar.
- **Context ring** with a token and cost popover; a Dock badge when a run finishes.
- **Auto-updates** from signed GitHub releases.

## Requirements

- Go 1.27 or later.
- The Pi CLI (v1.0+) on your `PATH`, or `PI_BIN` set to it.

## Run

```sh
go run .
```

Set `PI_BIN` to use a specific `pi` binary:

```sh
PI_BIN=/path/to/pi go run .
```

## Build

```sh
go tool mygo build
```

Packaged apps go to `build/<os>-<arch>/`. To sign updates, give `mygo build` the update key:

```sh
MYGO_UPDATER_PRIVATE_KEY="$(cat ~/Library/Application\ Support/mygo/update-keys/mygo-update.key)" go tool mygo build
```

## License

MIT
