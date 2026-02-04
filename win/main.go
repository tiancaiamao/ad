package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/sminez/ad/win/pkg/ad"
	"github.com/sminez/ad/win/pkg/repl"
)

const (
	DEFAULT_WINDOW_NAME = "+win"
	PROMPT              = "> "
)

// EchoInterpreter is a simple synchronous interpreter that adds line numbers to input.
type EchoInterpreter struct {
	*repl.SimpleSyncInterpreter
	lineNumber int
}

// NewEchoInterpreter creates a new echo interpreter.
func NewEchoInterpreter() (*EchoInterpreter, error) {
	interpreter := &EchoInterpreter{
		lineNumber: 0,
	}

	// Create the underlying sync interpreter
	sync := repl.NewSimpleSyncInterpreter(func(ctx context.Context, input string) (string, error) {
		interpreter.lineNumber++

		// Simple echo with line number
		output := fmt.Sprintf("%d\t%s", interpreter.lineNumber, input)

		return output, nil
	})

	interpreter.SimpleSyncInterpreter = sync

	return interpreter, nil
}

func main() {
	windowName := flag.String("name", DEFAULT_WINDOW_NAME, "window name")
	debug := flag.Bool("debug", false, "enable debug logging")
	flag.Parse()

	if *debug {
		log.SetFlags(log.Ltime | log.Lshortfile)
		log.Println("Starting win REPL with debug logging")
	}

	// Create ad client
	client, err := ad.NewClient()
	if err != nil {
		log.Fatalf("unable to connect to ad: %v", err)
	}
	defer func() {
		if *debug {
			log.Println("Closing client connection")
		}
		client.Close()
	}()

	if *debug {
		log.Println("Connected to ad successfully")
	}

	// Create interpreter
	interpreter, err := NewEchoInterpreter()
	if err != nil {
		log.Fatalf("unable to create interpreter: %v", err)
	}

	// Create REPL handler config
	config := repl.Config{
		Prompt:     PROMPT,
		WindowName: *windowName,
		WelcomeMessage: `# Echo REPL
#
# A simple REPL demonstrating the win framework.
# Type something and press Enter to see the output.
#
# The interpreter adds line numbers to your input.
#
# Usage:
# - Type commands here and press Enter to execute
# - Or select text in another buffer and send it here
#
`,
		Debug:   *debug,
		LogPath: "/tmp/win-repl.log",
	}

	// Create REPL handler
	handler, err := repl.NewHandler(config, client, interpreter)
	if err != nil {
		log.Fatalf("unable to create REPL handler: %v", err)
	}

	// Run the REPL
	if *debug {
		log.Println("Starting REPL...")
	}

	if err := handler.Run(); err != nil {
		log.Fatalf("REPL error: %v", err)
	}

	log.Println("REPL exited cleanly")
}
