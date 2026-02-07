package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sminez/ad/win/pkg/repl"
)

type PiInterpreter struct {
	*repl.BaseInterpreter
	cmdPath string
	cmdArgs []string
	debug   bool

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
	cancel context.CancelFunc

	mu      sync.Mutex
	stateMu sync.Mutex

	isStreaming            bool
	showAssistant          bool
	showThinking           bool
	showTools              bool
	showPrefixes           bool
	currentMessageRole     string
	currentMessageStreamed bool

	toolStates map[string]*piToolState

	// Model management
	availableModels      []Model
	currentModelID       string
	currentModelProvider string
	currentThinkingLevel string
	autoCompactionEnabled bool

	// Monitoring
	lastPiActivity time.Time
	rpcSequence    int64
	workingDir     string
}

type piToolState struct {
	toolName      string
	lastOutput    string
	prefixWritten bool
}

type Model struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Provider      string   `json:"provider"`
	API           string   `json:"api"`
	Reasoning     bool     `json:"reasoning"`
	Input         []string `json:"input"`
	ContextWindow int      `json:"contextWindow"`
	MaxTokens     int      `json:"maxTokens"`
}

type piCommand struct {
	Type              string `json:"type"`
	Message           string `json:"message"`
	StreamingBehavior string `json:"streamingBehavior,omitempty"`
}

type rpcEnvelope struct {
	Type string `json:"type"`
}

type rpcResponse struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Success bool   `json:"success"`
	Error   string `json:"error"`
}

type rpcModelResponse struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Data    struct {
		Models []Model `json:"models"`
	} `json:"data"`
}

type rpcSetModelResponse struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Data    Model  `json:"data"`
}

type rpcGetStateResponse struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Data    struct {
		Model              *Model `json:"model"`
		ThinkingLevel      string `json:"thinkingLevel"`
		IsStreaming        bool   `json:"isStreaming"`
		IsCompacting       bool   `json:"isCompacting"`
		SteeringMode       string `json:"steeringMode"`
		FollowUpMode       string `json:"followUpMode"`
		SessionFile        string `json:"sessionFile"`
		SessionID          string `json:"sessionId"`
		SessionName        string `json:"sessionName"`
		AutoCompactionEnabled bool `json:"autoCompactionEnabled"`
		MessageCount       int    `json:"messageCount"`
		PendingMessageCount int   `json:"pendingMessageCount"`
	} `json:"data"`
}

type rpcNewSessionResponse struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Data    struct {
		Cancelled bool `json:"cancelled"`
	} `json:"data"`
}

type rpcSwitchSessionResponse struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Data    struct {
		Cancelled bool `json:"cancelled"`
	} `json:"data"`
}

type rpcGetLastAssistantTextResponse struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Data    struct {
		Text string `json:"text"`
	} `json:"data"`
}

type rpcCycleThinkingLevelResponse struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Data    struct {
		Level string `json:"level"`
	} `json:"data"`
}

type rpcSetThinkingLevelResponse struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Data    struct {
		Level string `json:"level"`
	} `json:"data"`
}

type rpcSlashCommand struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"` // "extension", "prompt", "skill"
	Location    string `json:"location,omitempty"` // "user", "project", "path"
	Path        string `json:"path,omitempty"`
}

type rpcGetCommandsResponse struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Data    struct {
		Commands []rpcSlashCommand `json:"commands"`
	} `json:"data"`
}

type rpcGetMessagesResponse struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Data    struct {
		Messages []json.RawMessage `json:"messages"`
	} `json:"data"`
}

type rpcGetSessionStatsResponse struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Data    struct {
		SessionFile        string `json:"sessionFile"`
		SessionID          string `json:"sessionId"`
		UserMessages       int    `json:"userMessages"`
		AssistantMessages  int    `json:"assistantMessages"`
		ToolCalls          int    `json:"toolCalls"`
		ToolResults        int    `json:"toolResults"`
		TotalMessages      int    `json:"totalMessages"`
		Tokens             struct {
			Input      int `json:"input"`
			Output     int `json:"output"`
			CacheRead  int `json:"cacheRead"`
			CacheWrite int `json:"cacheWrite"`
			Total      int `json:"total"`
		} `json:"tokens"`
		Cost float64 `json:"cost"`
	} `json:"data"`
}

type rpcSetAutoCompactionResponse struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Success bool   `json:"success"`
	Error   string `json:"error"`
}

type rpcMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type rpcMessageStart struct {
	Type    string     `json:"type"`
	Message rpcMessage `json:"message"`
}

type rpcMessageEnd struct {
	Type    string     `json:"type"`
	Message rpcMessage `json:"message"`
}

type rpcMessageUpdate struct {
	Type                  string         `json:"type"`
	Message               rpcMessage     `json:"message"`
	AssistantMessageEvent rpcAssistEvent `json:"assistantMessageEvent"`
}

type rpcAssistEvent struct {
	Type         string `json:"type"`
	ContentIndex int    `json:"contentIndex"`
	Delta        string `json:"delta"`
	Content      string `json:"content"`
}

type rpcToolResult struct {
	Content []rpcContentBlock `json:"content"`
}

type rpcContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Thinking string `json:"thinking"`
}

type rpcToolStart struct {
	Type       string                 `json:"type"`
	ToolCallId string                 `json:"toolCallId"`
	ToolName   string                 `json:"toolName"`
	Args       map[string]interface{} `json:"args"`
}

type rpcToolUpdate struct {
	Type          string         `json:"type"`
	ToolCallId    string         `json:"toolCallId"`
	ToolName      string         `json:"toolName"`
	PartialResult *rpcToolResult `json:"partialResult"`
}

type rpcToolEnd struct {
	Type       string         `json:"type"`
	ToolCallId string         `json:"toolCallId"`
	ToolName   string         `json:"toolName"`
	Result     *rpcToolResult `json:"result"`
	IsError    bool           `json:"isError"`
}

func NewPiInterpreter(cmdPath string, cmdArgs []string, debug bool) *PiInterpreter {
	return &PiInterpreter{
		BaseInterpreter:        repl.NewBaseInterpreter(true),
		cmdPath:                cmdPath,
		cmdArgs:                cmdArgs,
		debug:                  debug,
		showAssistant:          true,
		showThinking:           true,
		showTools:              false,
		showPrefixes:           true,
		toolStates:             make(map[string]*piToolState),
		currentMessageRole:     "",
		currentMessageStreamed: false,
		currentThinkingLevel:   "",
	}
}

func (p *PiInterpreter) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cmd != nil {
		return fmt.Errorf("pi already started")
	}

	childCtx, cancel := context.WithCancel(ctx)
	p.cancel = cancel

	args := append([]string{"--mode", "rpc"}, p.cmdArgs...)
	cmd := exec.Command(p.cmdPath, args...)
	cmd.Env = os.Environ()

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("create stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return fmt.Errorf("create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		return fmt.Errorf("create stderr pipe: %w", err)
	}

	p.cmd = cmd
	p.stdin = stdin
	p.stdout = stdout
	p.stderr = stderr

	if err := p.cmd.Start(); err != nil {
		return fmt.Errorf("start pi: %w", err)
	}

	// Record the working directory (pi uses the same dir as win)
	if wd, err := os.Getwd(); err == nil {
		p.stateMu.Lock()
		p.workingDir = wd
		p.stateMu.Unlock()
		if p.debug {
			log.Printf("[PI-START] Working directory: %s", wd)
		}
	}

	if p.debug {
		log.Printf("[PI-START] pi started with PID %d", p.cmd.Process.Pid)
	}

	go p.readStdout(childCtx)
	go p.readStderr(childCtx)

	// Start heartbeat goroutine
	if p.debug {
		go p.heartbeat(childCtx)
	}

	return nil
}

func (p *PiInterpreter) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cancel != nil {
		p.cancel()
	}

	if p.stdin != nil {
		p.stdin.Close()
		p.stdin = nil
	}
	if p.stdout != nil {
		p.stdout.Close()
		p.stdout = nil
	}
	if p.stderr != nil {
		p.stderr.Close()
		p.stderr = nil
	}

	if p.cmd != nil && p.cmd.Process != nil {
		if err := p.cmd.Process.Kill(); err != nil {
			return fmt.Errorf("kill pi: %w", err)
		}
	}

	p.cmd = nil
	return nil
}

