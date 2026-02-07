// Package repl provides a general-purpose REPL framework for ad
package repl

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/sminez/ad/win/pkg/ad"
)

// Config holds configuration for a REPL handler.
type Config struct {
	// Prompt is displayed to user.
	Prompt string

	// WindowName is the name of the ad window to create.
	WindowName string

	// WelcomeMessage is shown when the REPL starts.
	WelcomeMessage string

	// SendPrefix is the prefix used by send-to-win for external commands.
	// If empty, defaults to Prompt.
	SendPrefix string

	// InputPrefix is an interpreter prompt prefix to strip from user input.
	// Example: "#> " will strip "0 #> " from cora prompts.
	InputPrefix string

	// EchoSendInput controls whether send-to-win input stays in the buffer.
	// If false, the injected line will be removed before executing.
	EchoSendInput bool

	// EnableKeyboardExecute controls whether pressing Enter executes input.
	EnableKeyboardExecute bool

	// EnableExecute controls whether Execute (X) events trigger execution.
	EnableExecute bool

	// Debug enables debug logging.
	Debug bool

	// LogPath is where debug logs are written.
	LogPath string
}

// DefaultConfig returns a default configuration.
func DefaultConfig() Config {
	return Config{
		Prompt:     "> ",
		WindowName: "+repl",
		WelcomeMessage: `# REPL
# Type commands and press Enter to execute.
`,
		SendPrefix:            "",
		InputPrefix:           "",
		EchoSendInput:         true,
		EnableKeyboardExecute: true,
		EnableExecute:         true,
		Debug:                 false,
		LogPath:               "/tmp/repl.log",
	}
}

// Handler manages a REPL session with ad.
type Handler struct {
	config      Config
	client      *ad.Client
	bufferID    string
	interpreter Interpreter
	outputChan  chan string
	doneChan    chan struct{}
	mu          sync.Mutex
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewHandler creates a new REPL handler.
func NewHandler(config Config, client *ad.Client, interpreter Interpreter) (*Handler, error) {
	if config.Prompt == "" {
		// Allow empty prompt (useful when interpreter owns the prompt)
	}
	if config.WindowName == "" {
		config.WindowName = "+repl"
	}
	if config.SendPrefix == "" {
		config.SendPrefix = config.Prompt
	}
	if !config.EnableKeyboardExecute {
		// Explicitly allow disabling keyboard-triggered execution.
	}
	if !config.EnableExecute {
		// Explicitly allow disabling Execute (X) events.
	}

	ctx, cancel := context.WithCancel(context.Background())

	handler := &Handler{
		config:      config,
		client:      client,
		interpreter: interpreter,
		outputChan:  make(chan string, 100),
		doneChan:    make(chan struct{}),
		ctx:         ctx,
		cancel:      cancel,
	}

	handler.client.SetDebug(config.Debug)

	if config.Debug {
		if err := handler.setupLogging(); err != nil {
			cancel()
			return nil, fmt.Errorf("setup logging: %w", err)
		}
	}

	if err := handler.initBuffer(); err != nil {
		cancel()
		return nil, fmt.Errorf("init buffer: %w", err)
	}

	if interpreter.IsStreaming() {
		if async, ok := interpreter.(AsyncInterpreter); ok {
			async.SetOutputWriter(&adOutputWriter{handler: handler})
		}
	} else {
		// For sync interpreters, set the writer
		type outputWriterSetter interface {
			SetOutputWriter(OutputWriter)
		}
		if sync, ok := interpreter.(outputWriterSetter); ok {
			sync.SetOutputWriter(&adOutputWriter{handler: handler})
		}
	}

	return handler, nil
}

// setupLogging configures debug logging.
func (h *Handler) setupLogging() error {
	logPath := h.config.LogPath
	if logPath == "" {
		logPath = "/tmp/repl.log"
	}

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}

	log.SetOutput(f)
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	log.Println("=== REPL Starting ===")

	return nil
}

// initBuffer creates and initializes REPL buffer.
func (h *Handler) initBuffer() error {
	bufferID, err := h.client.OpenInNewWindow(h.config.WindowName)
	if err != nil {
		return fmt.Errorf("open in new window: %w", err)
	}
	h.bufferID = bufferID

	if h.config.Debug {
		log.Printf("Created window %s with buffer ID: %s\n", h.config.WindowName, bufferID)
	}

	content := h.config.WelcomeMessage + h.config.Prompt
	if err := h.client.WriteBody(h.bufferID, content); err != nil {
		return fmt.Errorf("write body: %w", err)
	}

	if err := h.client.WriteAddr(h.bufferID, "$"); err != nil {
		return fmt.Errorf("write addr: %w", err)
	}

	return nil
}

