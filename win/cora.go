package main

// NewCoraInterpreter creates a new echo interpreter.
func NewCoraInterpreter() (*ShellInterpreter, error) {
	return NewShellInterpreter("/Users/genius/project/cora/cora")
}
