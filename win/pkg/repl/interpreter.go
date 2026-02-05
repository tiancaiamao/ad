// Package repl provides a general-purpose REPL (Read-Eval-Print Loop) framework
// for integrating external interpreters with the ad text editor.
package repl

import (
	"context"
	"fmt"
	"io"
)

// Interpreter represents an external command interpreter that can process user input.
// Implementations can be synchronous (stateless, one-shot commands) or asynchronous
// (maintaining state like a persistent shell session).
type Interpreter interface {
	// Process processes user input and returns the output.
	// For synchronous interpreters, this runs the command and returns the result.
	// For asynchronous interpreters, this sends input to the running process and
	// output will be delivered via the OutputWriter.
	Process(ctx context.Context, input string) error

	// Start initializes the interpreter. For async interpreters, this starts the
	// subprocess and begins streaming output. For sync interpreters, this is a no-op.
	Start(ctx context.Context) error

	// Stop gracefully shuts down the interpreter.
	// For async interpreters, this terminates the subprocess.
	Stop() error

	// IsStreaming returns true if the interpreter streams output asynchronously.
	// Streaming interpreters should use the OutputWriter to deliver output.
	IsStreaming() bool
}

// OutputWriter is used by streaming interpreters to write output incrementally.
type OutputWriter interface {
	// Write writes a chunk of output to the REPL buffer.
	Write(output string) error

	// WritePrompt writes a new prompt after output is complete.
	WritePrompt() error

	// ScrollToBottom scrolls the viewport to show the latest output.
	ScrollToBottom() error

	// Flush ensures all output is written to the buffer.
	Flush() error
}

// SyncInterpreter is a synchronous (stateless) interpreter that runs each command
// independently and returns complete output.
type SyncInterpreter interface {
	Interpreter
	// Run executes a single command and returns its complete output.
	Run(ctx context.Context, input string) (string, error)
}

// AsyncInterpreter is an asynchronous interpreter that maintains a persistent
// subprocess and streams output as it arrives.
type AsyncInterpreter interface {
	Interpreter
	// SetOutputWriter configures where streamed output should be sent.
	SetOutputWriter(writer OutputWriter)

	// SendInput sends input to the running interpreter process.
	SendInput(input string) error
}

// ControlInterpreter optionally handles control commands (e.g. toggles) locally
// without forwarding them to the external interpreter.
type ControlInterpreter interface {
	// HandleControl processes a control command. Returns true if handled.
	HandleControl(input string) (bool, error)
}

// BaseInterpreter provides common functionality for interpreter implementations.
type BaseInterpreter struct {
	streaming bool
	writer    OutputWriter
}

// NewBaseInterpreter creates a new base interpreter.
func NewBaseInterpreter(streaming bool) *BaseInterpreter {
	return &BaseInterpreter{
		streaming: streaming,
	}
}

// IsStreaming returns whether this is a streaming interpreter.
func (b *BaseInterpreter) IsStreaming() bool {
	return b.streaming
}

// SetOutputWriter sets the output writer for streaming output.
func (b *BaseInterpreter) SetOutputWriter(writer OutputWriter) {
// 	fmt.Fprintf(os.Stderr, "[BASE-INTERPRETER] SetOutputWriter(%p)\n", writer)
	b.writer = writer
}

// GetOutputWriter returns the current output writer.
func (b *BaseInterpreter) GetOutputWriter() OutputWriter {
	return b.writer
}

// Process handles input for both sync and async interpreters.
func (b *BaseInterpreter) Process(ctx context.Context, input string) error {
	if b.IsStreaming() {
		if b.writer == nil {
			return fmt.Errorf("output writer not set for streaming interpreter")
		}
		// For streaming interpreters, write the input to the buffer first
		if err := b.writer.Write(input); err != nil {
			return fmt.Errorf("write input: %w", err)
		}
		if err := b.writer.Write("\n"); err != nil {
			return fmt.Errorf("write newline: %w", err)
		}
	}
	return nil
}

// Start is a no-op for base interpreter (should be overridden).
func (b *BaseInterpreter) Start(ctx context.Context) error {
	return nil
}

// Stop is a no-op for base interpreter (should be overridden).
func (b *BaseInterpreter) Stop() error {
	return nil
}

// SimpleSyncInterpreter wraps a function that executes commands synchronously.
type SimpleSyncInterpreter struct {
	*BaseInterpreter
	runFunc func(ctx context.Context, input string) (string, error)
}