func (p *PiInterpreter) SendInput(input string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stdin == nil {
		return fmt.Errorf("pi stdin not available")
	}

	cmd := piCommand{
		Type:    "prompt",
		Message: input,
	}
	if p.getStreaming() {
		cmd.StreamingBehavior = "steer"
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal prompt: %w", err)
	}
	data = append(data, '\n')

	p.rpcSequence++
	seq := p.rpcSequence
	if p.debug {
		log.Printf("[PI-RPC-SEND] seq=%d type=%s message_len=%d", seq, cmd.Type, len(cmd.Message))
	}

	if _, err := p.stdin.Write(data); err != nil {
		return fmt.Errorf("write to pi: %w", err)
	}
	return nil
}

func (p *PiInterpreter) Process(ctx context.Context, input string) error {
	if p.debug {
		log.Printf("[PROCESS] START input_len=%d", len(input))
	}
	err := p.SendInput(input)
	if p.debug {
		if err != nil {
			log.Printf("[PROCESS] END error=%v", err)
		} else {
			log.Printf("[PROCESS] END sent successfully")
		}
	}
	return err
}

func (p *PiInterpreter) SetOutputWriter(writer repl.OutputWriter) {
	p.BaseInterpreter.SetOutputWriter(writer)
}

func (p *PiInterpreter) HandleControl(input string) (bool, error) {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return false, nil
	}

	// Recognize both :win and / as command prefixes
	var cmd string
	var args []string

	// Only recognize :win as command prefix (not /)
	if fields[0] == ":win" {
		if len(fields) < 2 {
			p.showHelp()
			return true, nil
		}
		cmd = fields[1]
		args = fields[2:]
	} else {
		// Not a win command, let it pass through as message to pi
		return false, nil
	}

	// Handle help explicitly
	if cmd == "help" || cmd == "" {
		p.showHelp()
		return true, nil
	}

	switch cmd {
	case "show-settings":
		p.showSettings()
		return true, nil
	case "session":
		return true, p.getState()
	case "session-state": // backward compatibility
		return true, p.getState()
	case "messages":
		return true, p.getMessages()
	case "show-usage":
		return true, p.showUsage()
	case "commands":
		return true, p.getCommands()
	case "models": // deprecated, show warning
		p.writeControlNoScroll("win: /models is deprecated, use /model-select instead")
		return true, p.showAvailableModels()
	case "model":
		if len(args) == 0 {
			p.writeControlNoScroll("win: /model is deprecated, use /model-select instead")
			return true, nil
		}
		p.writeControlNoScroll("win: /model is deprecated, use /model-select instead")
		return true, p.setModelFromInput(args[0])
	case "model-select":
		// Trigger external script for interactive model selection
		return true, p.triggerModelSelect()
	case "new", "new-session": // both /new and :win new-session
		return true, p.newSession()
	case "abort":
		return true, p.abort()
	case "auto-compaction":
		if len(args) == 0 {
			p.writeControlNoScroll("Usage: /auto-compaction <on|off>")
			return true, nil
		}
		return true, p.setAutoCompaction(args[0])
	case "thinking-level":
		if len(args) == 0 {
			p.writeControlNoScroll("Usage: /thinking-level <off|minimal|low|medium|high|xhigh>")
			return true, nil
		}
		return true, p.setThinkingLevel(args[0])
	case "cycle-thinking-level":
		return true, p.cycleThinkingLevel()
	case "thinking", "tools", "prefix":
		if err := p.toggleSetting(cmd, args); err != nil {
			p.writeControlNoScroll(err.Error())
		}
		return true, nil
	case "compact":
		customInstructions := strings.Join(args, " ")
		return true, p.compact(customInstructions)
	case "resume":
		sessionPath := strings.Join(args, " ")
		return true, p.triggerSessionResume(sessionPath)
	case "copy":
		return true, p.copyLastAssistantText()
	case "quit":
		p.writeControlNoScroll("win: exiting...")
		return true, fmt.Errorf("quit requested")
	default:
		p.writeControlNoScroll(fmt.Sprintf("win: unknown command '%s' (use /help for usage)", cmd))
		return true, nil
	}
}

func (p *PiInterpreter) toggleSetting(name string, args []string) error {
	current := false
	switch name {
	case "thinking":
		current = p.getShowThinking()
	case "tools":
		current = p.getShowTools()
	case "prefix":
		current = p.getShowPrefixes()
	}

	if len(args) == 0 {
		return p.setSetting(name, !current)
	}

	switch args[0] {
	case "on":
		return p.setSetting(name, true)
	case "off":
		return p.setSetting(name, false)
	case "toggle":
		return p.setSetting(name, !current)
	default:
		return fmt.Errorf("win: invalid value (use on|off|toggle)")
	}
}

func (p *PiInterpreter) setSetting(name string, value bool) error {
	p.stateMu.Lock()
	switch name {
	case "thinking":
		p.showThinking = value
	case "tools":
		p.showTools = value
	case "prefix":
		p.showPrefixes = value
	default:
		p.stateMu.Unlock()
		return fmt.Errorf("win: unknown setting")
	}
	p.stateMu.Unlock()
	p.writeControlNoScroll(fmt.Sprintf("win: %s=%v", name, value))
	return nil
}

func (p *PiInterpreter) readStdout(ctx context.Context) {
	reader := bufio.NewReader(p.stdout)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			p.handleLine(strings.TrimSpace(string(line)))
		}
		if err != nil {
			if err == io.EOF {
				return
			}
			if p.debug {
				fmt.Fprintf(os.Stderr, "pi stdout read error: %v\n", err)
			}
			return
		}
	}
}

func (p *PiInterpreter) readStderr(ctx context.Context) {
	reader := bufio.NewReader(p.stderr)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line, err := reader.ReadBytes('\n')
		if len(line) > 0 && p.debug {
			log.Printf("[PI-STDERR] %s", strings.TrimSpace(string(line)))
		}
		if err != nil {
			if err == io.EOF {
				return
			}
			return
		}
	}
}

// heartbeat logs periodic status updates for debugging
func (p *PiInterpreter) heartbeat(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	log.Printf("[HEARTBEAT] Started - will log every 30 seconds")

	for {
		select {
		case <-ctx.Done():
			log.Printf("[HEARTBEAT] Stopped")
			return
		case <-ticker.C:
			p.stateMu.Lock()
			streaming := p.isStreaming
			lastActivity := p.lastPiActivity
			cmd := p.cmd
			piPID := int64(0)
			if cmd != nil && cmd.Process != nil {
				piPID = int64(cmd.Process.Pid)
			}
			p.stateMu.Unlock()

			// Check if pi is still running
			if piPID > 0 {
				proc, err := os.FindProcess(int(piPID))
				if err != nil {
					log.Printf("[HEARTBEAT] ERROR: Cannot find pi process: %v", err)
				} else {
					if err := proc.Signal(os.Signal(nil)); err != nil {
						log.Printf("[HEARTBEAT] WARNING: pi process PID %d appears dead: %v", piPID, err)
					}
				}
			}

			// Log status
			idleTime := time.Since(lastActivity)
			log.Printf("[HEARTBEAT] pi_pid=%d streaming=%v idle=%s", piPID, streaming, idleTime)
		}
	}
}

func (p *PiInterpreter) handleLine(line string) {
	if line == "" {
		return
	}

	p.stateMu.Lock()
	p.lastPiActivity = time.Now()
	p.stateMu.Unlock()

	var env rpcEnvelope
	if err := json.Unmarshal([]byte(line), &env); err != nil {
		if p.debug {
			log.Printf("[PI-RECV] Malformed JSON: %q", line)
		}
		return
	}

	if p.debug {
		log.Printf("[PI-RECV] type=%s", env.Type)
	}

	switch env.Type {
	case "response":
		p.handleResponse(line)
	case "agent_start":
		if p.debug {
			log.Printf("[PI-AGENT] Agent started (streaming)")
		}
		p.setStreaming(true)
	case "agent_end":
		if p.debug {
			log.Printf("[PI-AGENT] Agent ended (streaming)")
		}
		p.setStreaming(false)
	case "turn_start":
		if p.debug {
			log.Printf("[PI-TURN] Turn started (streaming)")
		}
		p.setStreaming(true)
	case "turn_end":
		if p.debug {
			log.Printf("[PI-TURN] Turn ended (streaming)")
		}
		p.setStreaming(false)
	case "message_start":
		p.handleMessageStart(line)
	case "message_update":
		p.handleMessageUpdate(line)
	case "message_end":
		p.handleMessageEnd(line)
	case "tool_execution_start":
		p.handleToolStart(line)
	case "tool_execution_update":
		p.handleToolUpdate(line)
	case "tool_execution_end":
		p.handleToolEnd(line)
	default:
		if p.debug {
			log.Printf("[PI-RECV] Unknown event type: %s", env.Type)
		}
	}
}

