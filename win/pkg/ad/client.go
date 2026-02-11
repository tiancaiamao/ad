package ad

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"9fans.net/go/plan9"
	"9fans.net/go/plan9/client"
)

type Client struct {
	conn  *client.Conn
	fsys  *client.Fsys
	ns    string
	debug bool
	log   *slog.Logger
}

func NewClient() (*Client, error) {
	return NewClientWithDebug(false)
}

func NewClientWithDebug(debug bool) (*Client, error) {
	socketPath := findAdSocket()
	if socketPath == "" {
		return nil, fmt.Errorf("unable to find ad socket")
	}

	// Dial directly to the Unix socket
	conn, err := client.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", socketPath, err)
	}

	// Attach to the filesystem (empty user/aname for anonymous access)
	fsys, err := conn.Attach(nil, "", "")
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("attach: %w", err)
	}

	ns := "ad"
	if pid := os.Getenv("AD_PID"); pid != "" {
		ns = fmt.Sprintf("ad-%s", pid)
	}

	return &Client{
		conn:  conn,
		fsys:  fsys,
		ns:    ns,
		debug: debug,
		log:   slog.Default(), // Use default slog logger
	}, nil
}

func (c *Client) Close() error {
	if c.fsys != nil {
		c.fsys.Close()
	}
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// SetDebug sets the debug flag for logging.
func (c *Client) SetDebug(debug bool) {
	c.debug = debug
}

// SetLogger sets the logger for the client.
func (c *Client) SetLogger(logger *slog.Logger) {
	c.log = logger
}

// debugLog logs a message if debug is enabled.
func (c *Client) debugLog(msg string, args ...any) {
	if c.debug && c.log != nil {
		c.log.Debug(msg, args...)
	}
}

func (c *Client) ReadFile(path string) (string, error) {
	fid, err := c.fsys.Open(path, plan9.OREAD)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer fid.Close()

	data, err := io.ReadAll(fid)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}

	return string(data), nil
}

func (c *Client) WriteFile(path string, data []byte) (int, error) {
	fid, err := c.fsys.Open(path, plan9.OWRITE)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", path, err)
	}
	defer fid.Close()

	n, err := fid.Write(data)
	if err != nil {
		return n, fmt.Errorf("write %s: %w", path, err)
	}

	return n, nil
}

// OpenInNewWindow opens a file in a new window and returns the buffer ID
func (c *Client) OpenInNewWindow(path string) (string, error) {
	cmd := fmt.Sprintf("open-in-new-window %s", path)
	_, err := c.WriteFile("ctl", []byte(cmd))
	if err != nil {
		return "", fmt.Errorf("open-in-new-window: %w", err)
	}

	// Get the current buffer ID
	bufferID, err := c.ReadFile("buffers/current")
	if err != nil {
		return "", fmt.Errorf("get current buffer: %w", err)
	}

	trimmed := strings.TrimSpace(bufferID)
	c.debugLog("[OPEN-NEW-WINDOW] Raw buffer ID", "id", bufferID)
	c.debugLog("[OPEN-NEW-WINDOW] Trimmed", "id", trimmed, "length", len(trimmed))
	c.debugLog("[OPEN-NEW-WINDOW] Event file path will be", "path", fmt.Sprintf("buffers/%s/event", trimmed))

	return trimmed, nil
}

// WriteBody appends content to a buffer's body
func (c *Client) WriteBody(bufferID, content string) error {
	path := fmt.Sprintf("buffers/%s/body", bufferID)
	_, err := c.WriteFile(path, []byte(content))
	return err
}

// ReadBody reads a buffer's body
func (c *Client) ReadBody(bufferID string) (string, error) {
	path := fmt.Sprintf("buffers/%s/body", bufferID)
	return c.ReadFile(path)
}

// AppendToBody appends content to a buffer's body
func (c *Client) AppendToBody(bufferID, content string) error {
	return c.WriteBody(bufferID, content)
}

// AppendToBodyWithSource appends content to a buffer's body and marks it clean
// This is useful for output from the interpreter to avoid triggering event loops
func (c *Client) AppendToBodyWithSource(bufferID, content string, source EventSource) error {
	if err := c.WriteBody(bufferID, content); err != nil {
		return err
	}
	// Mark clean to indicate this was our own write
	return c.MarkClean(bufferID)
}

// WriteAddr sets the addr of a buffer
func (c *Client) WriteAddr(bufferID, addr string) error {
	path := fmt.Sprintf("buffers/%s/addr", bufferID)
	_, err := c.WriteFile(path, []byte(addr))
	return err
}

// ReadAddr reads the addr of a buffer
func (c *Client) ReadAddr(bufferID string) (string, error) {
	path := fmt.Sprintf("buffers/%s/addr", bufferID)
	content, err := c.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(content), nil
}

