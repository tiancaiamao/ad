# win - REPL Framework for ad Text Editor

`win` is a modular, extensible REPL framework for the ad text editor, inspired by Plan 9's acme `win` program. It provides a clean separation between the ad integration layer and the interpreter logic, making it easy to build custom REPL interfaces.

## Features

- **Event-driven architecture** using ad's 9P filesystem interface
- **Modular design**: REPL framework vs interpreter logic
- **Support for both synchronous and asynchronous interpreters**:
  - **Sync interpreters**: Stateless, one-shot command execution (e.g., Python `python -c "..."`, shell `sh -c "..."`)
  - **Async interpreters**: Persistent subprocess with streaming output (e.g., interactive shell `zsh -i`)
- **Streaming output support**: Real-time incremental output to ad buffer
- **Viewport and focus management**: Auto-scroll to show latest output while preserving focus in working buffer
- **Outcome-based event control**: Fine-grained control over event handling (Handled/Passthrough/Exit)
- **Source tracking**: Differentiate between keyboard input, mouse, and filesystem writes
- **Easy to extend**: Simple interface implementations

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

This creates a new window named `+win` with a simple echo interpreter.

### Command-Line Options

- `--name <name>`: Window name (default: `+win`)
- `--debug`: Enable debug logging to `/tmp/win-repl.log`

## Architecture

### Core Components

```
┌─────────────────────────────────────────────────────────────┐
│                      win (main)                          │
│  ┌──────────────┐         ┌───────────────────────────┐  │
│  │ ad.Client    │◄──────►│ REPL Handler             │  │
│  │ (9P bridge)  │         │  - Event routing        │  │
│  └──────────────┘         │  - Buffer management     │  │
│                          │  - Viewport control      │  │
│                          └───────┬─────────────────┘  │
└──────────────────────────────────────┼───────────────────┘
                                       │
                    ┌──────────────────┴──────────────────┐
                    │                                     │
        ┌───────────▼──────────┐             ┌──────────▼─────────┐
        │ Sync Interpreter     │             │ Async Interpreter  │
        │ - One-shot commands │             │ - Persistent proc   │
        │ - Returns output    │             │ - Streams output   │
        └────────────────────┘             └──────────────────┘
```

### Event Flow

1. **User types in REPL buffer** → `EventFilter` receives `Insert` event (Source: Keyboard)
2. **User presses Enter** → `HandleInsert` extracts last line, calls `ExecuteCommand`
3. **Interpreter processes input** → Output written to buffer (Source: Fsys)
4. **Viewport scrolled** → User sees output in real-time

### Event Sources

| Source | Description | Handling |
|--------|-------------|-----------|
| `K` (Keyboard) | User typing | Processed by REPL handler |
| `M` (Mouse) | Mouse clicks | Passthrough to ad |
| `F` (Fsys) | Our own writes | Handled (no passthrough) |

### Outcomes

| Outcome | Description |
|---------|-------------|
| `Handled` | Event fully processed, don't pass to ad |
| `Passthrough` | Pass event back to ad for internal processing |
| `PassthroughAndExit` | Pass to ad then exit event filter |
| `Exit` | Exit without passing event |

## Creating Custom REPLs

### Synchronous Interpreter

Simple one-shot command execution:

```go
import (
    "bytes"
    "context"
    "fmt"
    "os/exec"
    "github.com/sminez/ad/win/pkg/repl"
)

type PythonInterpreter struct {
    *repl.SimpleSyncInterpreter
}

func NewPythonInterpreter() *PythonInterpreter {
    sync := repl.NewSimpleSyncInterpreter(func(ctx context.Context, input string) (string, error) {
        cmd := exec.CommandContext(ctx, "python3", "-c", input)
        var stdout, stderr bytes.Buffer
        cmd.Stdout = &stdout
        cmd.Stderr = &stderr

        if err := cmd.Run(); err != nil {
            return stderr.String(), nil // Return stderr as output
        }
        return stdout.String(), nil
    })

    return &PythonInterpreter{SimpleSyncInterpreter: sync}
}
```

### Asynchronous Interpreter

Persistent subprocess with streaming output:

```go
import (
    "context"
    "fmt"
    "io"
    "os/exec"
    "sync"
    "github.com/sminez/ad/win/pkg/repl"
)

type ZshInterpreter struct {
    *repl.BaseInterpreter
    cmd    *exec.Cmd
    stdin  io.WriteCloser
    stdout io.ReadCloser
    mu     sync.Mutex
}

func NewZshInterpreter() (*ZshInterpreter, error) {
    cmd := exec.Command("zsh", "-i")

    stdin, err := cmd.StdinPipe()
    if err != nil {
        return nil, err
    }

    stdout, err := cmd.StdoutPipe()
    if err != nil {
        stdin.Close()
        return nil, err
    }

    stderr, _ := cmd.StderrPipe() // Merge with stdout

    interp := &ZshInterpreter{
        BaseInterpreter: repl.NewBaseInterpreter(true), // streaming
        cmd:             cmd,
        stdin:           stdin,
        stdout:          stdout,
    }

    interp.cmd.Stdin = interp.stdin
    interp.cmd.Stdout = interp.stdout
    interp.cmd.Stderr = stderr

    return interp, nil
}

func (z *ZshInterpreter) Start(ctx context.Context) error {
    z.mu.Lock()
    defer z.mu.Unlock()

    if err := z.cmd.Start(); err != nil {
        return err
    }

    // Stream output to buffer
    go repl.CopyReader(ctx, z.stdout, z.GetOutputWriter(), 1024)

    return nil
}

func (z *ZshInterpreter) SendInput(input string) error {
    z.mu.Lock()
    defer z.mu.Unlock()

    _, err := z.stdin.Write([]byte(input + "\n"))
    return err
}

func (z *ZshInterpreter) Stop() error {
    z.mu.Lock()
    defer z.mu.Unlock()

    if z.cmd.Process != nil {
        z.cmd.Process.Kill()
    }
    return nil
}
```

