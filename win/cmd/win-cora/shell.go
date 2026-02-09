package main

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"

	"github.com/sminez/ad/win/pkg/repl"
)

// ShellInterpreter maintains a persistent subprocess.
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
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil

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
		BaseInterpreter: repl.NewBaseInterpreter(true),
		cmd:             cmd,
		stdin:           stdin,
		stdout:          stdout,
		stderr:          stderr,
	}

	return interpreter, nil
}

// Start starts the subprocess and begins streaming output.
func (s *ShellInterpreter) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	childCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	if err := s.cmd.Start(); err != nil {
		return fmt.Errorf("start shell: %w", err)
	}

	go s.streamOutput(childCtx)
	return nil
}

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

func (s *ShellInterpreter) GetOutputWriter() repl.OutputWriter {
	return s.BaseInterpreter.GetOutputWriter()
}

// SendInput sends a command to the subprocess.
func (s *ShellInterpreter) SendInput(input string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stdin == nil {
		return fmt.Errorf("stdin not available")
	}

	if _, err := s.stdin.Write([]byte(input)); err != nil {
		return fmt.Errorf("write to stdin: %w", err)
	}

	if len(input) == 0 || input[len(input)-1] != '\n' {
		if _, err := s.stdin.Write([]byte("\n")); err != nil {
			return fmt.Errorf("write newline: %w", err)
		}
	}

	return nil
}

// Stop terminates the subprocess.
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
	if err := s.SendInput(input); err != nil {
		return err
	}
	return nil
}

// SetOutputWriter implements AsyncInterpreter.SetOutputWriter.
func (s *ShellInterpreter) SetOutputWriter(writer repl.OutputWriter) {
	s.BaseInterpreter.SetOutputWriter(writer)
}