// WriteXAddr sets the xaddr of a buffer
func (c *Client) WriteXAddr(bufferID, addr string) error {
	path := fmt.Sprintf("buffers/%s/xaddr", bufferID)
	_, err := c.WriteFile(path, []byte(addr))
	return err
}

// ReadXAddr reads the xaddr of a buffer
func (c *Client) ReadXAddr(bufferID string) (string, error) {
	path := fmt.Sprintf("buffers/%s/xaddr", bufferID)
	content, err := c.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(content), nil
}

// ReadXDot reads the xdot of a buffer
func (c *Client) ReadXDot(bufferID string) (string, error) {
	path := fmt.Sprintf("buffers/%s/xdot", bufferID)
	return c.ReadFile(path)
}

// WriteXDot replaces the xdot contents of a buffer.
func (c *Client) WriteXDot(bufferID, content string) error {
	path := fmt.Sprintf("buffers/%s/xdot", bufferID)
	_, err := c.WriteFile(path, []byte(content))
	return err
}

// MarkClean marks a buffer as clean
func (c *Client) MarkClean(bufferID string) error {
	_, err := c.WriteFile("ctl", []byte("mark-clean"))
	return err
}

// FocusBuffer focuses a buffer by writing to current
func (c *Client) FocusBuffer(bufferID string) error {
	_, err := c.WriteFile("buffers/current", []byte(bufferID))
	return err
}

// GetCurrentBuffer returns the currently focused buffer ID
func (c *Client) GetCurrentBuffer() (string, error) {
	bufferID, err := c.ReadFile("buffers/current")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(bufferID), nil
}

// SetViewport sends a viewport command to the editor
func (c *Client) SetViewport(command string) error {
	_, err := c.WriteFile("ctl", []byte(command))
	return err
}

// CenterViewport centers the viewport for a buffer and restores focus
func (c *Client) CenterViewport(bufferID string) error {
	// Save the current buffer
	currentBuffer, err := c.GetCurrentBuffer()
	if err != nil {
		return fmt.Errorf("get current buffer: %w", err)
	}

	// Focus the target buffer
	if err := c.FocusBuffer(bufferID); err != nil {
		return fmt.Errorf("focus buffer %s: %w", bufferID, err)
	}

	// Set addr to end of buffer
	if err := c.WriteAddr(bufferID, "$"); err != nil {
		return fmt.Errorf("write addr: %w", err)
	}

	// Scroll viewport to show the last line at bottom
	if err := c.SetViewport("viewport-bottom"); err != nil {
		return fmt.Errorf("viewport-bottom: %w", err)
	}

	// Restore focus to original buffer
	if currentBuffer != "" && currentBuffer != bufferID {
		if err := c.FocusBuffer(currentBuffer); err != nil {
			return fmt.Errorf("restore focus to %s: %w", currentBuffer, err)
		}
	}

	return nil
}

// ScrollToBottom scrolls a buffer to show the last line
// Unlike CenterViewport, this doesn't save/restore focus
func (c *Client) ScrollToBottom(bufferID string) error {
	// Save the current buffer
	currentBuffer, err := c.GetCurrentBuffer()
	if err != nil {
		return fmt.Errorf("get current buffer: %w", err)
	}

	// Focus the target buffer temporarily
	if currentBuffer != bufferID {
		if err := c.FocusBuffer(bufferID); err != nil {
			return fmt.Errorf("focus buffer %s: %w", bufferID, err)
		}
	}

	// Set addr to end of buffer
	if err := c.WriteAddr(bufferID, "$"); err != nil {
		// Try to restore focus before returning error
		if currentBuffer != bufferID {
			_ = c.FocusBuffer(currentBuffer)
		}
		return fmt.Errorf("write addr: %w", err)
	}

	// Scroll viewport to show the last line at bottom
	if err := c.SetViewport("viewport-bottom"); err != nil {
		// Try to restore focus before returning error
		if currentBuffer != bufferID {
			_ = c.FocusBuffer(currentBuffer)
		}
		return fmt.Errorf("viewport-bottom: %w", err)
	}

	// Restore focus to original buffer
	if currentBuffer != "" && currentBuffer != bufferID {
		if err := c.FocusBuffer(currentBuffer); err != nil {
			return fmt.Errorf("restore focus to %s: %w", currentBuffer, err)
		}
	}

	return nil
}

// FocusAndScroll focuses the buffer and scrolls to bottom
// Use this when you want to show the buffer to the user
func (c *Client) FocusAndScroll(bufferID string) error {
	if err := c.FocusBuffer(bufferID); err != nil {
		return fmt.Errorf("focus buffer %s: %w", bufferID, err)
	}
	return c.ScrollToBottom(bufferID)
}

