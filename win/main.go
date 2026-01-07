package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/sminez/ad/win/pkg/ad"
	"github.com/sminez/ad/win/pkg/shell"
)

const (
	DEFAULT_WINDOW_NAME = "+win"
	DEFAULT_PROMPT      = "$ "
)

func main() {
	windowName := flag.String("name", DEFAULT_WINDOW_NAME, "window name")
	defaultShell := os.Getenv("SHELL")
	if defaultShell == "" {
		// Try rc first (Plan 9 shell), fall back to sh
		if _, err := exec.LookPath("rc"); err == nil {
			defaultShell = "rc"
		} else {
			defaultShell = "/bin/sh"
		}
	}
	shellPath := flag.String("shell", defaultShell, "shell path")
	oneTimeCmd := flag.String("cmd", "", "one-time command (interactive mode if empty)")
	debug := flag.Bool("debug", false, "enable debug logging")
	flag.Parse()

	if *debug {
		log.SetFlags(log.Ltime | log.Lshortfile)
		log.Println("Starting win with debug logging")
	}

	client, err := ad.NewClient()
	if err != nil {
		log.Fatalf("unable to connect to ad\n%v", err)
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

	bufferID, err := client.OpenInNewWindow(*windowName)
	if err != nil {
		log.Fatalf("unable to create window\n%v", err)
	}

	if *debug {
		log.Printf("Created window %s with buffer ID: %s\n", *windowName, bufferID)
	}

	if *oneTimeCmd != "" {
		executeOneTimeCommand(client, bufferID, *oneTimeCmd, *shellPath)
		return
	}

	startInteractiveREPL(client, bufferID, *shellPath, *debug)
}

func executeOneTimeCommand(client *ad.Client, bufferID, cmd, shellPath string) {
	envVars := os.Environ()
	sh, err := shell.NewShell(shellPath, envVars, false, cmd)
	if err != nil {
		log.Fatalf("unable to create shell\n%v", err)
	}
	defer sh.Close()

	bodyWriter := client.BodyWriter(bufferID)
	go func() {
		io.Copy(bodyWriter, sh.Stdout())
	}()
	go func() {
		io.Copy(bodyWriter, sh.Stderr())
	}()

	sh.Wait()
}

func startInteractiveREPL(client *ad.Client, bufferID, shellPath string, debug bool) {
	if debug {
		log.Println("Initializing REPL window")
	}

	if err := client.WriteBody(bufferID, DEFAULT_PROMPT); err != nil {
		log.Fatalf("unable to initialize window\n%v", err)
	}
	if err := client.WriteAddr(bufferID, "$"); err != nil {
		log.Fatalf("unable to set address\n%v", err)
	}

	if debug {
		log.Println("Starting shell subprocess")
	}

	envVars := os.Environ()
	envVars = append(envVars,
		fmt.Sprintf("prompt=%s", DEFAULT_PROMPT),
		"TERM=dumb",          // Disable fancy terminal features
		"NO_COLOR=1",         // Disable colors
		"CLICOLOR=0",         // Disable CLI colors
	)

	// DO NOT use interactive mode (-i) as it may interfere with terminal control
	// Use a non-interactive shell instead
	sh, err := shell.NewShell(shellPath, envVars, false, "")
	if err != nil {
		log.Fatalf("unable to create shell\n%v", err)
	}
	defer sh.Close()

	if debug {
		log.Println("Shell started successfully")
	}

	// For Python and other interpreters, force unbuffered output
	if strings.Contains(shellPath, "python") {
		if debug {
			log.Println("Detected Python - forcing unbuffered mode")
		}
		// Add PYTHONUNBUFFERED to environment
		envVars = append(envVars, "PYTHONUNBUFFERED=1")
		// Restart shell with new env
		sh.Close()
		sh, err = shell.NewShell(shellPath, envVars, false, "")
		if err != nil {
			log.Fatalf("unable to recreate shell\n%v", err)
		}
	}

	// Stream shell output to buffer
	if debug {
		log.Println("Starting stdout copy goroutine")
	}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := sh.Stdout().Read(buf)
			if n > 0 {
				if err := client.WriteBody(bufferID, string(buf[:n])); err != nil {
					if debug {
						log.Printf("stdout write error: %v\n", err)
					}
					return
				}
				if err := client.WriteAddr(bufferID, "$"); err != nil {
					if debug {
						log.Printf("write-addr error: %v\n", err)
					}
				}
			}
			if err != nil {
				if debug && err != io.EOF {
					log.Printf("stdout read error: %v\n", err)
				}
				return
			}
		}
	}()
	
	if debug {
		log.Println("Starting stderr copy goroutine")
	}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := sh.Stderr().Read(buf)
			if n > 0 {
				if err := client.WriteBody(bufferID, string(buf[:n])); err != nil {
					if debug {
						log.Printf("stderr write error: %v\n", err)
					}
					return
				}
				if err := client.WriteAddr(bufferID, "$"); err != nil {
					if debug {
						log.Printf("write-addr error: %v\n", err)
					}
				}
			}
			if err != nil {
				if debug && err != io.EOF {
					log.Printf("stderr read error: %v\n", err)
				}
				return
			}
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		if debug {
			log.Println("Received signal, shutting down")
		}
		sh.Close()
		os.Exit(0)
	}()

	handler := &ReplHandler{
		bufferID: bufferID,
		shell:    sh,
		prompt:   DEFAULT_PROMPT,
		debug:    debug,
		client:   client,
	}

	if debug {
		log.Println("Starting event filter loop")
	}

	// Run event filter to handle user input
	if err := client.RunEventFilter(bufferID, handler); err != nil {
		log.Fatalf("event filter died\n%v", err)
	}
}