func (p *PiInterpreter) handleResponse(line string) {
	// Parse the basic response first
	var resp rpcResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		if p.debug {
			log.Printf("[PI-RESPONSE] ERROR failed to parse: %v", err)
		}
		return
	}

	if p.debug {
		log.Printf("[PI-RESPONSE] command=%s success=%v", resp.Command, resp.Success)
	}

	if !resp.Success {
		msg := fmt.Sprintf("pi: %s failed", resp.Command)
		if resp.Error != "" {
			msg = fmt.Sprintf("pi: %s failed: %s", resp.Command, resp.Error)
		}
		if p.debug {
			log.Printf("[PI-RESPONSE] ERROR %s", msg)
		}
		p.writeControl(msg)
		return
	}

	// Handle successful responses based on command type
	switch resp.Command {
	case "get_available_models":
		p.handleAvailableModelsResponse(line)
	case "set_model":
		p.handleSetModelResponse(line)
	case "get_commands":
		p.handleGetCommandsResponse(line)
	case "abort":
		p.handleAbortResponse(line)
	case "new_session":
		p.handleNewSessionResponse(line)
	case "switch_session":
		p.handleSwitchSessionResponse(line)
	case "get_last_assistant_text":
		p.handleGetLastAssistantTextResponse(line)
	case "get_state":
		p.handleGetStateResponse(line)
	case "set_thinking_level":
		p.handleSetThinkingLevelResponse(line)
	case "cycle_thinking_level":
		p.handleCycleThinkingLevelResponse(line)
	case "get_messages":
		p.handleGetMessagesResponse(line)
	case "get_session_stats":
		p.handleGetSessionStatsResponse(line)
	case "set_auto_compaction":
		p.handleSetAutoCompactionResponse(line)
	case "compact":
		p.handleCompactResponse(line)
	}
}

func (p *PiInterpreter) handleAvailableModelsResponse(line string) {
	var resp rpcModelResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		p.writeControl(fmt.Sprintf("error parsing models response: %v", err))
		return
	}

	if len(resp.Data.Models) == 0 {
		p.writeControl("no models available")
		return
	}

	p.updateAvailableModels(resp.Data.Models)
}

func (p *PiInterpreter) handleSetModelResponse(line string) {
	var resp rpcSetModelResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		p.writeControl(fmt.Sprintf("error parsing set_model response: %v", err))
		return
	}

	p.stateMu.Lock()
	p.currentModelID = resp.Data.ID
	p.currentModelProvider = resp.Data.Provider
	modelName := resp.Data.Name
	p.stateMu.Unlock()

	modelRef := formatModelRef(resp.Data.Provider, resp.Data.ID)
	p.writeControlNoScroll(fmt.Sprintf("Model changed to: %s (%s)", modelRef, modelName))
}

func (p *PiInterpreter) handleMessageStart(line string) {
	var evt rpcMessageStart
	if err := json.Unmarshal([]byte(line), &evt); err != nil {
		return
	}
	p.stateMu.Lock()
	p.currentMessageRole = evt.Message.Role
	p.currentMessageStreamed = false
	p.stateMu.Unlock()
}

func (p *PiInterpreter) handleMessageUpdate(line string) {
	var evt rpcMessageUpdate
	if err := json.Unmarshal([]byte(line), &evt); err != nil {
		return
	}

	role := evt.Message.Role
	if role == "" {
		role = p.getCurrentMessageRole()
	}

	if role != "assistant" {
		return
	}

	switch evt.AssistantMessageEvent.Type {
	case "text_start":
		if p.getShowAssistant() {
			p.writePrefix("assistant")
			p.setCurrentMessageStreamed(true)
		}
	case "text_delta":
		if p.getShowAssistant() && evt.AssistantMessageEvent.Delta != "" {
			p.writeRaw(evt.AssistantMessageEvent.Delta)
			p.setCurrentMessageStreamed(true)
		}
	case "text_end":
		if p.getShowAssistant() {
			p.writeRaw("\n")
		}
	case "thinking_start":
		if p.getShowThinking() {
			p.writePrefix("thinking")
			p.setCurrentMessageStreamed(true)
		}
	case "thinking_delta":
		if p.getShowThinking() && evt.AssistantMessageEvent.Delta != "" {
			p.writeRaw(evt.AssistantMessageEvent.Delta)
			p.setCurrentMessageStreamed(true)
		}
	case "thinking_end":
		if p.getShowThinking() {
			p.writeRaw("\n")
		}
	}
}

func (p *PiInterpreter) handleMessageEnd(line string) {
	var evt rpcMessageEnd
	if err := json.Unmarshal([]byte(line), &evt); err != nil {
		return
	}

	if evt.Message.Role != "assistant" {
		return
	}

	if p.getCurrentMessageStreamed() {
		return
	}

	blocks := p.extractContentBlocks(evt.Message.Content)
	p.writeMessageBlocks(blocks)
	p.setCurrentMessageStreamed(true)
}

func (p *PiInterpreter) handleToolStart(line string) {
	var evt rpcToolStart
	if err := json.Unmarshal([]byte(line), &evt); err != nil {
		return
	}
	p.stateMu.Lock()
	p.toolStates[evt.ToolCallId] = &piToolState{toolName: evt.ToolName}
	p.stateMu.Unlock()

	// Always show tool call start (even when showTools=false) so user knows pi is working
	if p.getShowPrefixes() {
		p.writeRaw(fmt.Sprintf("[calling %s...]", evt.ToolName))
	} else {
		p.writeRaw(fmt.Sprintf("[%s...]", evt.ToolName))
	}
}

func (p *PiInterpreter) handleToolUpdate(line string) {
	if !p.getShowTools() {
		return
	}

	var evt rpcToolUpdate
	if err := json.Unmarshal([]byte(line), &evt); err != nil {
		return
	}
	if evt.ToolCallId == "" {
		return
	}

	output := p.toolResultText(evt.PartialResult)
	if output == "" {
		return
	}

	p.stateMu.Lock()
	state := p.toolStates[evt.ToolCallId]
	if state == nil {
		state = &piToolState{toolName: evt.ToolName}
		p.toolStates[evt.ToolCallId] = state
	}
	delta := output
	if state.lastOutput != "" {
		if strings.HasPrefix(output, state.lastOutput) {
			delta = output[len(state.lastOutput):]
		}
	}
	state.lastOutput = output
	p.stateMu.Unlock()

	if delta == "" {
		return
	}

	if !state.prefixWritten {
		p.writePrefix(fmt.Sprintf("tool:%s", state.toolName))
		p.stateMu.Lock()
		state.prefixWritten = true
		p.stateMu.Unlock()
	}
	p.writeRaw(delta)
}

