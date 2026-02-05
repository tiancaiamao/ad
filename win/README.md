# win - REPL Framework for ad Text Editor

`win` is a modular REPL framework for the ad text editor, inspired by Plan 9's acme `win`. It separates the ad integration layer from interpreter logic so you can swap interpreters without changing the UI glue.

**Features**
- Event-driven architecture over ad's 9P filesystem interface
- Modular design: REPL framework vs interpreter logic
- Sync and async interpreters
- Streaming output to ad buffers
- Viewport and focus management
- Outcome-based event control (Handled/Passthrough/Exit)
- Source tracking (Keyboard/Mouse/Fsys)

## Installation

```bash
cd win
go build -o ~/.ad/bin/win
```

## Quick Start

**Pi (RPC)**
1. `!win --interp pi`
2. Select text in any buffer and run `send-to-win +pi`
3. Control output with `win-ctl +pi thinking|tools|prefix toggle`

**Cora (Lisp)**
1. Make sure `/Users/genius/project/cora/cora` exists (see `win/cora.go`)
2. `!win --interp cora`
3. Select text and run `send-to-win +win`

## Command-Line Options

- `--interp <name>`: `cora|pi|shell` (default: `cora`)
- `--name <name>`: window name (default: `+win`, or `+pi` when `--interp pi`)
- `--debug`: enable debug logging to `/tmp/win-repl.log`
- `--pi-cmd <path>`: pi executable (default: `pi`)
- `--pi-args "..."`: extra args for pi (space-separated)

Example:
```bash
!win --interp pi --pi-args "--provider zai --model glm-4.6"
```

## Interpreters

**cora**
- Uses `/Users/genius/project/cora/cora` (hard-coded in `win/cora.go`).
- Interpreter prompt is produced by cora. `InputPrefix` is set to `#> ` so `0 #> ...` is stripped before execution.
- `send-to-win` prefix is `;; `.

**pi (RPC)**
- Runs `pi --mode rpc` and streams events to the buffer.
- Execution is **send-to-win only**. Keyboard input is just text (no command execution).
- Tool output streams live into `+pi` with prefixes.
Controls (handled locally by win):
- `:win thinking on|off|toggle`
- `:win tools on|off|toggle`
- `:win prefix on|off|toggle`
- `:win status`

**shell**
- Uses a persistent `zsh -i` subprocess.
- Keyboard Enter executes input (same as other async REPLs).
- `send-to-win` works with the same `;; ` prefix.

## send-to-win

`send-to-win` takes an optional buffer name. Default is `+win`.

```bash
send-to-win +pi
send-to-win +win
```

The script injects `;; <text>` into the target buffer, and win executes it when `SendPrefix` matches.

## win-ctl

`win-ctl` is a helper for local control commands (pi only). It sends `:win ...` via `send-to-win`.

```bash
win-ctl +pi thinking toggle
win-ctl +pi tools off
win-ctl +pi prefix toggle
win-ctl +pi status
```

## Pi API Key (no env var)

pi can read API keys from `~/.pi/agent/auth.json` (preferred over env vars).

```json
{
  "zai": { "type": "api_key", "key": "YOUR_KEY" }
}
```

File permissions should be `0600`.

## Event Flow

- `send-to-win` writes `;; <text>` to the REPL buffer (Source: Fsys).
- win detects the prefix and routes input to the interpreter.
- Interpreter output is streamed back to the same buffer (Source: Fsys).
- Keyboard input is normal text. In `pi` mode, Enter does not execute anything.

## API Reference (repl)

**Config**
```go
type Config struct {
    Prompt               string
    WindowName           string
    WelcomeMessage       string
    SendPrefix           string
    InputPrefix          string
    EchoSendInput        bool
    EnableKeyboardExecute bool
    EnableExecute        bool
    Debug                bool
    LogPath              string
}
```

**Interpreter**
```go
type Interpreter interface {
    Process(ctx context.Context, input string) error
    Start(ctx context.Context) error
    Stop() error
    IsStreaming() bool
}
```

**SyncInterpreter**
```go
type SyncInterpreter interface {
    Interpreter
    Run(ctx context.Context, input string) (string, error)
}
```

**AsyncInterpreter**
```go
type AsyncInterpreter interface {
    Interpreter
    SetOutputWriter(writer OutputWriter)
    SendInput(input string) error
}
```

**ControlInterpreter**
```go
type ControlInterpreter interface {
    HandleControl(input string) (bool, error)
}
```

**OutputWriter**
```go
type OutputWriter interface {
    Write(output string) error
    WritePrompt() error
    ScrollToBottom() error
    Flush() error
}
```

## Configuration Example

Add bindings in `~/.ad/config.toml`:

```toml
[keys.normal]
"<space> s p" = { run = "send-to-win +pi" }
"<space> s w" = { run = "send-to-win +win" }
"<space> t p" = { run = "win-ctl +pi thinking toggle" }
```

## License

MIT
