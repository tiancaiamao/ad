# win - REPL Framework for ad Text Editor

`win` is a modular REPL framework for the ad text editor, inspired by Plan 9's acme `win`. It provides the ad integration layer and REPL plumbing so different interpreters can be plugged in without changing the UI glue.

## Scope

- This repo provides the **library** (`pkg/ad`, `pkg/repl`) and **cmd/win-cora**.
- The **ai interpreter moved to the ai repo**. Use `win-ai` from `github.com/tiancaiamao/ai` to integrate ai with ad (the ad editor repo is `github.com/tiancaiamao/ad`).

## Build

```bash
cd /Users/genius/project/ad/win
go build -o ~/.ad/bin/win-cora ./cmd/win-cora
```

## Quick Start (Cora)

```bash
# In ad command line
!win-cora
```

- Select text in any buffer and run `send-to-win +win`.
- `win-cora` uses the cora prompt (`0 #> ...`) and strips the `#> ` prefix.
- The `send-to-win` prefix is `;; `.

## Command-Line Options (win-cora)

- `--name <name>`: window name (default `+win`)
- `--debug`: enable debug logging to `/tmp/win-repl.log`
- `--cora-cmd <path>`: cora executable (default `/Users/genius/project/cora/cora`)
- `--cora-args "..."`: extra args for cora (space-separated)

Example:
```bash
!win-cora --debug --name +win
```

## Using win as a Library

- `pkg/ad`: ad 9P client
- `pkg/repl`: REPL handler, interpreter interfaces, and helpers

These are used by `win-cora` and by external interpreters (e.g. `win-ai` in the ai repo).

## AI Integration (Moved)

The ai bridge is no longer in this repo. Build it from the ai repo:

```bash
cd /Users/genius/project/ai
go build -o ~/.ad/bin/win-ai ./cmd/win-ai
```

Then run in ad:
```bash
!win-ai
```

`win-ai` only supports `/command` syntax for controls.

## send-to-win

`send-to-win` takes an optional buffer name. Default is `+win`.

```bash
send-to-win +win
```

The script injects `;; <text>` into the target buffer, and win executes it when the `SendPrefix` matches.

## Debugging

- Run `win-cora --debug` to enable detailed logging in `/tmp/win-repl.log`.
- See `DEBUGGING-TIPS.md` for diagnosing hang issues.