func (p *PiInterpreter) handleToolEnd(line string) {
	var evt rpcToolEnd
	if err := json.Unmarshal([]byte(line), &evt); err != nil {
		return
	}

	// Get tool state for name
	p.stateMu.Lock()
	state := p.toolStates[evt.ToolCallId]
	toolName := ""
	if state != nil {
		toolName = state.toolName
	}
	p.stateMu.Unlock()

	// Always print tool end (even when showTools=false) so user knows it's done
	if toolName != "" {
		if evt.IsError {
			if p.getShowPrefixes() {
				p.writeRaw(fmt.Sprintf("[%s error]\n", toolName))
			} else {
				p.writeRaw("[error]\n")
			}
		} else {
			if p.getShowPrefixes() {
				p.writeRaw(fmt.Sprintf("[%s done]\n", toolName))
			} else {
				p.writeRaw("[done]\n")
			}
		}
	}

	// Only show tool output if showTools=true
	if !p.getShowTools() {
		p.stateMu.Lock()
		delete(p.toolStates, evt.ToolCallId)
		p.stateMu.Unlock()
		return
	}

	output := p.toolResultText(evt.Result)
	p.stateMu.Lock()
	if state == nil {
		state = &piToolState{toolName: evt.ToolName}
		p.toolStates[evt.ToolCallId] = state
	}
	delta := output
	if state.lastOutput != "" {
		if strings.HasPrefix(output, state.lastOutput) {
			delta = output[len(state.lastOutput):]
		}
	}
	state.lastOutput = output
	p.stateMu.Unlock()

	if delta != "" {
		if !state.prefixWritten {
			p.writePrefix(fmt.Sprintf("tool:%s", state.toolName))
			p.stateMu.Lock()
			state.prefixWritten = true
			p.stateMu.Unlock()
		}
		p.writeRaw(delta)
	}

	if !strings.HasSuffix(output, "\n") {
		p.writeRaw("\n")
	}

	p.stateMu.Lock()
	delete(p.toolStates, evt.ToolCallId)
	p.stateMu.Unlock()
}

func (p *PiInterpreter) extractContentBlocks(raw json.RawMessage) []rpcContentBlock {
	if len(raw) == 0 {
		return nil
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return []rpcContentBlock{{Type: "text", Text: asString}}
	}

	var blocks []rpcContentBlock
	if err := json.Unmarshal(raw, &blocks); err == nil {
		return blocks
	}

	return nil
}

func (p *PiInterpreter) writeMessageBlocks(blocks []rpcContentBlock) {
	for _, block := range blocks {
		switch block.Type {
		case "text":
			if p.getShowAssistant() {
				p.writeWithPrefix("assistant", block.Text)
			}
		case "thinking":
			if p.getShowThinking() {
				p.writeWithPrefix("thinking", block.Thinking)
			}
		}
	}
}

func (p *PiInterpreter) toolResultText(result *rpcToolResult) string {
	if result == nil {
		return ""
	}
	var sb strings.Builder
	for _, c := range result.Content {
		switch c.Type {
		case "text":
			sb.WriteString(c.Text)
		}
	}
	return sb.String()
}

func (p *PiInterpreter) writeWithPrefix(prefix, body string) {
	if p.getShowPrefixes() && prefix != "" {
		p.writeRaw(prefix + ": ")
	}
	if body != "" {
		p.writeRaw(body)
	}
	p.writeRaw("\n")
}

func (p *PiInterpreter) writePrefix(prefix string) {
	if p.getShowPrefixes() && prefix != "" {
		p.writeRaw(prefix + ": ")
	}
}

func (p *PiInterpreter) writeControl(msg string) {
	p.writeRaw(msg)
	p.writeRaw("\n")
}

// writeControlNoScroll writes a control message without scrolling.
// Use this in event handlers to avoid deadlock.
func (p *PiInterpreter) writeControlNoScroll(msg string) {
	p.writeRawNoScroll(msg)
	p.writeRawNoScroll("\n")
}

func (p *PiInterpreter) writeRaw(s string) {
	writer := p.GetOutputWriter()
	if writer == nil || s == "" {
		return
	}
	_ = writer.Write(s)
	if err := writer.ScrollToBottom(); err != nil {
		// Log error but don't fail the write
		_ = fmt.Sprintf("scroll to bottom error: %v", err)
	}
}

// writeRawNoScroll writes without scrolling to avoid deadlock in event handlers.
func (p *PiInterpreter) writeRawNoScroll(s string) {
	writer := p.GetOutputWriter()
	if writer == nil || s == "" {
		return
	}
	_ = writer.Write(s)
}

func (p *PiInterpreter) getStreaming() bool {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	return p.isStreaming
}

func (p *PiInterpreter) setStreaming(val bool) {
	p.stateMu.Lock()
	p.isStreaming = val
	p.stateMu.Unlock()
}

func (p *PiInterpreter) getCurrentMessageRole() string {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	return p.currentMessageRole
}

func (p *PiInterpreter) setCurrentMessageStreamed(val bool) {
	p.stateMu.Lock()
	p.currentMessageStreamed = val
	p.stateMu.Unlock()
}

func (p *PiInterpreter) getCurrentMessageStreamed() bool {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	return p.currentMessageStreamed
}

func (p *PiInterpreter) getShowAssistant() bool {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	return p.showAssistant
}

func (p *PiInterpreter) getShowThinking() bool {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	return p.showThinking
}

func (p *PiInterpreter) getShowTools() bool {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	return p.showTools
}

func (p *PiInterpreter) getShowPrefixes() bool {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	return p.showPrefixes
}

// Display commands

func (p *PiInterpreter) showHelp() {
	var sb strings.Builder
	sb.WriteString("═════════════════════════════════════\n")
	sb.WriteString("win Commands (use /command or :win command)\n")
	sb.WriteString("═════════════════════════════════════\n\n")

	sb.WriteString("Information Commands:\n")
	sb.WriteString("  /session            - Show pi session state\n")
	sb.WriteString("  /messages            - Show all messages history\n")
	sb.WriteString("  /show-usage          - Show session statistics\n")
	sb.WriteString("  /commands            - Show available commands (skills, prompts, extensions)\n\n")

	sb.WriteString("Display Settings (win-specific):\n")
	sb.WriteString("  /show-settings       - Show win display settings\n")
	sb.WriteString("  /thinking [on|off]    - Show/hide AI thinking\n")
	sb.WriteString("  /tools [on|off]      - Show/hide full tool output\n")
	sb.WriteString("  /prefix [on|off]     - Show/hide label prefixes\n\n")

	sb.WriteString("Model Management:\n")
	sb.WriteString("  /model-select        - Interactive model selection\n\n")

	sb.WriteString("Session Management:\n")
	sb.WriteString("  /new                 - Start a new session\n")
	sb.WriteString("  /resume              - Resume from previous session\n\n")

	sb.WriteString("Context Control:\n")
	sb.WriteString("  /compact [instructions] - Manually compact context\n")
	sb.WriteString("  /copy                - Copy last assistant message to clipboard\n\n")

	sb.WriteString("Settings:\n")
	sb.WriteString("  /auto-compaction <on|off> - Enable/disable auto-compaction\n")
	sb.WriteString("  /thinking-level <off|minimal|low|medium|high|xhigh>\n")
	sb.WriteString("                       - Set pi thinking level\n")
	sb.WriteString("  /cycle-thinking-level  - Cycle through available thinking levels\n\n")

	sb.WriteString("Win Control:\n")
	sb.WriteString("  /abort               - Abort current operation\n")
	sb.WriteString("  /quit                - Exit win\n")
	sb.WriteString("  /help                - Show this help message\n\n")

	sb.WriteString("═════════════════════════════════════\n")

	p.writeControlNoScroll(sb.String())
}

// Model management functions

func (p *PiInterpreter) triggerModelSelect() error {
	// Execute the external pi-model-select script
	home := os.Getenv("HOME")
	scriptPath := filepath.Join(home, ".ad", "bin", "pi-model-select")

	// Check if script exists
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		p.writeControlNoScroll("win: pi-model-select script not found")
		p.writeControlNoScroll(fmt.Sprintf("Expected at: %s", scriptPath))
		p.writeControlNoScroll("")
		p.writeControlNoScroll("You can still use the external command directly:")
		p.writeControlNoScroll("  pi-model-select")
		return fmt.Errorf("pi-model-select script not found")
	}

	// Run the script
	cmd := exec.Command(scriptPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	p.writeControlNoScroll("win: launching model selection...")
	if err := cmd.Run(); err != nil {
		p.writeControlNoScroll(fmt.Sprintf("win: model selection failed: %v", err))
		return fmt.Errorf("run pi-model-select: %w", err)
	}

	return nil
}

