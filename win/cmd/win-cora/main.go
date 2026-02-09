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
	defaultWindowName = "+win"
	sendPrefix        = ";; "
	inputPrefix       = "#> "
)

func main() {
	windowName := flag.String("name", "", "window name (default +win)")
	debug := flag.Bool("debug", false, "enable debug logging")
	coraCmd := flag.String("cora-cmd", "/Users/genius/project/cora/cora", "cora executable")
	coraArgs := flag.String("cora-args", "", "extra args for cora (space-separated)")
	flag.Parse()

	if *debug {
		log.SetFlags(log.Ltime | log.Lshortfile)
		log.Println("Starting win-cora REPL with debug logging")
	}

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

	extraArgs := strings.Fields(*coraArgs)
	interpreter, err := NewCoraInterpreter(*coraCmd, extraArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating interpreter: %v\n", err)
		os.Exit(1)
	}
	defer interpreter.Stop()

	name := *windowName
	if name == "" {
		name = defaultWindowName
	}

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
		SendPrefix:            sendPrefix,
		InputPrefix:           inputPrefix,
		EchoSendInput:         false,
		EnableKeyboardExecute: true,
		EnableExecute:         true,
		Debug:                 *debug,
		LogPath:               "/tmp/win-repl.log",
	}

	handler, err := repl.NewHandler(config, client, interpreter)
	if err != nil {
		log.Fatalf("unable to create REPL handler: %v", err)
	}

	if *debug {
		log.Println("Starting REPL...")
	}

	if err := handler.Run(); err != nil {
		log.Fatalf("REPL error: %v", err)
	}

	log.Println("REPL exited cleanly")
}