// Start begins the REPL session.
func (h *Handler) Start() error {
	if h.config.Debug {
		log.Println("Starting interpreter...")
	}

	if err := h.interpreter.Start(h.ctx); err != nil {
		return fmt.Errorf("start interpreter: %w", err)
	}

	if h.config.Debug {
		log.Println("Interpreter started successfully")
		log.Println("Ready to process events")
	}

	return nil
}

// Stop stops the REPL session.
func (h *Handler) Stop() error {
	if h.config.Debug {
		log.Println("Stopping REPL...")
	}

	h.cancel()

	if err := h.interpreter.Stop(); err != nil {
		if h.config.Debug {
			log.Printf("Error stopping interpreter: %v", err)
		}
	}

	close(h.outputChan)

	if h.config.Debug {
		log.Println("REPL stopped")
	}

	return nil
}

// BufferID returns the buffer ID for this REPL.
func (h *Handler) BufferID() string {
	return h.bufferID
}

// WriteOutput writes output to the buffer (thread-safe).
func (h *Handler) WriteOutput(output string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if output == "" {
		return nil
	}

	if err := h.client.AppendToBodyWithSource(h.bufferID, output, ad.SourceFsys); err != nil {
		return fmt.Errorf("append to body: %w", err)
	}

	if h.config.Debug {
		log.Printf("[OUTPUT] Wrote %d bytes", len(output))
	}

	return nil
}

// WritePrompt writes a new prompt with preceding newline.
func (h *Handler) WritePrompt() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.config.Prompt == "" {
		return nil
	}

	// Add newline before prompt
	if err := h.client.AppendToBodyWithSource(h.bufferID, "\n", ad.SourceFsys); err != nil {
		return fmt.Errorf("write newline before prompt: %w", err)
	}

	// Write prompt
	if err := h.client.AppendToBodyWithSource(h.bufferID, h.config.Prompt, ad.SourceFsys); err != nil {
		return fmt.Errorf("write prompt: %w", err)
	}

	if h.config.Debug {
		log.Println("[OUTPUT] Wrote prompt with preceding newline")
	}

	return nil
}

// ScrollToBottom scrolls the viewport to show latest output.
func (h *Handler) ScrollToBottom() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if err := h.client.ScrollToBottom(h.bufferID); err != nil {
		return fmt.Errorf("scroll to bottom: %w", err)
	}

	if h.config.Debug {
		log.Println("[OUTPUT] Scrolled to bottom")
	}

	return nil
}

// Flush ensures all output is written.
func (h *Handler) Flush() error {
	return nil
}

// ExecuteCommand executes a command in interpreter.
func (h *Handler) ExecuteCommand(input string, centerViewport bool) error {
	if h.config.Debug {
		log.Printf("[COMMAND] START input=%q centerViewport=%v", input, centerViewport)
	}

	if h.config.Debug {
		log.Printf("[COMMAND] Calling interpreter.Process...")
	}
	if err := h.interpreter.Process(h.ctx, input); err != nil {
		if h.config.Debug {
			log.Printf("[COMMAND] interpreter.Process ERROR: %v", err)
		}
		return fmt.Errorf("process input: %w", err)
	}
	if h.config.Debug {
		log.Printf("[COMMAND] interpreter.Process completed successfully")
	}

	if centerViewport {
		if h.config.Debug {
			log.Printf("[COMMAND] Centering viewport for buffer %s", h.bufferID)
		}
		if err := h.client.CenterViewport(h.bufferID); err != nil {
			if h.config.Debug {
				log.Printf("[COMMAND] CenterViewport error: %v", err)
			}
			return fmt.Errorf("center viewport: %w", err)
		}
		if h.config.Debug {
			log.Printf("[COMMAND] CenterViewport completed")
		}
	} else {
		if h.config.Debug {
			log.Printf("[COMMAND] Focusing buffer %s", h.bufferID)
		}
		if err := h.client.FocusBuffer(h.bufferID); err != nil {
			if h.config.Debug {
				log.Printf("[COMMAND] FocusBuffer error: %v", err)
			}
			return fmt.Errorf("focus buffer: %w", err)
		}
		if h.config.Debug {
			log.Printf("[COMMAND] FocusBuffer completed")
		}
	}

	if h.config.Debug {
		log.Printf("[COMMAND] END completed successfully")
	}

	return nil
}

// ExtractLastLine extracts the last line before a prompt.
func (h *Handler) ExtractLastLine(body string) string {
	lastNewline := strings.LastIndex(body, "\n")
	if lastNewline == -1 {
		return strings.TrimSpace(strings.TrimPrefix(body, h.config.Prompt))
	}

	prevNewline := strings.LastIndex(body[:lastNewline], "\n")
	var lastLine string
	if prevNewline == -1 {
		lastLine = body[:lastNewline]
	} else {
		lastLine = body[prevNewline+1 : lastNewline]
	}

	return strings.TrimSpace(strings.TrimPrefix(lastLine, h.config.Prompt))
}

