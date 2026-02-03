package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/sminez/ad/win/pkg/ad"
)

const (
	DEFAULT_WINDOW_NAME = "+win"
	PROMPT              = "> "
)

// EchoInterpreter 是一个简单的解释器
// 每次调用时启动新进程，而不是保持持久连接
type EchoInterpreter struct {
	lineNumber int
}

func NewEchoInterpreter() (*EchoInterpreter, error) {
	return &EchoInterpreter{
		lineNumber: 0,
	}, nil
}

func (e *EchoInterpreter) Process(input string) (string, error) {
	// 每次启动新进程处理输入
	e.lineNumber++

	// 使用 awk 给输入加上行号
	cmd := exec.Command("awk", fmt.Sprintf("{ print \"%d\\t\" $0 }", e.lineNumber))

	cmd.Stdin = strings.NewReader(input)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("interpreter run failed: %w", err)
	}

	output := stdout.String()
	if output == "" {
		output = input // 如果 awk 没输出，直接返回输入
	}

	return output, nil
}

func (e *EchoInterpreter) Close() error {
	return nil
}

type EchoReplHandler struct {
	bufferID    string
	interpreter *EchoInterpreter
	client      *ad.Client
}

func main() {
	windowName := flag.String("name", DEFAULT_WINDOW_NAME, "window name")
	debug := flag.Bool("debug", false, "enable debug logging")
	flag.Parse()

	if *debug {
		log.SetFlags(log.Ltime | log.Lshortfile)
		log.Println("Starting echo-repl with debug logging")
	}

	client, err := ad.NewClient()
	if err != nil {
		log.Fatalf("unable to connect to ad: %v", err)
	}
	defer func() {
		if *debug {
			log.Println("Closing client connection")
		}
		client.Close()
	}()

	if *debug {
		log.Println("Connected to ad successfully")
	}

	bufferID, err := client.OpenInNewWindow(*windowName)
	if err != nil {
		log.Fatalf("unable to create window: %v", err)
	}

	if *debug {
		log.Printf("Created window %s with buffer ID: %s\n", *windowName, bufferID)
	}

	startEchoREPL(client, bufferID, *debug)
}

func startEchoREPL(client *ad.Client, bufferID string, debug bool) {
	// 打开日志文件
	logFile, err := os.OpenFile("/tmp/echo-repl.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		log.Fatalf("unable to open log file: %v", err)
	}
	defer logFile.Close()

	log.SetOutput(logFile)
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	log.Println("=== Echo REPL Starting ===")
	log.Printf("Buffer ID: %s\n", bufferID)

	// 初始化 buffer 内容
	welcome := `# Echo REPL
#
# 这是一个简单的 REPL 示例，展示如何将 ad 与外部解释器整合
#
# 工作流程：
# 1. 在任意 buffer 中选中文本（dot）
# 2. 按 C-t 将选中的文本发送到这里
# 3. 解释器会处理输入并返回结果（加行号）
#
# 或者直接在这里输入内容，按回车执行
#
` + PROMPT

	if err := client.WriteBody(bufferID, welcome); err != nil {
		log.Fatalf("unable to initialize window: %v", err)
	}
	if err := client.WriteAddr(bufferID, "$"); err != nil {
		log.Fatalf("unable to set address: %v", err)
	}

	log.Println("Creating interpreter...")
	interpreter, err := NewEchoInterpreter()
	if err != nil {
		log.Fatalf("unable to create interpreter: %v", err)
	}
	defer interpreter.Close()

	log.Println("Interpreter created successfully")

	handler := &EchoReplHandler{
		bufferID:    bufferID,
		interpreter: interpreter,
		client:      client,
	}

	log.Println("Ready to process events")

	if err := client.RunEventFilter(bufferID, handler); err != nil {
		log.Fatalf("event filter died: %v", err)
	}
}

