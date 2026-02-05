package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/sminez/ad/win/pkg/ad"
	"github.com/sminez/ad/win/pkg/repl"
)

const (
	DEFAULT_WINDOW_NAME = "+win"
	SEND_PREFIX         = ";; "
	INPUT_PREFIX        = "#> "
)

func main() {
	interp := flag.String("interp", "cora", "interpreter: cora|pi|shell")
	windowName := flag.String("name", "", "window name (default depends on interpreter)")
	debug := flag.Bool("debug", false, "enable debug logging")
	piCmd := flag.String("pi-cmd", "pi", "pi executable")
	piArgs := flag.String("pi-args", "", "extra args for pi (space-separated)")
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

	var interpreter repl.Interpreter
	switch *interp {
	case "cora":
		interpreter, err = NewCoraInterpreter()
	case "pi":
		extraArgs := strings.Fields(*piArgs)
		interpreter = NewPiInterpreter(*piCmd, extraArgs, *debug)
	case "shell":
		interpreter, err = NewShellInterpreter("zsh", "-i")
	default:
		log.Fatalf("unknown interpreter: %s", *interp)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating interpreter: %v\n", err)
		os.Exit(1)
	}
	defer interpreter.Stop()

	name := *windowName
	if name == "" {
		if *interp == "pi" {
			name = "+pi"
		} else {
			name = DEFAULT_WINDOW_NAME
		}
	}

	// Create REPL handler config
	config := repl.Config{
		Prompt:     "",
		WindowName: name,
		WelcomeMessage: `# Cora REPL
#
# Interpreter prompt comes from cora (e.g. "0 #> ").
# send-to-win prefix: ";; "
#
# Usage:
# - Type commands here and press Enter to execute
# - Or select text in another buffer and send it here
#
`,
		SendPrefix:            SEND_PREFIX,
		InputPrefix:           INPUT_PREFIX,
		EchoSendInput:         false,
		EnableKeyboardExecute: true,
		EnableExecute:         true,
		Debug:                 *debug,
		LogPath:               "/tmp/win-repl.log",
	}
	if *interp == "pi" {
		config.WelcomeMessage = `# Pi REPL
#
# Use send-to-win to send prompts (prefix ";; ").
# Controls: use win-ctl (or send ":win ..." via send-to-win).
#
`
		config.InputPrefix = ""
		config.EchoSendInput = true
		config.EnableKeyboardExecute = false
		config.EnableExecute = false
	} else if *interp == "shell" {
		config.WelcomeMessage = `# Shell REPL
#
# Use send-to-win to send commands (prefix ";; ").
#
`
		config.InputPrefix = ""
		config.EchoSendInput = true
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