func (p *PiInterpreter) showSettings() {
	p.writeControlNoScroll("=== win display settings ===")
	p.writeControlNoScroll(fmt.Sprintf("thinking: %v (show AI thinking)", p.getShowThinking()))
	p.writeControlNoScroll(fmt.Sprintf("tools: %v (show full tool output, tool calls always shown)", p.getShowTools()))
	p.writeControlNoScroll(fmt.Sprintf("prefix: %v (show 'assistant:', 'thinking:', 'tool:xxx:' labels)", p.getShowPrefixes()))

	p.stateMu.Lock()
	if p.workingDir != "" {
		p.writeControlNoScroll(fmt.Sprintf("working-dir: %s", p.workingDir))
	}
	if p.currentModelID != "" || p.currentModelProvider != "" {
		p.writeControlNoScroll(fmt.Sprintf("model: %s", formatModelRef(p.currentModelProvider, p.currentModelID)))
	} else {
		p.writeControlNoScroll("model: (not set)")
	}
	if p.currentThinkingLevel != "" {
		p.writeControlNoScroll(fmt.Sprintf("thinking-level: %s", p.currentThinkingLevel))
	}
	p.writeControlNoScroll(fmt.Sprintf("auto-compaction: %v", p.autoCompactionEnabled))
	if p.cmd != nil && p.cmd.Process != nil {
		p.writeControlNoScroll(fmt.Sprintf("pi-pid: %d", p.cmd.Process.Pid))
	}
	p.stateMu.Unlock()

	p.writeControlNoScroll("=============================")
}

func (p *PiInterpreter) compact(customInstructions string) error {
	cmd := map[string]interface{}{
		"type": "compact",
	}

	if customInstructions != "" {
		cmd["customInstructions"] = customInstructions
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		p.writeControlNoScroll(fmt.Sprintf("win: failed to marshal compact: %v", err))
		return err
	}
	data = append(data, '\n')

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stdin == nil {
		p.writeControlNoScroll("win: pi stdin not available")
		return fmt.Errorf("pi stdin not available")
	}

	if _, err := p.stdin.Write(data); err != nil {
		p.writeControlNoScroll(fmt.Sprintf("win: failed to write to pi: %v", err))
		return err
	}

	p.writeControlNoScroll("win: compaction requested...")
	return nil
}

func (p *PiInterpreter) handleCompactResponse(line string) error {
	var resp struct {
		Type    string `json:"type"`
		Command string `json:"command"`
		Success bool   `json:"success"`
		Error   string `json:"error"`
		Data    struct {
			Summary          string `json:"summary"`
			FirstKeptEntryID string `json:"firstKeptEntryId"`
			TokensBefore     int    `json:"tokensBefore"`
			TokensAfter      int    `json:"tokensAfter"`
		} `json:"data"`
	}

	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return err
	}

	if !resp.Success {
		p.writeControlNoScroll(fmt.Sprintf("win: compaction failed: %s", resp.Error))
		return nil
	}

	var sb strings.Builder
	sb.WriteString("═════════════════════════════════════\n")
	sb.WriteString("Compaction Complete\n")
	sb.WriteString("═════════════════════════════════════\n\n")
	sb.WriteString(fmt.Sprintf("Tokens before: %d\n", resp.Data.TokensBefore))
	sb.WriteString(fmt.Sprintf("Tokens after:  %d\n", resp.Data.TokensAfter))
	if resp.Data.TokensBefore > 0 {
		reduction := float64(resp.Data.TokensBefore-resp.Data.TokensAfter) / float64(resp.Data.TokensBefore) * 100
		sb.WriteString(fmt.Sprintf("Reduction:    %.1f%%\n", reduction))
	}
	sb.WriteString(fmt.Sprintf("\nSummary: %s\n", resp.Data.Summary))
	sb.WriteString("═════════════════════════════════════\n")

	p.writeControlNoScroll(sb.String())
	return nil
}

func (p *PiInterpreter) getSessionCWD(sessionPath string) (string, error) {
	// Read first line of session file to get cwd
	file, err := os.Open(sessionPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if scanner.Scan() {
		line := scanner.Text()
		var sessionEntry struct {
			Type string `json:"type"`
			CWD  string `json:"cwd"`
		}
		if err := json.Unmarshal([]byte(line), &sessionEntry); err == nil {
			if sessionEntry.Type == "session" && sessionEntry.CWD != "" {
				return sessionEntry.CWD, nil
			}
		}
	}
	return "", nil
}

func (p *PiInterpreter) triggerSessionResume(sessionPath string) error {
	if sessionPath == "" {
		// No path provided, execute the external pi-session-select script
		home := os.Getenv("HOME")
		scriptPath := filepath.Join(home, ".ad", "bin", "pi-session-select")

		// Check if script exists
		if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
			p.writeControlNoScroll("win: pi-session-select script not found")
			p.writeControlNoScroll(fmt.Sprintf("Expected at: %s", scriptPath))
			p.writeControlNoScroll("")
			p.writeControlNoScroll("You can still use the external command directly:")
			p.writeControlNoScroll("  pi-session-select")
			p.writeControlNoScroll("")
			p.writeControlNoScroll("Or provide a session path directly:")
			p.writeControlNoScroll("  /resume ~/.pi/agent/sessions/--...--/session.jsonl")
			return fmt.Errorf("pi-session-select script not found")
		}

		// Run the script
		cmd := exec.Command(scriptPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin

		p.writeControlNoScroll("win: launching session selection...")
		if err := cmd.Run(); err != nil {
			p.writeControlNoScroll(fmt.Sprintf("win: session selection failed: %v", err))
			return fmt.Errorf("run pi-session-select: %w", err)
		}

		return nil
	}

	// Validate the session path
	if !filepath.IsAbs(sessionPath) {
		home := os.Getenv("HOME")
		if strings.HasPrefix(sessionPath, "~/") {
			sessionPath = filepath.Join(home, sessionPath[2:])
		}
	}

	if _, err := os.Stat(sessionPath); os.IsNotExist(err) {
		p.writeControlNoScroll(fmt.Sprintf("win: session file not found: %s", sessionPath))
		return nil
	}

	// Check session CWD vs current working directory
	sessionCWD, err := p.getSessionCWD(sessionPath)
	if err == nil && sessionCWD != "" {
		p.stateMu.Lock()
		currentCWD := p.workingDir
		p.stateMu.Unlock()

		// Normalize paths for comparison
		absSessionCWD, _ := filepath.Abs(sessionCWD)
		absCurrentCWD, _ := filepath.Abs(currentCWD)

		if absSessionCWD != absCurrentCWD {
			p.writeControlNoScroll("═════════════════════════════════════")
			p.writeControlNoScroll("⚠️  Working Directory Mismatch")
			p.writeControlNoScroll("═════════════════════════════════════")
			p.writeControlNoScroll("")
			p.writeControlNoScroll(fmt.Sprintf("Session CWD:    %s", sessionCWD))
			p.writeControlNoScroll(fmt.Sprintf("Current CWD:   %s", currentCWD))
			p.writeControlNoScroll("")
			p.writeControlNoScroll("This session was created in a different directory.")
			p.writeControlNoScroll("Relative paths and file operations may not work correctly.")
			p.writeControlNoScroll("")
			p.writeControlNoScroll("To switch to this project, consider:")
			p.writeControlNoScroll("  1. Navigate to the project directory")
			p.writeControlNoScroll("  2. Start a new pi session with /new")
			p.writeControlNoScroll("  3. Use /resume to load the correct session")
			p.writeControlNoScroll("")
			p.writeControlNoScroll("Continuing anyway...")
			p.writeControlNoScroll("═════════════════════════════════════")
			p.writeControlNoScroll("")
		}
	}

	// Send switch_session RPC command
	cmd := map[string]interface{}{
		"type":        "switch_session",
		"sessionPath": sessionPath,
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal switch_session: %w", err)
	}
	data = append(data, '\n')

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stdin == nil {
		return fmt.Errorf("pi stdin not available")
	}

	if _, err := p.stdin.Write(data); err != nil {
		p.writeControlNoScroll(fmt.Sprintf("win: failed to switch session: %v", err))
		return fmt.Errorf("write to pi: %w", err)
	}

	p.writeControlNoScroll(fmt.Sprintf("win: switching to session: %s", filepath.Base(sessionPath)))
	return nil
}

func (p *PiInterpreter) copyLastAssistantText() error {
	// Send get_last_assistant_text command
	cmd := map[string]interface{}{
		"type": "get_last_assistant_text",
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal get_last_assistant_text: %w", err)
	}
	data = append(data, '\n')

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stdin == nil {
		return fmt.Errorf("pi stdin not available")
	}

	if _, err := p.stdin.Write(data); err != nil {
		p.writeControlNoScroll(fmt.Sprintf("win: failed to get last assistant text: %v", err))
		return fmt.Errorf("write to pi: %w", err)
	}

	p.writeControlNoScroll("win: fetching last assistant message...")
	return nil
}

func (p *PiInterpreter) showAvailableModels() error {
	// Send get_available_models command
	cmd := map[string]interface{}{
		"type": "get_available_models",
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal get_available_models: %w", err)
	}
	data = append(data, '\n')

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stdin == nil {
		return fmt.Errorf("pi stdin not available")
	}

	if _, err := p.stdin.Write(data); err != nil {
		return fmt.Errorf("write to pi: %w", err)
	}

	return nil
}

func (p *PiInterpreter) setModel(provider, modelID string) error {
	if provider == "" || modelID == "" {
		return fmt.Errorf("model must include provider and id (e.g. provider/model-id)")
	}
	cmd := map[string]interface{}{
		"type":     "set_model",
		"provider": provider,
		"modelId":  modelID,
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal set_model: %w", err)
	}
	data = append(data, '\n')

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stdin == nil {
		return fmt.Errorf("pi stdin not available")
	}

	if _, err := p.stdin.Write(data); err != nil {
		return fmt.Errorf("write to pi: %w", err)
	}

	return nil
}

func (p *PiInterpreter) setModelFromInput(input string) error {
	input = strings.TrimSpace(input)
	if input == "" {
		return fmt.Errorf("model not specified")
	}

	// Strip numeric prefix "N: ..." if present
	if idx := strings.Index(input, ":"); idx > 0 {
		prefix := strings.TrimSpace(input[:idx])
		if _, err := parseInt(prefix); err == nil {
			input = strings.TrimSpace(input[idx+1:])
		}
	}

	// If input includes extra fields (e.g. "provider/id - Name"), take the first token
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return fmt.Errorf("model not specified")
	}
	token := fields[0]

	// Try to parse as number (index into available models)
	p.stateMu.Lock()
	availableModels := make([]Model, len(p.availableModels))
	copy(availableModels, p.availableModels)
	p.stateMu.Unlock()

	var provider string
	var modelID string

	// Try to parse as number
	if num, err := parseInt(token); err == nil {
		if num >= 0 && num < len(availableModels) {
			modelID = availableModels[num].ID
			provider = availableModels[num].Provider
		}
	}

	// Try to parse provider/modelId or provider:modelId
	if modelID == "" {
		if p, id, ok := splitModelRef(token); ok {
			provider = p
			modelID = id
		}
	}

	// Try to match by model ID (or prefix) using cached list
	if modelID == "" {
		match, err := matchModelByID(token, availableModels)
		if err != nil {
			return err
		}
		if match.ID != "" {
			modelID = match.ID
			provider = match.Provider
		}
	}

	if modelID == "" || provider == "" {
		if len(availableModels) == 0 {
			return fmt.Errorf("model not found: %s (run /models to list available, then use /model-select or provider/model-id)", input)
		}
		return fmt.Errorf("model not found: %s (use /model-select or provider/model-id)", input)
	}

	return p.setModel(provider, modelID)
}

