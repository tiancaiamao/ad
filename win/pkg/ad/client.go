package ad

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"9fans.net/go/plan9"
	"9fans.net/go/plan9/client"
)

type Client struct {
	conn *client.Conn
	fsys *client.Fsys
	ns   string
}

func NewClient() (*Client, error) {
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
		conn: conn,
		fsys: fsys,
		ns:   ns,
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

	return strings.TrimSpace(bufferID), nil
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
	eventPath := fmt.Sprintf("buffers/%s/event", bufferID)

	// Open the event file with Open() to get a Fid for streaming reads
	fid, err := c.fsys.Open(eventPath, plan9.OREAD)
	if err != nil {
		return fmt.Errorf("open %s: %w", eventPath, err)
	}
	defer fid.Close()

	// Read events line by line using blocking reads
	// The event file will block until new events are available
	buf := make([]byte, 8192)
	remainder := ""

	for {
		// Blocking read - will wait for data
		n, err := fid.Read(buf)

		if n > 0 {
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
			for _, line := range lines {
				if line == "" {
					continue
				}

				// DEBUG: 打印所有收到的事件
				log.Printf("[EVENT-RAW] %s", line)

				var evt FsysEvent
				if err := json.Unmarshal([]byte(line), &evt); err != nil {
					// Skip malformed events
					log.Printf("[EVENT-ERROR] Failed to parse: %v", err)
					continue
				}

				// DEBUG: 打印解析后的事件
				log.Printf("[EVENT] kind=%s source=%s txt=%q", evt.Kind, evt.Source, evt.Txt)

				// Dispatch to handler based on event kind
				var handlerErr error
				switch evt.Kind {
				case "I": // InsertBody
					handlerErr = handler.HandleInsert(evt.Source, evt.ChFrom, evt.ChTo, evt.Txt, c)
				case "D": // DeleteBody
					handlerErr = handler.HandleDelete(evt.Source, evt.ChFrom, evt.ChTo, c)
				case "X": // ExecuteBody
					log.Printf("[EVENT] Calling HandleExecute!")
					handlerErr = handler.HandleExecute(evt.Source, evt.ChFrom, evt.ChTo, evt.Txt, c)
				case "L": // LoadBody
					// Write event back to ad for internal processing
					if err := c.WriteEventBack(bufferID, &evt); err != nil {
						return err
					}
				}

				if handlerErr != nil {
					return handlerErr
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

// EventHandler interface for handling buffer events
type EventHandler interface {
	HandleInsert(source string, from, to int, txt string, client *Client) error
	HandleDelete(source string, from, to int, client *Client) error
	HandleExecute(source string, from, to int, txt string, client *Client) error
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
