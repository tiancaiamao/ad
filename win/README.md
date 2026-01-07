# win - Shell REPL for ad text editor

`win` is a Go implementation of a shell REPL for ad text editor,
inspired by Plan 9's acme `win` program.

## Features

- Creates a new window in ad for interactive shell sessions
- Supports bash, zsh, and other POSIX shells
- Event-driven architecture using ad's 9P filesystem interface
- Can run one-time commands or interactive REPL mode
- Compatible with existing ad tooling (e.g., sendtoRepl)

## Installation

```bash
cd win
go build -o ~/.ad/bin/win
```

## Usage

### Interactive Mode

In ad, run:
```
!win
```

This creates a new window named `+win` with an interactive shell.

### One-Time Command Mode

```bash
win --shell=/bin/bash --cmd "ls -la"
```

### Command-Line Options

- `--name <name>`: Window name (default: `+win`)
- `--shell <path>`: Shell path (default: $SHELL)
- `--cmd <command>`: One-time command (interactive mode if empty)

## Integration with ad

`win` integrates seamlessly with ad's filesystem interface:

- Uses ad's 9P socket for communication
- Reads and writes buffer events
- Manages shell I/O through goroutines
- Handles window scrolling automatically

## Compatibility

`win` is designed to be compatible with existing ad tooling:

- Works with `sendtoRepl` script
- Uses same window naming convention (`+win`)
- Compatible with existing shell environments

## Architecture

`win` consists of three main components:

1. **ad package** (`pkg/ad/client.go`): 
   - 9P client for ad communication
   - Buffer operations (read/write body, addr, xaddr, xdot)
   - Event filtering (JSON-based event parsing)
   - Control commands (mark-clean, open-in-new-window)

2. **shell package** (`pkg/shell/shell.go`): 
   - Shell subprocess management
   - stdin/stdout/stderr piping
   - Interactive and one-time command modes

3. **main** (`main.go`): 
   - Event handling (Insert, Delete, Execute)
   - REPL logic with prompt support
   - Signal handling (SIGINT, SIGTERM)

### Event Format

`win` uses JSON-formatted events matching the Rust `ad_event` crate:

```json
{"source":"K","kind":"I","ch_from":17,"ch_to":18,"truncated":false,"txt":"hello"}
```

- **source**: Event source (K=Keyboard, M=Mouse, F=Fsys)
- **kind**: Event type (I=Insert, D=Delete, X=Execute, L=Load)
- **ch_from/ch_to**: Character positions
- **txt**: Text content

## Design Decisions

- **9P Library**: Uses `github.com/hugelgupf/p9` for clean API
- **Shell Default**: Uses $SHELL (your zsh) for better integration
- **Event-Driven**: Follows acme win's event-filtering approach
- **No Special Commands**: Relies on ad's built-in commands (Del, Exit, etc.)
