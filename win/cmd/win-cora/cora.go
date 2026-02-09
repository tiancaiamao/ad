package main

// NewCoraInterpreter creates a new cora interpreter.
func NewCoraInterpreter(cmdPath string, args []string) (*ShellInterpreter, error) {
	return NewShellInterpreter(cmdPath, args...)
}