type ReplHandler struct {
	bufferID string
	shell    *shell.Shell
	prompt   string
	debug    bool
	client   *ad.Client
}

func (h *ReplHandler) HandleInsert(source string, from, to int, txt string, client *ad.Client) error {
	if h.debug {
		log.Printf("HandleInsert: source=%s, from=%d, to=%d, txt=%q\n", source, from, to, txt)
	}

	if err := client.MarkClean(h.bufferID); err != nil {
		return fmt.Errorf("mark-clean failed: %w", err)
	}

	// If this is output from the shell, just move dot to EOF
	if source == "F" {
		return client.WriteAddr(h.bufferID, "$")
	}

	// When user presses enter, send the last line they typed
	if txt == "\n" {
		if err := client.WriteXAddr(h.bufferID, "$"); err != nil {
			return fmt.Errorf("write-xaddr failed: %w", err)
		}

		xaddr, err := client.ReadXAddr(h.bufferID)
		if err != nil {
			return fmt.Errorf("read-xaddr failed: %w", err)
		}

		addr, err := client.ReadAddr(h.bufferID)
		if err != nil {
			return fmt.Errorf("read-addr failed: %w", err)
		}

		// Only execute if cursor is at EOF
		if xaddr == addr {
			// Read the last line before the newline
			if err := client.WriteXAddr(h.bufferID, "$-1"); err != nil {
				return fmt.Errorf("write-xaddr failed: %w", err)
			}

			raw, err := client.ReadXDot(h.bufferID)
			if err != nil {
				return fmt.Errorf("read-xdot failed: %w", err)
			}

			// Strip the prompt and any shell output artifacts
			input := stripPrompt(raw, h.prompt)
			input = strings.TrimSpace(input)
			
			if input != "" {
				return h.sendInput(input, client)
			}
		}
	}

	return nil
}

func (h *ReplHandler) HandleDelete(source string, from, to int, client *ad.Client) error {
	return client.MarkClean(h.bufferID)
}

func (h *ReplHandler) HandleExecute(source string, from, to int, txt string, client *ad.Client) error {
	input := stripPrompt(txt, h.prompt)

	// Echo the command with prompt
	content := fmt.Sprintf("\n%s%s\n", h.prompt, strings.TrimSpace(input))
	if err := client.AppendToBody(h.bufferID, content); err != nil {
		return fmt.Errorf("append-to-body failed: %w", err)
	}

	return h.sendInput(input, client)
}

func (h *ReplHandler) sendInput(input string, client *ad.Client) error {
	// Trim whitespace and ensure single newline at end
	cmd := strings.TrimSpace(input) + "\n"

	if h.debug {
		log.Printf("Sending to shell: %q\n", cmd)
	}

	if err := h.shell.Write(cmd); err != nil {
		return fmt.Errorf("shell write failed: %w", err)
	}

	// Add a small delay to let output complete, then add prompt
	go func() {
		time.Sleep(300 * time.Millisecond)
		if err := client.AppendToBody(h.bufferID, "\n"+h.prompt); err == nil {
			client.WriteAddr(h.bufferID, "$")
		}
	}()

	return nil
}

func stripPrompt(s, prompt string) string {
	// Strip our own prompt first
	s = strings.TrimPrefix(s, prompt)
	
	// Strip common interpreter prompts
	commonPrompts := []string{
		">>> ",  // Python primary prompt
		"... ",  // Python continuation prompt
		"irb> ", // Ruby irb
		">> ",   // Other REPLs
		"> ",    // Generic prompt
	}
	
	for _, p := range commonPrompts {
		s = strings.TrimPrefix(s, p)
	}
	
	return s
}