// BodyWriter returns an io.Writer for writing to a buffer's body
func (c *Client) BodyWriter(bufferID string) *BodyWriter {
	return &BodyWriter{
		client:   c,
		bufferID: bufferID,
	}
}

// RunEventFilter runs an event filter on a buffer
// This continuously reads events from the buffer's event file and dispatches them to the handler
func (c *Client) RunEventFilter(bufferID string, handler EventHandler) error {
	c.debugLog("[RUN-EVENT-FILTER] Starting event filter")
	c.debugLog("[RUN-EVENT-FILTER] Buffer ID", "id", bufferID)

	eventPath := fmt.Sprintf("buffers/%s/event", bufferID)
	c.debugLog("[RUN-EVENT-FILTER] Event path", "path", eventPath)

	// Open the event file with Open() to get a Fid for streaming reads
	// This triggers ad to attach an input filter to the buffer
	c.debugLog("[RUN-EVENT-FILTER] Opening event file (OREAD mode)...")
	fid, err := c.fsys.Open(eventPath, plan9.OREAD)
	if err != nil {
		c.debugLog("[RUN-EVENT-FILTER] Open FAILED", "error", err)
		return fmt.Errorf("open %s: %w", eventPath, err)
	}
	defer fid.Close()

	c.debugLog("[RUN-EVENT-FILTER] Event file opened successfully")
	c.debugLog("[RUN-EVENT-FILTER] Entering event loop (blocking reads)...")

	// Read events line by line using blocking reads
	// The event file will block until new events are available
	buf := make([]byte, 8192)
	remainder := ""
	readCount := 0

	for {
		// Blocking read - will wait for data
		n, err := fid.Read(buf)

		if n > 0 {
			c.debugLog("[RUN-EVENT-FILTER] Read bytes from event file", "count", n)
			readCount++
			c.debugLog("[RUN-EVENT-FILTER] Total reads so far", "count", readCount)

			// Safety check: n should never exceed buffer size
			if n > len(buf) {
				return fmt.Errorf("read returned invalid size: %d > %d", n, len(buf))
			}

			// Append to remainder and split by newlines
			data := remainder + string(buf[:n])
			lines := strings.Split(data, "\n")

			// Keep the last incomplete line
			if len(lines) > 0 && !strings.HasSuffix(data, "\n") {
				remainder = lines[len(lines)-1]
				lines = lines[:len(lines)-1]
			} else {
				remainder = ""
			}

			// Process complete lines
			lineCount := 0
			for _, line := range lines {
				if line == "" {
					continue
				}

				lineCount++
				c.debugLog("[EVENT-RAW] Line", "num", lineCount, "content", line)

				var evt FsysEvent
				if err := json.Unmarshal([]byte(line), &evt); err != nil {
					// Skip malformed events
					c.debugLog("[EVENT-ERROR] Failed to parse", "error", err)
					continue
				}

				// DEBUG: 打印解析后的事件
				c.debugLog("[EVENT] kind source txt", "kind", evt.Kind, "source", evt.Source, "txt", evt.Txt)

				// Convert string source to EventSource
				var src EventSource
				switch evt.Source {
				case "K":
					src = SourceKeyboard
				case "M":
					src = SourceMouse
				case "F":
					src = SourceFsys
				default:
					c.debugLog("[EVENT] Unknown source", "source", evt.Source)
					continue
				}

				// Dispatch to handler based on event kind
				var outcome Outcome
				var handlerErr error
				switch evt.Kind {
				case "I": // InsertBody
					outcome, handlerErr = handler.HandleInsert(src, evt.ChFrom, evt.ChTo, evt.Txt, c)
				case "D": // DeleteBody
					outcome, handlerErr = handler.HandleDelete(src, evt.ChFrom, evt.ChTo, c)
				case "X": // ExecuteBody
					c.debugLog("[EVENT] Calling HandleExecute!")
					outcome, handlerErr = handler.HandleExecute(src, evt.ChFrom, evt.ChTo, evt.Txt, c)
				case "L": // LoadBody
					outcome, handlerErr = Passthrough, nil // Default passthrough for Load
				default:
					// Unknown event types are passed through
					outcome = Passthrough
				}

				if handlerErr != nil {
					return handlerErr
				}

				// Handle event outcomes
				switch outcome {
				case Handled:
					// Event was fully handled, do nothing
				case Passthrough:
					// Write event back to ad for internal processing
					if evt.Kind == "L" || evt.Kind != "I" && evt.Kind != "D" && evt.Kind != "X" {
						if err := c.WriteEventBack(bufferID, &evt); err != nil {
							return err
						}
					}
				case PassthroughAndExit:
					// Write event back and exit
					if err := c.WriteEventBack(bufferID, &evt); err != nil {
						return err
					}
					return nil
				case Exit:
					// Exit without passing event back
					return nil
				}
			}
		}

		// Handle read errors
		if err != nil {
			if err == io.EOF {
				// Event file closed, exit gracefully
				return nil
			}
			return fmt.Errorf("read event: %w", err)
		}
	}
}