// NewSimpleSyncInterpreter creates a simple sync interpreter from a function.
func NewSimpleSyncInterpreter(runFunc func(ctx context.Context, input string) (string, error)) *SimpleSyncInterpreter {
	return &SimpleSyncInterpreter{
		BaseInterpreter: NewBaseInterpreter(false),
		runFunc:         runFunc,
	}
}

// Run executes the command and returns output.
func (s *SimpleSyncInterpreter) Run(ctx context.Context, input string) (string, error) {
	return s.runFunc(ctx, input)
}

// SetOutputWriter sets the output writer for SimpleSyncInterpreter.
func (s *SimpleSyncInterpreter) SetOutputWriter(writer OutputWriter) {
	s.BaseInterpreter.SetOutputWriter(writer)
}

// Process for sync interpreters runs the command and writes output via the writer.
// Note: Input is already in the buffer (user typed it or send-to-win sent it),
// so we don't write the input again.
func (s *SimpleSyncInterpreter) Process(ctx context.Context, input string) error {
	// Run the command
	output, err := s.Run(ctx, input)
	if err != nil {
		return fmt.Errorf("run command: %w", err)
	}

	// Write output via the writer set on BaseInterpreter
	writer := s.BaseInterpreter.GetOutputWriter()
	if writer != nil {
		if output != "" {
			if err := writer.Write(output); err != nil {
				return fmt.Errorf("write output: %w", err)
			}
		}
		if err := writer.WritePrompt(); err != nil {
			return fmt.Errorf("write prompt: %w", err)
		}
		if err := writer.ScrollToBottom(); err != nil {
			return fmt.Errorf("scroll to bottom: %w", err)
		}
	}

	return nil
}

// StreamingWriter wraps an OutputWriter with buffering for efficiency.
type StreamingWriter struct {
	writer         OutputWriter
	buffer         []byte
	bufferLen      int
	flushThreshold int
}

// NewStreamingWriter creates a new streaming writer with buffering.
func NewStreamingWriter(writer OutputWriter, bufferSize int) *StreamingWriter {
	return &StreamingWriter{
		writer:         writer,
		buffer:         make([]byte, 0, bufferSize),
		bufferLen:      0,
		flushThreshold: bufferSize, // Use the capacity as threshold
	}
}

// Write adds data to the buffer. If buffer exceeds threshold, flush it.
func (w *StreamingWriter) Write(data string) error {
	w.buffer = append(w.buffer, data...)
	w.bufferLen += len(data)

	// Flush if buffer is too large
	if w.bufferLen >= w.flushThreshold {
		return w.Flush()
	}
	return nil
}

// WritePrompt writes a new prompt.
func (w *StreamingWriter) WritePrompt() error {
	if err := w.Flush(); err != nil {
		return err
	}
	return w.writer.WritePrompt()
}

// ScrollToBottom flushes and scrolls.
func (w *StreamingWriter) ScrollToBottom() error {
	if err := w.Flush(); err != nil {
		return err
	}
	return w.writer.ScrollToBottom()
}

// Flush writes all buffered data.
func (w *StreamingWriter) Flush() error {
	if w.bufferLen == 0 {
		return nil
	}

	data := string(w.buffer)
	if err := w.writer.Write(data); err != nil {
		return err
	}

	w.buffer = w.buffer[:0]
	w.bufferLen = 0
	return nil
}

// CopyReader copies from a reader to the writer in chunks, flushing after each chunk.
// This is useful for streaming subprocess output.
func CopyReader(ctx context.Context, reader io.Reader, writer OutputWriter, chunkSize int) error {
	buf := make([]byte, chunkSize)
	streamingWriter := NewStreamingWriter(writer, 4096)

	for {
		select {
		case <-ctx.Done():
			// Context cancelled, flush and exit
			return streamingWriter.Flush()
		default:
			n, err := reader.Read(buf)
			if err != nil {
				if err == io.EOF {
					return streamingWriter.Flush()
				}
				return fmt.Errorf("read: %w", err)
			}
			if n > 0 {
				if err := streamingWriter.Write(string(buf[:n])); err != nil {
					return err
				}
				// Scroll after each chunk for real-time feedback
				if err := streamingWriter.ScrollToBottom(); err != nil {
					return err
				}
			}
		}
	}
}
