// Example: Shell REPL with persistent subprocess
// This demonstrates an async interpreter that maintains a shell session.
//
// Usage:
//   go run examples/shell_repl.go
//
// Or integrate into main.go by replacing EchoInterpreter with ShellInterpreter.

package main

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"

	"github.com/sminez/ad/win/pkg/repl"
)

// ShellInterpreter maintains a persistent shell subprocess.
type ShellInterpreter struct {
	*repl.BaseInterpreter
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
	cancel context.CancelFunc
	mu     sync.Mutex
}

// NewShellInterpreter creates a new shell interpreter.
func NewShellInterpreter(shell string, args ...string) (*ShellInterpreter, error) {
	cmd := exec.Command(shell, args...)
	cmd.Stdin = nil // We'll set this separately
	cmd.Stdout = nil
	cmd.Stderr = nil // Merge stderr with stdout

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("create stderr pipe: %w", err)
	}

	interpreter := &ShellInterpreter{
		BaseInterpreter: repl.NewBaseInterpreter(true), // Streaming
		cmd:             cmd,
		stdin:           stdin,
		stdout:          stdout,
		stderr:          stderr,
	}

	// Setup pipes (cmd.StdinPipe etc. already configured)
	// The pipes are already connected to the cmd

	return interpreter, nil
}

// Start starts the shell subprocess and begins streaming output.
func (s *ShellInterpreter) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	childCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	// Start the subprocess
	if err := s.cmd.Start(); err != nil {
		return fmt.Errorf("start shell: %w", err)
	}

	// Start streaming output in background
	go s.streamOutput(childCtx)

	return nil
}

// streamOutput reads stdout/stderr and writes to the buffer.
func (s *ShellInterpreter) streamOutput(ctx context.Context) {
	merged := io.MultiReader(s.stdout, s.stderr)

	writer := s.GetOutputWriter()
	if writer != nil {
		if err := repl.CopyReader(ctx, merged, writer, 1024); err != nil {
			if ctx.Err() != nil {
				return
			}
		}
	} else {
		io.Copy(io.Discard, merged)
	}
}

// GetOutputWriter returns the configured output writer.
func (s *ShellInterpreter) GetOutputWriter() repl.OutputWriter {
	return s.BaseInterpreter.GetOutputWriter()
}

// SendInput sends a command to the shell.
func (s *ShellInterpreter) SendInput(input string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stdin == nil {
		return fmt.Errorf("stdin not available")
	}

	// Write the command
	if _, err := s.stdin.Write([]byte(input)); err != nil {
		return fmt.Errorf("write to stdin: %w", err)
	}

	// Add newline if not present
	if len(input) == 0 || input[len(input)-1] != '\n' {
		if _, err := s.stdin.Write([]byte("\n")); err != nil {
			return fmt.Errorf("write newline: %w", err)
		}
	}

	return nil
}

// Stop terminates the shell subprocess.
func (s *ShellInterpreter) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cancel != nil {
		s.cancel()
	}

	if s.stdin != nil {
		s.stdin.Close()
		s.stdin = nil
	}

	if s.stdout != nil {
		s.stdout.Close()
		s.stdout = nil
	}

	if s.stderr != nil {
		s.stderr.Close()
		s.stderr = nil
	}

	if s.cmd != nil && s.cmd.Process != nil {
		if err := s.cmd.Process.Kill(); err != nil {
			return fmt.Errorf("kill shell: %w", err)
		}
	}

	return nil
}

// Process implements Interpreter.Process.
func (s *ShellInterpreter) Process(ctx context.Context, input string) error {
	// Input is already present in the buffer (typed or send-to-win), so just
	// forward it to the subprocess.
	if err := s.SendInput(input); err != nil {
		return err
	}
	return nil
}

// SetOutputWriter implements AsyncInterpreter.SetOutputWriter.
func (s *ShellInterpreter) SetOutputWriter(writer repl.OutputWriter) {
	s.BaseInterpreter.SetOutputWriter(writer)
}