func (h *Handler) deleteLastSendToWinInsert() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// send-to-win writes a line plus a blank line, so remove the last two lines.
	if err := h.client.WriteXAddr(h.bufferID, "$-1,$"); err != nil {
		return fmt.Errorf("write xaddr: %w", err)
	}
	if err := h.client.WriteXDot(h.bufferID, ""); err != nil {
		return fmt.Errorf("write xdot: %w", err)
	}
	if err := h.client.WriteAddr(h.bufferID, "$"); err != nil {
		return fmt.Errorf("write addr: %w", err)
	}
	return nil
}

func (h *Handler) stripInputPrefix(input string) string {
	if h.config.InputPrefix == "" {
		return input
	}
	if idx := strings.LastIndex(input, h.config.InputPrefix); idx != -1 {
		return strings.TrimSpace(input[idx+len(h.config.InputPrefix):])
	}
	return input
}

// adOutputWriter implements OutputWriter for writing to an ad buffer.
type adOutputWriter struct {
	handler *Handler
}

// Write implements OutputWriter.Write.
func (w *adOutputWriter) Write(output string) error {
	return w.handler.WriteOutput(output)
}

// WritePrompt implements OutputWriter.WritePrompt.
func (w *adOutputWriter) WritePrompt() error {
	return w.handler.WritePrompt()
}

// ScrollToBottom implements OutputWriter.ScrollToBottom.
func (w *adOutputWriter) ScrollToBottom() error {
	return w.handler.ScrollToBottom()
}

// Flush implements OutputWriter.Flush.
func (w *adOutputWriter) Flush() error {
	return w.handler.Flush()
}

// NewEventHandler creates an EventHandler implementation for use with ad's event loop.
func (h *Handler) NewEventHandler() ad.EventHandler {
	return &replEventHandler{handler: h}
}

// replEventHandler implements the ad.EventHandler interface.
type replEventHandler struct {
	handler *Handler
}