func parseInt(s string) (int, error) {
	var result int
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("not a number")
		}
		result = result*10 + int(ch-'0')
	}
	return result, nil
}

func splitModelRef(s string) (string, string, bool) {
	if s == "" {
		return "", "", false
	}
	if strings.Contains(s, "/") {
		parts := strings.SplitN(s, "/", 2)
		if parts[0] != "" && parts[1] != "" {
			return parts[0], parts[1], true
		}
	}
	if strings.Contains(s, ":") {
		parts := strings.SplitN(s, ":", 2)
		if parts[0] != "" && parts[1] != "" {
			return parts[0], parts[1], true
		}
	}
	return "", "", false
}

func matchModelByID(input string, models []Model) (Model, error) {
	var matches []Model
	for _, m := range models {
		if m.ID == input || strings.HasPrefix(m.ID, input) {
			matches = append(matches, m)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return Model{}, fmt.Errorf("model id is ambiguous: %s (use provider/model-id)", input)
	}
	return Model{}, nil
}

func formatModelRef(provider, id string) string {
	if provider == "" {
		return id
	}
	if id == "" {
		return provider
	}
	return provider + "/" + id
}

func (p *PiInterpreter) updateAvailableModels(models []Model) {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	p.availableModels = models

	// Render model list
	var sb strings.Builder
	sb.WriteString("═════════════════════════════════════\n")
	sb.WriteString("Available Models\n")
	sb.WriteString("═════════════════════════════════════\n\n")

	for i, m := range models {
		currentMarker := ""
		if p.currentModelID == m.ID && p.currentModelProvider == m.Provider {
			currentMarker = " [current]"
		}
		modelRef := formatModelRef(m.Provider, m.ID)
		sb.WriteString(fmt.Sprintf("%d: %-32s - %s%s\n", i, modelRef, m.Name, currentMarker))
	}

	sb.WriteString("\n")
	sb.WriteString("═════════════════════════════════════\n\n")
	sb.WriteString("Usage:\n")
	sb.WriteString("  - Visual select a model line above\n")
	sb.WriteString("  - Press: <space> p m to set selected model\n")
	sb.WriteString("  - Or type: /model <number|provider/model-id>\n\n")

	p.writeControlNoScroll(sb.String())
}

// Abort command implementation

func (p *PiInterpreter) abort() error {
	cmd := map[string]interface{}{
		"type": "abort",
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal abort: %w", err)
	}
	data = append(data, '\n')

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stdin == nil {
		return fmt.Errorf("pi stdin not available")
	}

	if _, err := p.stdin.Write(data); err != nil {
		return fmt.Errorf("write to pi: %w", err)
	}

	return nil
}

func (p *PiInterpreter) handleAbortResponse(line string) {
	var resp rpcResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return
	}

	if !resp.Success {
		msg := "pi: abort failed"
		if resp.Error != "" {
			msg = fmt.Sprintf("pi: abort failed: %s", resp.Error)
		}
		p.writeControl(msg)
		return
	}

	p.writeControl("pi: operation aborted")
}

// Commands command implementation

func (p *PiInterpreter) getCommands() error {
	cmd := map[string]interface{}{
		"type": "get_commands",
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal get_commands: %w", err)
	}
	data = append(data, '\n')

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stdin == nil {
		return fmt.Errorf("pi stdin not available")
	}

	if _, err := p.stdin.Write(data); err != nil {
		return fmt.Errorf("write to pi: %w", err)
	}

	return nil
}

