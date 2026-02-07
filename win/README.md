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
3. More control commands see `pi-ctl /help`

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

**Commands (preferred `/command` syntax):**

Information Commands:
- `/session` - Show pi session state (from pi)
- `/session-state` - Show pi session state (alias for `/session`, deprecated)
- `/messages` - Show all messages history (pi only)
- `/show-usage` - Show session usage statistics (pi only)

Display Settings (win-specific):
- `/show-settings` - Show win display settings (local config)
- `/thinking [on|off|toggle]` - Show/hide AI thinking
- `/tools [on|off|toggle]` - Show/hide full tool output
- `/prefix [on|off|toggle]` - Show/hide label prefixes

Model Management:
- `/model-select` - Launch interactive model selection (calls `pi-model-select`)
- `/models` - Show available models (deprecated, use `/model-select`)
- `/model <number|provider/model-id>` - Set model (deprecated, use `/model-select`)

Session Management:
- `/new` - Start a new pi session
- `/resume [session-path]` - Resume from previous session
  - Without args: launches interactive session selection (calls `pi-session-select`)
  - With path: switches to specified session (e.g., `/resume ~/.pi/agent/sessions/--...--/session.jsonl`)

Context Control:
- `/compact [instructions]` - Manually compact context with optional custom instructions

Settings (pi only):
- `/auto-compaction <on|off>` - Enable/disable auto-compaction
- `/thinking-level <off|minimal|low|medium|high|xhigh>` - Set thinking level
- `/cycle-thinking-level` - Cycle through available thinking levels

Utilities:
- `/copy` - Copy last assistant message to clipboard
- `/abort` - Abort current pi operation
- `/quit` - Exit win
- `/help` - Show all commands and usage

**Backward Compatibility:**
- Commands also work with `:win command` syntax (e.g., `:win session`, `:win new`), but `/command` is preferred.

**Debugging**:
- Run `win --debug` to enable detailed logging to `/tmp/win-repl.log`
- See `DEBUGGING-TIPS.md` for diagnosing卡死 (hang) issues
- `:win status` shows pi's working directory and PID

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

`win-ctl` is a helper for local control commands (pi only). It sends commands via `send-to-win`.

```bash
win-ctl +pi help             # Show all commands
win-ctl +pi show-settings    # Show win display settings
win-ctl +pi session          # Show pi session state
win-ctl +pi messages         # Show message history
win-ctl +pi show-usage       # Show session usage statistics
win-ctl +pi thinking toggle
win-ctl +pi tools off
win-ctl +pi prefix toggle
win-ctl +pi quit
```

**Note:** `win-ctl` accepts both `/command` and `:win command` syntax internally.

## Pi Model Selection

The pi interpreter provides convenient model selection using ad's minibuffer:

```bash
# Interactive model selection with minibuffer
win-model-select

# Or bind to a key (recommended):
# ~/.ad/config.toml:
[keys.normal]
"<space> p m" = { run = "win-model-select" }
```

This will pop up a minibuffer dialog listing all available models. Select one and press Enter.

### Alternative Methods

```bash
# Set model by ID directly
send-to-win +pi ";; /model anthropic/claude-sonnet-4-20250514"

# Set model by number (deprecated - requires listing first)
send-to-win +pi ";; :win models"
send-to-win +pi ";; :win model 0"
```

**Note:** `/model` command is deprecated. Use `pi-model-select` for interactive selection.

### How It Works

1. `pi-model-select` calls `pi-get-models` (a Python RPC client)
2. `pi-get-models` connects to pi in RPC mode and fetches the model list
3. The model list is piped to `minibufferSelect` (from `ad.sh`)
4. User selects a model from the minibuffer dialog
5. The selected model ID is sent to win via `send-to-win`
6. win sends the `set_model` RPC command to pi

## Pi Session Selection

The pi interpreter provides convenient session selection using ad's minibuffer:

```bash
# Interactive session selection with minibuffer
pi-session-select
```

This will list all available sessions with timestamps, sizes, and modification times. Select one to resume.

### Alternative Methods

```bash
# Direct path
send-to-win +pi ";; /resume ~/.pi/agent/sessions/--...--/session.jsonl"

# Show usage
send-to-win +pi ";; /resume"
```

Example keybindings for session management:

```toml
[keys.normal]
# Session selection (minibuffer)
"<space> p r" = { run = "pi-session-select" }

# Show current session state
"<space> p s" = { run = "win-ctl", args = ["+pi", "session"] }

# Show usage statistics
"<space> p u" = { run = "win-ctl", args = ["+pi", "show-usage"] }
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
# Send to buffers
"<space> s p" = { run = "send-to-win", args = ["+pi"] }
"<space> s w" = { run = "send-to-win", args = ["+win"] }

# Display toggles
"<space> t p" = { run = "win-ctl", args = ["+pi", "thinking toggle"] }
"<space> t o" = { run = "win-ctl", args = ["+pi", "tools toggle"] }
"<space> t l" = { run = "win-ctl", args = ["+pi", "prefix toggle"] }

# Model management
"<space> p m" = { run = "win-model-select" }

# Session management
"<space> p r" = { run = "pi-session-select" }
"<space> p n" = { run = "win-ctl", args = ["+pi", "new"] }
"<space> p s" = { run = "win-ctl", args = ["+pi", "session"] }

# Context control
"<space> p c" = { run = "win-ctl", args = ["+pi", "compact"] }
"<space> p C" = { run = "win-ctl", args = ["+pi", "copy"] }

# Utilities
"<space> p a" = { run = "win-ctl", args = ["+pi", "abort"] }
"<space> p h" = { run = "win-ctl", args = ["+pi", "help"] }
```

## License

MIT