// HandleInsert implements ad.EventHandler.HandleInsert.
func (e *replEventHandler) HandleInsert(source ad.EventSource, from, to int, txt string, client *ad.Client) (ad.Outcome, error) {
	if e.handler.config.Debug {
		log.Printf("[HANDLE-INSERT] START source=%v from=%d to=%d txt=%q", source, from, to, txt)
	}

	if err := e.handler.client.MarkClean(e.handler.bufferID); err != nil {
		if e.handler.config.Debug {
			log.Printf("[HANDLE-INSERT] MarkClean error: %v", err)
		}
		return ad.Handled, fmt.Errorf("mark-clean: %w", err)
	}

	if source == ad.SourceFsys {
		if e.handler.config.SendPrefix != "" && strings.HasPrefix(txt, e.handler.config.SendPrefix) {
			input := strings.TrimSpace(strings.TrimPrefix(txt, e.handler.config.SendPrefix))
			input = strings.TrimRight(input, "\n")
			if input != "" {
				if e.handler.config.Debug {
					log.Printf("[HANDLE-INSERT] External command from send-to-win: %q", input)
				}
				if ctrl, ok := e.handler.interpreter.(ControlInterpreter); ok {
					if e.handler.config.Debug {
						log.Printf("[HANDLE-INSERT] Calling HandleControl...")
					}
					handled, err := ctrl.HandleControl(input)
					if e.handler.config.Debug {
						log.Printf("[HANDLE-INSERT] HandleControl returned: handled=%v err=%v", handled, err)
					}
					if err != nil {
						return ad.Handled, err
					}
					if handled {
						if err := e.handler.deleteLastSendToWinInsert(); err != nil && e.handler.config.Debug {
							log.Printf("[HANDLE-INSERT] delete send-to-win text failed: %v", err)
						}
						// Scroll to bottom to show the control command output
						if err := e.handler.ScrollToBottom(); err != nil && e.handler.config.Debug {
							log.Printf("[HANDLE-INSERT] ScrollToBottom after control command failed: %v", err)
						}
						if e.handler.config.Debug {
							log.Printf("[HANDLE-INSERT] END (control handled)")
						}
						return ad.Handled, nil
					}
				}
				if !e.handler.config.EchoSendInput {
					if err := e.handler.deleteLastSendToWinInsert(); err != nil && e.handler.config.Debug {
						log.Printf("[HANDLE-INSERT] delete send-to-win text failed: %v", err)
					}
				}
				if e.handler.config.Debug {
					log.Printf("[HANDLE-INSERT] Calling ExecuteCommand...")
				}
				err := e.handler.ExecuteCommand(input, true)
				if e.handler.config.Debug {
					log.Printf("[HANDLE-INSERT] ExecuteCommand returned: err=%v", err)
				}
				return ad.Handled, err
			}
		}

		if err := e.handler.client.WriteAddr(e.handler.bufferID, "$"); err != nil {
			if e.handler.config.Debug {
				log.Printf("[HANDLE-INSERT] WriteAddr error: %v", err)
			}
			return ad.Handled, err
		}
		if e.handler.config.Debug {
			log.Printf("[HANDLE-INSERT] END (Fsys insert, no command)")
		}
		return ad.Handled, nil
	}

	if source == ad.SourceKeyboard && txt == "\n" {
		if !e.handler.config.EnableKeyboardExecute {
			if e.handler.config.Debug {
				log.Printf("[HANDLE-INSERT] END (keyboard execution disabled)")
			}
			return ad.Handled, nil
		}
		if e.handler.config.Debug {
			log.Println("[HANDLE-INSERT] User pressed Enter")
		}

		body, err := e.handler.client.ReadBody(e.handler.bufferID)
		if err != nil {
			if e.handler.config.Debug {
				log.Printf("[HANDLE-INSERT] ReadBody error: %v", err)
			}
			return ad.Handled, fmt.Errorf("read-body: %w", err)
		}

		if len(body) > 500 {
			body = body[len(body)-500:]
		}

		if e.handler.config.Debug {
			log.Printf("[HANDLE-INSERT] body tail: %q", body)
		}

		input := e.handler.ExtractLastLine(body)
		input = e.handler.stripInputPrefix(input)
		if e.handler.config.Debug {
			log.Printf("[HANDLE-INSERT] Extracted input: %q", input)
		}

		if input != "" {
			if e.handler.config.Debug {
				log.Printf("[HANDLE-INSERT] Calling ExecuteCommand...")
			}
			err := e.handler.ExecuteCommand(input, false)
			if e.handler.config.Debug {
				log.Printf("[HANDLE-INSERT] ExecuteCommand returned: err=%v", err)
			}
			return ad.Handled, err
		}

		if err := e.handler.client.WriteAddr(e.handler.bufferID, "$"); err != nil {
			return ad.Handled, err
		}
		if e.handler.config.Debug {
			log.Printf("[HANDLE-INSERT] END (empty input)")
		}
		return ad.Handled, nil
	}

	if e.handler.config.Debug {
		log.Printf("[HANDLE-INSERT] END (passthrough for source=%v)", source)
	}
	return ad.Handled, nil
}

// HandleDelete implements ad.EventHandler.HandleDelete.
func (e *replEventHandler) HandleDelete(source ad.EventSource, from, to int, client *ad.Client) (ad.Outcome, error) {
	if e.handler.config.Debug {
		log.Printf("[HANDLE-DELETE] source=%v from=%d to=%d", source, from, to)
	}

	if err := e.handler.client.MarkClean(e.handler.bufferID); err != nil {
		return ad.Handled, err
	}

	return ad.Handled, nil
}

// HandleExecute implements ad.EventHandler.HandleExecute.
func (e *replEventHandler) HandleExecute(source ad.EventSource, from, to int, txt string, client *ad.Client) (ad.Outcome, error) {
	if !e.handler.config.EnableExecute {
		return ad.Handled, nil
	}
	input := strings.TrimSpace(txt)

	if e.handler.config.Debug {
		log.Printf("[HANDLE-EXECUTE] source=%v from=%d to=%d txt=%q input=%q", source, from, to, txt, input)
	}

	if err := e.handler.client.AppendToBody(e.handler.bufferID, "\n"); err != nil {
		if e.handler.config.Debug {
			log.Printf("[HANDLE-EXECUTE] AppendToBody error: %v", err)
		}
		return ad.Handled, fmt.Errorf("append-to-body: %w", err)
	}

	return ad.Handled, e.handler.ExecuteCommand(input, true)
}

// Run is a convenience method that starts the handler and runs the event loop.
func (h *Handler) Run() error {
	if h.config.Debug {
		log.Printf("[RUN] Starting REPL for buffer %s\n", h.bufferID)
	}

	if err := h.Start(); err != nil {
		return err
	}
	defer h.Stop()

	if h.config.Debug {
		log.Printf("[RUN] Starting event filter...\n")
	}

	eventHandler := h.NewEventHandler()
	if err := h.client.RunEventFilter(h.bufferID, eventHandler); err != nil {
		return fmt.Errorf("event filter: %w", err)
	}

	return nil
}
