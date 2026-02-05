package main

import (
	"context"
	"fmt"
	"github.com/sminez/ad/win/pkg/repl"
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