// MinibufferSelect opens the minibuffer with the given options and returns the selected line
// It writes the options to ad/minibuffer, optionally sets a prompt, and reads back the selection
func (c *Client) MinibufferSelect(prompt string, options []string) (string, error) {
	// Write options to the minibuffer
	lines := strings.Join(options, "\n")
	if lines != "" {
		lines += "\n"
	}
	if _, err := c.WriteFile("minibuffer", []byte(lines)); err != nil {
		return "", fmt.Errorf("write minibuffer: %w", err)
	}

	// Optionally set the prompt
	if prompt != "" {
		cmd := fmt.Sprintf("minibuffer-prompt %s", prompt)
		if _, err := c.WriteFile("ctl", []byte(cmd)); err != nil {
			return "", fmt.Errorf("set minibuffer prompt: %w", err)
		}
	}

	// Read the user's selection
	selection, err := c.ReadFile("minibuffer")
	if err != nil {
		return "", fmt.Errorf("read minibuffer: %w", err)
	}

	return strings.TrimSpace(selection), nil
}

// WriteEventBack writes an event back to the event file for ad to process
func (c *Client) WriteEventBack(bufferID string, evt *FsysEvent) error {
	data, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	path := fmt.Sprintf("buffers/%s/event", bufferID)
	_, err = c.WriteFile(path, data)
	return err
}

// FsysEvent represents an event from the ad filesystem
type FsysEvent struct {
	Source    string `json:"source"`
	Kind      string `json:"kind"`
	ChFrom    int    `json:"ch_from"`
	ChTo      int    `json:"ch_to"`
	Truncated bool   `json:"truncated"`
	Txt       string `json:"txt"`
}

// EventSource identifies the origin of an event
type EventSource string

const (
	SourceKeyboard EventSource = "K" // Keyboard input
	SourceMouse    EventSource = "M" // Mouse input
	SourceFsys     EventSource = "F" // Filesystem (our own writes)
)

// Outcome determines how an event should be handled
type Outcome int

const (
	// Handled means the event was fully processed and should not be passed back to ad
	Handled Outcome = iota
	// Passthrough means the event should be passed back to ad for internal processing
	Passthrough
	// PassthroughAndExit means pass the event back to ad and then exit the event filter
	PassthroughAndExit
	// Exit means stop the event filter without passing the event back
	Exit
)

// EventHandler interface for handling buffer events
type EventHandler interface {
	HandleInsert(source EventSource, from, to int, txt string, client *Client) (Outcome, error)
	HandleDelete(source EventSource, from, to int, client *Client) (Outcome, error)
	HandleExecute(source EventSource, from, to int, txt string, client *Client) (Outcome, error)
}

// BodyWriter implements io.Writer for writing to a buffer's body
type BodyWriter struct {
	client   *Client
	bufferID string
}

func (w *BodyWriter) Write(p []byte) (n int, err error) {
	err = w.client.WriteBody(w.bufferID, string(p))
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func findAdSocket() string {
	// Check for NAMESPACE environment variable first (standard Plan 9 approach)
	if namespace := os.Getenv("NAMESPACE"); namespace != "" {
		if pid := os.Getenv("AD_PID"); pid != "" {
			socketPath := fmt.Sprintf("%s/ad-%s", namespace, pid)
			if pathExists(socketPath) {
				return socketPath
			}
		}

		socketPath := fmt.Sprintf("%s/ad", namespace)
		if pathExists(socketPath) {
			return socketPath
		}
	}

	// Fall back to constructing the path from USER and DISPLAY
	user := os.Getenv("USER")
	if user == "" {
		user = "unknown"
	}

	display := os.Getenv("DISPLAY")
	if display == "" {
		display = ":0"
	}

	// On macOS/Plan 9, the socket directory is /tmp/ns.$USER.$DISPLAY
	// where DISPLAY is the full path (e.g., /private/tmp/.../org.xquartz:0)
	socketDir := fmt.Sprintf("/tmp/ns.%s.%s", user, display)

	if pid := os.Getenv("AD_PID"); pid != "" {
		socketPath := fmt.Sprintf("%s/ad-%s", socketDir, pid)
		if pathExists(socketPath) {
			return socketPath
		}
	}

	socketPath := fmt.Sprintf("%s/ad", socketDir)
	if pathExists(socketPath) {
		return socketPath
	}

	if pid := os.Getenv("AD_PID"); pid != "" {
		return fmt.Sprintf("%s/ad-%s", socketDir, pid)
	}

	return socketPath
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
