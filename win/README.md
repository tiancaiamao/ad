# win - REPL Framework for ad Text Editor

`win` is a REPL framework for the ad text editor, inspired by Plan 9's acme `win` program. It provides a clean event-driven architecture for building interactive REPL interfaces.

## Features

- **Event-driven architecture** using ad's 9P filesystem interface
- **Separation of concerns**: REPL framework vs interpreter logic
- **Stateless design**: Each command is processed independently
- **Viewport control**: Auto-scroll to show latest output
- **Focus management**: Keep focus in working buffer while REPL scrolls in background
- **Easy to extend**: Implement `Process(input) -> (output, error)` to create new REPLs

## Installation

```bash
cd win
go build -o ~/.ad/bin/win
```

## Usage

### Basic REPL

In ad, run:
```
!win
```

This creates a new window named `+win` with a simple echo interpreter that adds line numbers to input.

### Sending Commands from Other Buffers

Select text in any buffer and press `C-t` (if configured) to send it to the REPL.

### Command-Line Options

- `--name <name>`: Window name (default: `+win`)
- `--debug`: Enable debug logging to `/tmp/win.log`

## Integration with ad

`win` integrates seamlessly with ad's filesystem interface:

- Uses ad's 9P socket for communication
- Monitors buffer events via `RunEventFilter`
- Direct buffer manipulation (body, addr, xaddr, xdot)
- Viewport control (viewport-bottom) for auto-scrolling

## Architecture

### Event System

`win` uses JSON-formatted events from ad's buffer event file:

```json
{"source":"K","kind":"I","ch_from":17,"ch_to":18,"truncated":false,"txt":"hello"}
```

- **source**: Event source (K=Keyboard, M=Mouse, F=Fsys)
- **kind**: Event type (I=Insert, D=Delete, X=Execute, L=Load)
- **ch_from/ch_to**: Character positions
- **txt**: Text content

### Interpreter Interface

To create a custom REPL, implement the `Interpreter` interface:

```go
type Interpreter interface {
    Process(input string) (string, error)
    Close() error
}
```

Examples:
- **Shell REPL**: Wrap bash/zsh with stdin/stdout pipes
- **Python REPL**: Use `python -i` or ptpython
- **LLM Chat**: Send prompts to Claude/GPT API
- **Database**: SQL query REPL with psql/mysql

### Viewport and Focus Management

The framework handles two scenarios:

1. **Manual input** (user types in +win and presses Enter):
   - Cursor moves to end of output
   - Focus stays in +win buffer

2. **Command execution** (sent from another buffer):
   - Output is displayed
   - +win viewport scrolls to bottom
   - Focus returns to original buffer
   - User can continue working in code buffer while seeing REPL output

## Creating Custom REPLs

### Example: Python REPL

```go
type PythonInterpreter struct {}

func NewPythonInterpreter() (*PythonInterpreter, error) {
    return &PythonInterpreter{}, nil
}

func (p *PythonInterpreter) Process(input string) (string, error) {
    cmd := exec.Command("python3", "-c", input)
    var stdout, stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr
    if err := cmd.Run(); err != nil {
        return stderr.String(), nil
    }
    return stdout.String(), nil
}

func (p *PythonInterpreter) Close() error {
    return nil
}
```

Then replace `NewEchoInterpreter()` with `NewPythonInterpreter()` in `main()`.

### Example: Shell REPL

```go
type ShellInterpreter struct {
    shellPath string
    envVars   []string
}

func NewShellInterpreter(shellPath string) (*ShellInterpreter, error) {
    return &ShellInterpreter{
        shellPath: shellPath,
        envVars:   os.Environ(),
    }, nil
}

func (s *ShellInterpreter) Process(input string) (string, error) {
    cmd := exec.Command(s.shellPath, "-c", input)
    cmd.Env = s.envVars
    var stdout, stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr
    if err := cmd.Run(); err != nil {
        return stderr.String(), nil
    }
    return stdout.String(), nil
}

func (s *ShellInterpreter) Close() error {
    return nil
}
```

## Design Decisions

- **9P Library**: Uses `9fans.net/go/plan9` for Plan 9 protocol
- **Event Filtering**: External event handling via 9p event file
- **Stateless**: Each command spawns a fresh process (avoids pipe buffering issues)
- **External Composition**: Follows Plan 9 / Acme philosophy of external tools over internal features
- **Viewport over Focus**: Scrolls REPL window without stealing focus from work buffer

## Comparison with @win/ Implementation

This new implementation is superior to the previous `@win/` approach:

- **Simpler**: No need for ad to understand "win" or special window types
- **More robust**: Event-driven via filesystem, not internal window management
- **Extensible**: Interpreter interface allows any REPL backend
- **Fewer bugs**: Stateless design avoids process pipe and buffering issues
- **Better UX**: Viewport scrolling without focus stealing

## Configuration Example

Add to `~/.ad/config.toml`:

```toml
[keys.normal]
"C-w" = { run = "win" }
"C-t" = { run = "send-to-win" }
```

Where `send-to-win` sends selected text to the +win buffer (similar to `send-to-echo`).
