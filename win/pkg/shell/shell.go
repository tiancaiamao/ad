package shell

import (
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// Shell wraps a shell subprocess for interactive or one-time execution
type Shell struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
}

// NewShell creates a new shell instance
// If interactive is true, runs shell in interactive mode (-i flag)
// If cmd is non-empty, runs that command instead of starting interactive mode
func NewShell(shellPath string, envVars []string, interactive bool, cmd string) (*Shell, error) {
	var shellCmd *exec.Cmd

	if cmd != "" {
		// One-time command execution
		shellCmd = exec.Command(shellPath, "-c", cmd)
	} else if interactive {
		// Interactive shell
		shellCmd = exec.Command(shellPath, "-i")
	} else {
		// Non-interactive mode - simple pipes
		// Special handling for Python: use custom REPL without prompts
		if strings.Contains(shellPath, "python") {
			shellCmd = exec.Command(shellPath, "-u", "-c", `
import sys
import code

# Disable prompts by setting ps1 and ps2 to empty strings
sys.ps1 = ''
sys.ps2 = ''

# Create interactive console
console = code.InteractiveConsole()
console.interact(banner='', exitmsg='')
`)
		} else {
			// For other programs, just run them
			shellCmd = exec.Command(shellPath)
		}
	}

	shellCmd.Env = envVars

	// Set up simple pipes (no PTY)
	stdin, err := shellCmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdin pipe: %w", err)
	}

	stdout, err := shellCmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("create stdout pipe: %w", err)
	}

	stderr, err := shellCmd.StderrPipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("create stderr pipe: %w", err)
	}

	// Start the process
	if err := shellCmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		stderr.Close()
		return nil, fmt.Errorf("start shell: %w", err)
	}

	return &Shell{
		cmd:    shellCmd,
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
	}, nil
}

// Stdout returns the stdout reader
func (s *Shell) Stdout() io.Reader {
	return s.stdout
}

// Stderr returns the stderr reader
func (s *Shell) Stderr() io.Reader {
	return s.stderr
}

// Write writes a command to the shell's stdin
func (s *Shell) Write(cmd string) error {
	_, err := s.stdin.Write([]byte(cmd))
	return err
}

// Wait waits for the shell process to complete
func (s *Shell) Wait() error {
	if s.cmd != nil && s.cmd.Process != nil {
		return s.cmd.Wait()
	}
	return nil
}

// Close closes the shell and cleans up resources
func (s *Shell) Close() error {
	if s.stdin != nil {
		s.stdin.Close()
	}

	if s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Kill()
		s.cmd.Wait()
	}

	if s.stdout != nil {
		s.stdout.Close()
	}
	if s.stderr != nil {
		s.stderr.Close()
	}

	return nil
}