func (p *PiInterpreter) handleGetCommandsResponse(line string) {
	var resp rpcGetCommandsResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		p.writeControl(fmt.Sprintf("error parsing commands response: %v", err))
		return
	}

	if !resp.Success {
		msg := "pi: get_commands failed"
		if resp.Error != "" {
			msg = fmt.Sprintf("pi: get_commands failed: %s", resp.Error)
		}
		p.writeControl(msg)
		return
	}

	if len(resp.Data.Commands) == 0 {
		p.writeControl("no commands available")
		return
	}

	// Group commands by source
	extensions := make([]rpcSlashCommand, 0)
	prompts := make([]rpcSlashCommand, 0)
	skills := make([]rpcSlashCommand, 0)

	for _, cmd := range resp.Data.Commands {
		switch cmd.Source {
		case "extension":
			extensions = append(extensions, cmd)
		case "prompt":
			prompts = append(prompts, cmd)
		case "skill":
			skills = append(skills, cmd)
		}
	}

	var sb strings.Builder
	sb.WriteString("═════════════════════════════════════\n")
	sb.WriteString("Available Commands\n")
	sb.WriteString("═════════════════════════════════════\n\n")

	if len(skills) > 0 {
		sb.WriteString("Skills (/skill:name):\n")
		for _, cmd := range skills {
			name := strings.TrimPrefix(cmd.Name, "skill:")
			desc := cmd.Description
			if desc != "" {
				sb.WriteString(fmt.Sprintf("  /skill:%-30s - %s\n", name, desc))
			} else {
				sb.WriteString(fmt.Sprintf("  /skill:%s\n", name))
			}
		}
		sb.WriteString("\n")
	}

	if len(prompts) > 0 {
		sb.WriteString("Prompt Templates (/name):\n")
		for _, cmd := range prompts {
			desc := cmd.Description
			if desc != "" {
				sb.WriteString(fmt.Sprintf("  /%-33s - %s\n", cmd.Name, desc))
			} else {
				sb.WriteString(fmt.Sprintf("  /%s\n", cmd.Name))
			}
		}
		sb.WriteString("\n")
	}

	if len(extensions) > 0 {
		sb.WriteString("Extension Commands (/name):\n")
		for _, cmd := range extensions {
			desc := cmd.Description
			if desc != "" {
				sb.WriteString(fmt.Sprintf("  /%-33s - %s\n", cmd.Name, desc))
			} else {
				sb.WriteString(fmt.Sprintf("  /%s\n", cmd.Name))
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("═════════════════════════════════════\n")

	p.writeControlNoScroll(sb.String())
}

// New session command implementation

func (p *PiInterpreter) newSession() error {
	cmd := map[string]interface{}{
		"type": "new_session",
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal new_session: %w", err)
	}
	data = append(data, '\n')

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stdin == nil {
		return fmt.Errorf("pi stdin not available")
	}

	if _, err := p.stdin.Write(data); err != nil {
		return fmt.Errorf("write to pi: %w", err)
	}

	return nil
}

func (p *PiInterpreter) handleNewSessionResponse(line string) {
	var resp rpcNewSessionResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return
	}

	if !resp.Success {
		msg := "pi: new_session failed"
		if resp.Error != "" {
			msg = fmt.Sprintf("pi: new_session failed: %s", resp.Error)
		}
		p.writeControl(msg)
		return
	}

	if resp.Data.Cancelled {
		p.writeControl("pi: new session cancelled by extension")
		return
	}

	p.writeControl("pi: started new session")
}

func (p *PiInterpreter) handleSwitchSessionResponse(line string) {
	var resp rpcSwitchSessionResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return
	}

	if !resp.Success {
		msg := "pi: switch_session failed"
		if resp.Error != "" {
			msg = fmt.Sprintf("pi: switch_session failed: %s", resp.Error)
		}
		p.writeControl(msg)
		return
	}

	if resp.Data.Cancelled {
		p.writeControl("pi: session switch cancelled by extension")
		return
	}

	p.writeControl("pi: session switched successfully")
}

func (p *PiInterpreter) handleGetLastAssistantTextResponse(line string) {
	var resp rpcGetLastAssistantTextResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return
	}

	if !resp.Success {
		msg := "pi: get_last_assistant_text failed"
		if resp.Error != "" {
			msg = fmt.Sprintf("pi: get_last_assistant_text failed: %s", resp.Error)
		}
		p.writeControl(msg)
		return
	}

	if resp.Data.Text == "" {
		p.writeControl("win: no assistant message found")
		return
	}

	// Copy to system clipboard using pbcopy (macOS)
	cmd := exec.Command("pbcopy")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		p.writeControl(fmt.Sprintf("win: failed to copy to clipboard: %v", err))
		return
	}

	if err := cmd.Start(); err != nil {
		p.writeControl(fmt.Sprintf("win: failed to start pbcopy: %v", err))
		return
	}

	if _, err := stdin.Write([]byte(resp.Data.Text)); err != nil {
		p.writeControl(fmt.Sprintf("win: failed to write to clipboard: %v", err))
		return
	}

	if err := stdin.Close(); err != nil {
		p.writeControl(fmt.Sprintf("win: failed to close stdin: %v", err))
		return
	}

	if err := cmd.Wait(); err != nil {
		p.writeControl(fmt.Sprintf("win: pbcopy failed: %v", err))
		return
	}

	// Calculate character count
	charCount := len(resp.Data.Text)
	lineCount := strings.Count(resp.Data.Text, "\n") + 1

	p.writeControl(fmt.Sprintf("win: copied last assistant message to clipboard (%d characters, %d lines)", charCount, lineCount))
}

func (p *PiInterpreter) cycleThinkingLevel() error {
	// Send cycle_thinking_level command
	cmd := map[string]interface{}{
		"type": "cycle_thinking_level",
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal cycle_thinking_level: %w", err)
	}
	data = append(data, '\n')

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stdin == nil {
		return fmt.Errorf("pi stdin not available")
	}

	if _, err := p.stdin.Write(data); err != nil {
		p.writeControlNoScroll(fmt.Sprintf("win: failed to cycle thinking level: %v", err))
		return fmt.Errorf("write to pi: %w", err)
	}

	p.writeControlNoScroll("win: cycling thinking level...")
	return nil
}

func (p *PiInterpreter) handleCycleThinkingLevelResponse(line string) {
	var resp rpcCycleThinkingLevelResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return
	}

	if !resp.Success {
		msg := "pi: cycle_thinking_level failed"
		if resp.Error != "" {
			msg = fmt.Sprintf("pi: cycle_thinking_level failed: %s", resp.Error)
		}
		p.writeControl(msg)
		return
	}

	if resp.Data.Level != "" {
		p.writeControl(fmt.Sprintf("pi: thinking level set to: %s", resp.Data.Level))
	} else {
		p.writeControl("pi: cycling thinking level (model doesn't support thinking)")
	}
}

// Get state command implementation

func (p *PiInterpreter) getState() error {
	cmd := map[string]interface{}{
		"type": "get_state",
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal get_state: %w", err)
	}
	data = append(data, '\n')

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stdin == nil {
		return fmt.Errorf("pi stdin not available")
	}

	if _, err := p.stdin.Write(data); err != nil {
		return fmt.Errorf("write to pi: %w", err)
	}

	return nil
}

func (p *PiInterpreter) handleGetStateResponse(line string) {
	var resp rpcGetStateResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		p.writeControl(fmt.Sprintf("error parsing get_state response: %v", err))
		return
	}

	if !resp.Success {
		msg := "pi: get_state failed"
		if resp.Error != "" {
			msg = fmt.Sprintf("pi: get_state failed: %s", resp.Error)
		}
		p.writeControl(msg)
		return
	}

	data := resp.Data
	p.stateMu.Lock()
	p.currentThinkingLevel = data.ThinkingLevel
	p.autoCompactionEnabled = data.AutoCompactionEnabled
	if data.Model != nil {
		p.currentModelID = data.Model.ID
		p.currentModelProvider = data.Model.Provider
	}
	p.stateMu.Unlock()

	var sb strings.Builder
	sb.WriteString("═════════════════════════════════════\n")
	sb.WriteString("Session State\n")
	sb.WriteString("═════════════════════════════════════\n\n")

	sb.WriteString(fmt.Sprintf("Session ID: %s\n", data.SessionID))
	if data.SessionName != "" {
		sb.WriteString(fmt.Sprintf("Session Name: %s\n", data.SessionName))
	}
	if data.SessionFile != "" {
		sb.WriteString(fmt.Sprintf("Session File: %s\n", data.SessionFile))
	}
	sb.WriteString(fmt.Sprintf("Streaming: %v\n", data.IsStreaming))
	sb.WriteString(fmt.Sprintf("Compacting: %v\n", data.IsCompacting))
	sb.WriteString(fmt.Sprintf("Steering Mode: %s\n", data.SteeringMode))
	sb.WriteString(fmt.Sprintf("Follow-up Mode: %s\n", data.FollowUpMode))
	sb.WriteString(fmt.Sprintf("Auto-compaction: %v\n", data.AutoCompactionEnabled))
	sb.WriteString(fmt.Sprintf("Thinking Level: %s\n", data.ThinkingLevel))
	sb.WriteString(fmt.Sprintf("Message Count: %d\n", data.MessageCount))
	sb.WriteString(fmt.Sprintf("Pending Messages: %d\n", data.PendingMessageCount))

	// Add working directory info
	p.stateMu.Lock()
	workingDir := p.workingDir
	if p.cmd != nil && p.cmd.Process != nil {
		sb.WriteString(fmt.Sprintf("\nwin/pi PID: %d\n", p.cmd.Process.Pid))
	}
	p.stateMu.Unlock()
	if workingDir != "" {
		sb.WriteString(fmt.Sprintf("Working Directory: %s\n", workingDir))
	}

	if data.Model != nil {
		sb.WriteString(fmt.Sprintf("\nCurrent Model: %s\n", formatModelRef(data.Model.Provider, data.Model.ID)))
		sb.WriteString(fmt.Sprintf("  Name: %s\n", data.Model.Name))
		sb.WriteString(fmt.Sprintf("  API: %s\n", data.Model.API))
		sb.WriteString(fmt.Sprintf("  Context Window: %d\n", data.Model.ContextWindow))
		sb.WriteString(fmt.Sprintf("  Max Tokens: %d\n", data.Model.MaxTokens))
	}

	sb.WriteString("\n═════════════════════════════════════\n")

	p.writeControlNoScroll(sb.String())
}

