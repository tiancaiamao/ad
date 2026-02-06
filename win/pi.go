package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

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

	go p.readStdout(childCtx)
	go p.readStderr(childCtx)

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

	if _, err := p.stdin.Write(data); err != nil {
		return fmt.Errorf("write to pi: %w", err)
	}
	return nil
}

func (p *PiInterpreter) Process(ctx context.Context, input string) error {
	return p.SendInput(input)
}

func (p *PiInterpreter) SetOutputWriter(writer repl.OutputWriter) {
	p.BaseInterpreter.SetOutputWriter(writer)
}

func (p *PiInterpreter) HandleControl(input string) (bool, error) {
	fields := strings.Fields(input)
	if len(fields) == 0 || fields[0] != ":win" {
		return false, nil
	}

	if len(fields) == 1 || fields[1] == "help" {
		p.writeControlNoScroll("win: commands: thinking|tools|prefix [on|off|toggle], status, quit, models, model [id|num], model-select")
		return true, nil
	}

	switch fields[1] {
	case "thinking":
		if err := p.toggleSetting("thinking", fields[2:]); err != nil {
			p.writeControlNoScroll(err.Error())
		}
		return true, nil
	case "tools":
		if err := p.toggleSetting("tools", fields[2:]); err != nil {
			p.writeControlNoScroll(err.Error())
		}
		return true, nil
	case "prefix":
		if err := p.toggleSetting("prefix", fields[2:]); err != nil {
			p.writeControlNoScroll(err.Error())
		}
		return true, nil
	case "status":
		p.showStatus()
		return true, nil
	case "quit":
		p.writeControlNoScroll("win: exiting...")
		return true, fmt.Errorf("quit requested")
	case "models":
		return true, p.showAvailableModels()
	case "model":
		if len(fields) == 2 {
			// :win model without args - show usage
			p.writeControlNoScroll("Usage: :win model <id|number> to set model")
			return true, nil
		}
		return true, p.setModelFromInput(fields[2])
	case "model-select":
		// input is the selected text from visual mode
		return true, p.handleModelSelectInput(input)
	default:
		p.writeControlNoScroll("win: unknown command")
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
			// Log to debug file instead of stderr to avoid ad capturing it
			// TODO: add proper logger
		}
		if err != nil {
			if err == io.EOF {
				return
			}
			return
		}
	}
}

func (p *PiInterpreter) handleLine(line string) {
	if line == "" {
		return
	}

	var env rpcEnvelope
	if err := json.Unmarshal([]byte(line), &env); err != nil {
		// Silently skip malformed JSON lines
		return
	}

	switch env.Type {
	case "response":
		p.handleResponse(line)
	case "agent_start":
		p.setStreaming(true)
	case "agent_end":
		p.setStreaming(false)
	case "turn_start":
		p.setStreaming(true)
	case "turn_end":
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
	}
}

func (p *PiInterpreter) handleResponse(line string) {
	// Parse the basic response first
	var resp rpcResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return
	}

	if !resp.Success {
		msg := fmt.Sprintf("pi: %s failed", resp.Command)
		if resp.Error != "" {
			msg = fmt.Sprintf("pi: %s failed: %s", resp.Command, resp.Error)
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
	_ = writer.ScrollToBottom()
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

// Model management functions

func (p *PiInterpreter) showStatus() {
	p.writeControlNoScroll("=== win display settings ===")
	p.writeControlNoScroll(fmt.Sprintf("thinking: %v (show AI thinking)", p.getShowThinking()))
	p.writeControlNoScroll(fmt.Sprintf("tools: %v (show full tool output, tool calls always shown)", p.getShowTools()))
	p.writeControlNoScroll(fmt.Sprintf("prefix: %v (show 'assistant:', 'thinking:', 'tool:xxx:' labels)", p.getShowPrefixes()))

	p.stateMu.Lock()
	if p.currentModelID != "" || p.currentModelProvider != "" {
		p.writeControlNoScroll(fmt.Sprintf("model: %s", formatModelRef(p.currentModelProvider, p.currentModelID)))
	} else {
		p.writeControlNoScroll("model: (not set)")
	}
	p.stateMu.Unlock()

	p.writeControlNoScroll("=============================")
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
			return fmt.Errorf("model not found: %s (run :win models or use provider/model-id)", input)
		}
		return fmt.Errorf("model not found: %s (use :win models to list available models)", input)
	}

	return p.setModel(provider, modelID)
}

func (p *PiInterpreter) handleModelSelectInput(input string) error {
	// Input is from send-to-win after visual selection
	// Format could be:
	// - "N: provider/model-id  - Model Name [current]" (full line)
	// - "provider/model-id" (just the ID)
	// - "N" (just the number)

	input = strings.TrimSpace(input)
	const prefix = ":win model-select"
	if strings.HasPrefix(input, prefix) {
		input = strings.TrimSpace(strings.TrimPrefix(input, prefix))
	}

	return p.setModelFromInput(input)
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
	sb.WriteString("  - Or type: :win model <number|provider/model-id>\n\n")

	p.writeControlNoScroll(sb.String())
}