func (h *EchoReplHandler) HandleInsert(source string, from, to int, txt string, client *ad.Client) error {
	log.Printf("HandleInsert: source=%s, from=%d, to=%d, txt=%q", source, from, to, txt)

	if err := client.MarkClean(h.bufferID); err != nil {
		log.Printf("HandleInsert: MarkClean error: %v", err)
		return fmt.Errorf("mark-clean failed: %w", err)
	}

	// 如果是来自 send-to-echo 脚本的命令（以 > 开头）
	if source == "F" && strings.HasPrefix(txt, PROMPT) {
		// 提取命令并执行
		input := strings.TrimPrefix(txt, PROMPT)
		input = strings.TrimSpace(input)
		// 去掉末尾的换行符
		input = strings.TrimSuffix(input, "\n")
		if input != "" {
			log.Printf("HandleInsert: executing script command: %q", input)
			return h.executeCommand(input, true) // 来自 send-to-echo，需要居中视口
		}
	}

	// 用户按回车时，发送最后一行给解释器
	if source == "K" && txt == "\n" {
		log.Println("HandleInsert: user pressed Enter")

		// 读取 buffer body 的最后一部分
		// 只读取最后 200 个字符，足够包含最后一行
		body, err := h.client.ReadFile("buffers/" + h.bufferID + "/body")
		if err != nil {
			log.Printf("HandleInsert: ReadFile error: %v", err)
			return fmt.Errorf("read-body failed: %w", err)
		}

		// 只保留最后 200 字符以提高效率
		if len(body) > 200 {
			body = body[len(body)-200:]
		}

		log.Printf("HandleInsert: body tail: %q", body)

		// 找到最后一个换行符（用户按回车插入的）
		lastNewline := strings.LastIndex(body, "\n")
		if lastNewline == -1 {
			// buffer 只有一行
			input := strings.TrimPrefix(body, PROMPT)
			input = strings.TrimSpace(input)
			log.Printf("HandleInsert: extracted input: %q", input)
			if input != "" {
				return h.executeCommand(input, false) // 手动输入，不需要居中视口
			}
			return client.MarkClean(h.bufferID)
		}

		// 找到倒数第二个换行符
		prevNewline := strings.LastIndex(body[:lastNewline], "\n")
		var lastLine string
		if prevNewline == -1 {
			lastLine = body[:lastNewline]
		} else {
			lastLine = body[prevNewline+1 : lastNewline]
		}

		log.Printf("HandleInsert: last line: %q", lastLine)

		input := strings.TrimPrefix(lastLine, PROMPT)
		input = strings.TrimSpace(input)

		log.Printf("HandleInsert: extracted input: %q", input)

		if input != "" {
			return h.executeCommand(input, false) // 手动输入，不需要居中视口
		}
	}

	// 其他情况（解释器输出），确保光标在末尾
	// 这是 executeCommand 写入输出后触发的，我们需要确保光标在正确的位置
	return client.WriteAddr(h.bufferID, "$")
}

func (h *EchoReplHandler) HandleDelete(source string, from, to int, client *ad.Client) error {
	return client.MarkClean(h.bufferID)
}

func (h *EchoReplHandler) HandleExecute(source string, from, to int, txt string, client *ad.Client) error {
	input := strings.TrimSpace(txt)

	log.Printf("HandleExecute: source=%s, from=%d, to=%d, txt=%q, input=%q", source, from, to, txt, input)

	// 只添加换行，然后发送给解释器
	if err := client.AppendToBody(h.bufferID, "\n"); err != nil {
		log.Printf("HandleExecute: AppendToBody error: %v", err)
		return fmt.Errorf("append-to-body failed: %w", err)
	}

	return h.executeCommand(input, true) // Execute 事件需要居中视口
}

func (h *EchoReplHandler) executeCommand(input string, centerViewport bool) error {
	log.Printf("executeCommand: processing input: %q, centerViewport=%v", input, centerViewport)

	// 调用解释器处理输入
	output, err := h.interpreter.Process(input)
	if err != nil {
		log.Printf("executeCommand: interpreter error: %v", err)
		return fmt.Errorf("interpreter error: %w", err)
	}

	log.Printf("executeCommand: interpreter output: %q", output)

	// 一次性写入输出和提示符（减少事件触发次数）
	fullOutput := output + "\n" + PROMPT
	if err := h.client.AppendToBody(h.bufferID, fullOutput); err != nil {
		log.Printf("executeCommand: AppendToBody error: %v", err)
		return fmt.Errorf("append-to-body failed: %w", err)
	}

	// 移动光标到末尾
	if err := h.client.WriteAddr(h.bufferID, "$"); err != nil {
		log.Printf("executeCommand: WriteAddr error: %v", err)
		return fmt.Errorf("write-addr failed: %w", err)
	}

	// 如果需要居中视口（从其他 buffer 执行命令）
	if centerViewport {
		log.Printf("executeCommand: centering viewport for buffer %s", h.bufferID)
		if err := h.client.CenterViewport(h.bufferID); err != nil {
			log.Printf("executeCommand: CenterViewport error: %v", err)
			return fmt.Errorf("center-viewport failed: %w", err)
		}
	} else {
		// 手动输入的情况：请求焦点以刷新光标位置
		log.Printf("executeCommand: requesting focus for buffer %s", h.bufferID)
		if err := h.client.FocusBuffer(h.bufferID); err != nil {
			log.Printf("executeCommand: FocusBuffer error: %v", err)
			return fmt.Errorf("focus-buffer failed: %w", err)
		}
	}

	log.Println("executeCommand: completed successfully")
	return nil
}