// Set thinking level command implementation

func (p *PiInterpreter) setThinkingLevel(level string) error {
	level = strings.ToLower(strings.TrimSpace(level))
	validLevels := map[string]bool{
		"off":     true,
		"minimal": true,
		"low":     true,
		"medium":  true,
		"high":    true,
		"xhigh":   true,
	}

	if !validLevels[level] {
		return fmt.Errorf("invalid thinking level (use off|minimal|low|medium|high|xhigh)")
	}

	cmd := map[string]interface{}{
		"type":  "set_thinking_level",
		"level": level,
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal set_thinking_level: %w", err)
	}
	data = append(data, '\n')

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stdin == nil {
		return fmt.Errorf("pi stdin not available")
	}

	if _, err := p.stdin.Write(data); err != nil {
		return fmt.Errorf("write to pi: %w", err)
	}

	return nil
}

func (p *PiInterpreter) handleSetThinkingLevelResponse(line string) {
	var resp rpcSetThinkingLevelResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return
	}

	if !resp.Success {
		msg := "pi: set_thinking_level failed"
		if resp.Error != "" {
			msg = fmt.Sprintf("pi: set_thinking_level failed: %s", resp.Error)
		}
		p.writeControl(msg)
		return
	}

	p.writeControl("pi: thinking level updated")
}

// Get messages command implementation

func (p *PiInterpreter) getMessages() error {
	cmd := map[string]interface{}{
		"type": "get_messages",
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal get_messages: %w", err)
	}
	data = append(data, '\n')

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stdin == nil {
		return fmt.Errorf("pi stdin not available")
	}

	if _, err := p.stdin.Write(data); err != nil {
		return fmt.Errorf("write to pi: %w", err)
	}

	return nil
}

func (p *PiInterpreter) handleGetMessagesResponse(line string) {
	var resp rpcGetMessagesResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		p.writeControl(fmt.Sprintf("error parsing get_messages response: %v", err))
		return
	}

	if !resp.Success {
		msg := "pi: get_messages failed"
		if resp.Error != "" {
			msg = fmt.Sprintf("pi: get_messages failed: %s", resp.Error)
		}
		p.writeControl(msg)
		return
	}

	messages := resp.Data.Messages
	if len(messages) == 0 {
		p.writeControl("No messages in session")
		return
	}

	var sb strings.Builder
	sb.WriteString("═════════════════════════════════════\n")
	sb.WriteString(fmt.Sprintf("Messages History (%d messages)\n", len(messages)))
	sb.WriteString("═════════════════════════════════════\n\n")

	for i, msg := range messages {
		// Parse message to get role and content
		var msgObj struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(msg, &msgObj); err != nil {
			sb.WriteString(fmt.Sprintf("[%d] (parse error)\n", i+1))
			continue
		}

		sb.WriteString(fmt.Sprintf("[%d] %s\n", i+1, msgObj.Role))

		// Try to parse content
		var contentStr string
		if msgObj.Content[0] == '"' {
			// Simple string content
			if err := json.Unmarshal(msgObj.Content, &contentStr); err == nil {
				// Truncate long content
				if len(contentStr) > 200 {
					contentStr = contentStr[:200] + "..."
				}
				sb.WriteString(fmt.Sprintf("    %s\n", contentStr))
			}
		} else {
			// Structured content (tool calls, etc.)
			var contentMap map[string]interface{}
			if err := json.Unmarshal(msgObj.Content, &contentMap); err == nil {
				if toolName, ok := contentMap["name"].(string); ok {
					sb.WriteString(fmt.Sprintf("    tool: %s\n", toolName))
				} else {
					sb.WriteString(fmt.Sprintf("    (complex content, %d bytes)\n", len(msgObj.Content)))
				}
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("═════════════════════════════════════\n")
	p.writeControlNoScroll(sb.String())
}

// Show usage command implementation

func (p *PiInterpreter) showUsage() error {
	cmd := map[string]interface{}{
		"type": "get_session_stats",
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal get_session_stats: %w", err)
	}
	data = append(data, '\n')

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stdin == nil {
		return fmt.Errorf("pi stdin not available")
	}

	if _, err := p.stdin.Write(data); err != nil {
		return fmt.Errorf("write to pi: %w", err)
	}

	return nil
}

func (p *PiInterpreter) handleGetSessionStatsResponse(line string) {
	var resp rpcGetSessionStatsResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		p.writeControl(fmt.Sprintf("error parsing get_session_stats response: %v", err))
		return
	}

	if !resp.Success {
		msg := "pi: get_session_stats failed"
		if resp.Error != "" {
			msg = fmt.Sprintf("pi: get_session_stats failed: %s", resp.Error)
		}
		p.writeControl(msg)
		return
	}

	stats := resp.Data

	var sb strings.Builder
	sb.WriteString("═════════════════════════════════════\n")
	sb.WriteString("Session Usage\n")
	sb.WriteString("═════════════════════════════════════\n\n")

	sb.WriteString("Session Info:\n")
	sb.WriteString(fmt.Sprintf("  Session ID: %s\n", stats.SessionID))
	if stats.SessionFile != "" {
		sb.WriteString(fmt.Sprintf("  Session File: %s\n", stats.SessionFile))
	}
	sb.WriteString("\n")

	sb.WriteString("Message Counts:\n")
	sb.WriteString(fmt.Sprintf("  User messages: %d\n", stats.UserMessages))
	sb.WriteString(fmt.Sprintf("  Assistant messages: %d\n", stats.AssistantMessages))
	sb.WriteString(fmt.Sprintf("  Tool calls: %d\n", stats.ToolCalls))
	sb.WriteString(fmt.Sprintf("  Tool results: %d\n", stats.ToolResults))
	sb.WriteString(fmt.Sprintf("  Total messages: %d\n", stats.TotalMessages))
	sb.WriteString("\n")

	sb.WriteString("Token Usage:\n")
	sb.WriteString(fmt.Sprintf("  Input tokens: %d\n", stats.Tokens.Input))
	sb.WriteString(fmt.Sprintf("  Output tokens: %d\n", stats.Tokens.Output))
	sb.WriteString(fmt.Sprintf("  Cache read: %d\n", stats.Tokens.CacheRead))
	sb.WriteString(fmt.Sprintf("  Cache write: %d\n", stats.Tokens.CacheWrite))
	sb.WriteString(fmt.Sprintf("  Total tokens: %d\n", stats.Tokens.Total))
	sb.WriteString("\n")

	sb.WriteString(fmt.Sprintf("Estimated cost: $%.4f\n", stats.Cost))
	sb.WriteString("\n═════════════════════════════════════\n")

	p.writeControlNoScroll(sb.String())
}

// Set auto-compaction command implementation

func (p *PiInterpreter) setAutoCompaction(arg string) error {
	arg = strings.ToLower(strings.TrimSpace(arg))
	var enabled bool

	switch arg {
	case "on", "true", "1", "yes":
		enabled = true
	case "off", "false", "0", "no":
		enabled = false
	default:
		return fmt.Errorf("invalid argument (use on|off)")
	}

	cmd := map[string]interface{}{
		"type":    "set_auto_compaction",
		"enabled": enabled,
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal set_auto_compaction: %w", err)
	}
	data = append(data, '\n')

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stdin == nil {
		return fmt.Errorf("pi stdin not available")
	}

	if _, err := p.stdin.Write(data); err != nil {
		return fmt.Errorf("write to pi: %w", err)
	}

	return nil
}

func (p *PiInterpreter) handleSetAutoCompactionResponse(line string) {
	var resp rpcSetAutoCompactionResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return
	}

	if !resp.Success {
		msg := "pi: set_auto_compaction failed"
		if resp.Error != "" {
			msg = fmt.Sprintf("pi: set_auto_compaction failed: %s", resp.Error)
		}
		p.writeControl(msg)
		return
	}

	p.writeControl("pi: auto-compaction updated")
}
