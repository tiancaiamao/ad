package repl

import (
	"context"
	"testing"
)

func TestBaseInterpreter(t *testing.T) {
	tests := []struct {
		name      string
		streaming bool
	}{
		{"sync interpreter", false},
		{"async interpreter", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bi := NewBaseInterpreter(tt.streaming)

			if bi.IsStreaming() != tt.streaming {
				t.Errorf("IsStreaming() = %v, want %v", bi.IsStreaming(), tt.streaming)
			}

			if bi.GetOutputWriter() != nil {
				t.Errorf("GetOutputWriter() should be nil initially")
			}

			// Test that Start/Stop don't crash
			ctx := context.Background()
			if err := bi.Start(ctx); err != nil {
				t.Errorf("Start() error = %v", err)
			}

			if err := bi.Stop(); err != nil {
				t.Errorf("Stop() error = %v", err)
			}
		})
	}
}

func TestSimpleSyncInterpreter(t *testing.T) {
	callCount := 0
	runFunc := func(ctx context.Context, input string) (string, error) {
		callCount++
		return "output: " + input, nil
	}

	si := NewSimpleSyncInterpreter(runFunc)

	ctx := context.Background()

	// Process should call RunFunc
	if err := si.Process(ctx, "test"); err != nil {
		t.Errorf("Process() error = %v", err)
	}

	if callCount != 1 {
		t.Errorf("RunFunc called %d times, want 1", callCount)
	}
}

func TestStreamingWriter(t *testing.T) {
	mockWriter := &mockOutputWriter{}
	sw := NewStreamingWriter(mockWriter, 10) // Small buffer

	// Write small amount (should stay buffered)
	if err := sw.Write("hello"); err != nil {
		t.Errorf("Write() error = %v", err)
	}

	if mockWriter.writeCount != 0 {
		t.Errorf("writeCount = %d, want 0 (buffered)", mockWriter.writeCount)
	}

	// Write enough to exceed buffer (hello + verylongstring > 10)
	// The buffer size is 10 bytes, and we write "hello" (5 bytes),
	// then "verylongstring" (14 bytes), total should exceed threshold
	if err := sw.Write("verylongstring"); err != nil {
		t.Errorf("Write() error = %v", err)
	}

	// Should have flushed at least once
	if mockWriter.writeCount == 0 {
		t.Errorf("writeCount = %d, want >0 (flushed)", mockWriter.writeCount)
	}

	// Explicit flush
	if err := sw.Flush(); err != nil {
		t.Errorf("Flush() error = %v", err)
	}

	// Should have written output
	if mockWriter.writeCount == 0 {
		t.Errorf("writeCount = %d, want >0", mockWriter.writeCount)
	}
}

// mockOutputWriter for testing
type mockOutputWriter struct {
	writeCount    int
	promptCount   int
	scrollCount   int
	flushCount    int
	lastOutput    string
}

func (m *mockOutputWriter) Write(output string) error {
	m.writeCount++
	m.lastOutput = output
	return nil
}

func (m *mockOutputWriter) WritePrompt() error {
	m.promptCount++
	return nil
}

func (m *mockOutputWriter) ScrollToBottom() error {
	m.scrollCount++
	return nil
}

func (m *mockOutputWriter) Flush() error {
	m.flushCount++
	return nil
}

func TestSyncInterpreterProcess(t *testing.T) {
	mockWriter := &mockOutputWriter{}
	runFunc := func(ctx context.Context, input string) (string, error) {
		return "result: " + input, nil
	}

	si := NewSimpleSyncInterpreter(runFunc)
	si.SetOutputWriter(mockWriter)

	ctx := context.Background()

	// Process input
	if err := si.Process(ctx, "test input"); err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	// Should have written output
	if mockWriter.writeCount == 0 {
		t.Error("writeCount = 0, want >0")
	}

	// Should have written prompt
	if mockWriter.promptCount == 0 {
		t.Error("promptCount = 0, want 1")
	}

	// Should have scrolled
	if mockWriter.scrollCount == 0 {
		t.Error("scrollCount = 0, want 1")
	}
}