### Using Custom Interpreters

Replace the interpreter in `main.go`:

```go
func main() {
    // ... client setup ...

    // Use your custom interpreter
    interpreter := NewPythonInterpreter()  // or NewZshInterpreter()

    config := repl.Config{
        Prompt:         ">>> ",
        WindowName:     "+python",
        WelcomeMessage: "# Python REPL\n",
        Debug:          *debug,
    }

    handler, err := repl.NewHandler(config, client, interpreter)
    if err != nil {
        log.Fatal(err)
    }

    if err := handler.Run(); err != nil {
        log.Fatal(err)
    }
}
```

## API Reference

### repl.Config

Configuration for REPL handler.

```go
type Config struct {
    Prompt         string   // User prompt (default: "> ")
    WindowName     string   // ad window name (default: "+repl")
    WelcomeMessage string   // Initial buffer content
    Debug          bool     // Enable debug logging
    LogPath        string   // Log file path (default: "/tmp/repl.log")
}
```

### repl.Interpreter

Base interface for all interpreters.

```go
type Interpreter interface {
    Process(ctx context.Context, input string) error
    Start(ctx context.Context) error
    Stop() error
    IsStreaming() bool
}
```

### repl.SyncInterpreter

For synchronous (one-shot) interpreters.

```go
type SyncInterpreter interface {
    Interpreter
    Run(ctx context.Context, input string) (string, error)
}
```

### repl.AsyncInterpreter

For asynchronous (streaming) interpreters.

```go
type AsyncInterpreter interface {
    Interpreter
    SetOutputWriter(writer OutputWriter)
    SendInput(input string) error
}
```

### repl.OutputWriter

For streaming output to ad buffer.

```go
type OutputWriter interface {
    Write(output string) error
    WritePrompt() error
    ScrollToBottom() error
    Flush() error
}
```

### repl.Handler

Main REPL handler.

```go
type Handler struct{}

func NewHandler(config Config, client *ad.Client, interpreter Interpreter) (*Handler, error)
func (h *Handler) Start() error
func (h *Handler) Stop() error
func (h *Handler) Run() error
func (h *Handler) BufferID() string
```

### ad.Client

9P client for communicating with ad.

```go
client := ad.NewClient()
defer client.Close()

// Window management
client.OpenInNewWindow("+name")  // returns bufferID

// Buffer operations
client.ReadBody(bufferID)
client.WriteBody(bufferID, content)
client.AppendToBody(bufferID, content)
client.AppendToBodyWithSource(bufferID, content, source)

// Address and viewport
client.WriteAddr(bufferID, addr)      // Set cursor position
client.ScrollToBottom(bufferID)       // Scroll to end
client.CenterViewport(bufferID)       // Center and restore focus
client.FocusBuffer(bufferID)           // Set focus

// Control
client.MarkClean(bufferID)            // Mark as not modified
client.SetViewport("viewport-bottom")  // Viewport command
```

## Comparison with Original win

The new framework improves upon the previous implementation:

| Feature | Old win | New win |
|---------|----------|---------|
| **Architecture** | Monolithic | Modular (framework + interpreters) |
| **Async support** | ❌ No | ✅ Yes (persistent subprocess) |
| **Streaming output** | ❌ Batch writes | ✅ Real-time incremental |
| **Event control** | Error returns only | ✅ Outcome enum (Handled/Passthrough/Exit) |
| **Source tracking** | Prompt prefix detection | ✅ Explicit EventSource type |
| **Viewport** | ✅ CenterViewport | ✅ ScrollToBottom + CenterViewport |
| **Extensibility** | Copy-paste code | ✅ Clean interfaces |
| **Testing** | Difficult | ✅ Can unit test interpreters |

## Design Decisions

- **9P Library**: Uses `9fans.net/go/plan9` for Plan 9 protocol
- **Event Filtering**: External event handling via 9P event file
- **Dual interpreter modes**: Sync for simple cases, async for complex sessions
- **External composition**: Follows Plan 9 / Acme philosophy of external tools over internal features
- **Outcome-based control**: Precise control over event flow without callbacks
- **Source tracking**: Explicit typing prevents confusion between user input and our writes
- **Real-time streaming**: Incremental output with buffering for efficiency

## Configuration Example

Add to `~/.ad/config.toml`:

```toml
[keys.normal]
"C-w" = { run = "win --name +python" }
"C-p" = { run = "win --name +shell" }
```

Where `send-to-win` sends selected text to the REPL window (create this script).

## Future Work

- **Pi integration**: Use `pi --mode rpc` as interpreter
- **Multi-mode REPL**: Support multiple interpreters in one window
- **Session persistence**: Save/load REPL sessions
- **Rich output**: Support syntax highlighting, images in REPL output
- **Command history**: Per-interpreter command history
- **Completions**: Shell-style tab completions

## License

MIT
