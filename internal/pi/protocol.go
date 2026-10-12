// Package pi talks to the Pi coding agent
// (github.com/earendil-works/pi, v1.0+) over its RPC mode: JSONL commands on
// stdin, responses and session events on stdout.
//
// Start a client with Start, send commands with the typed methods, and
// receive activity through the callbacks in Options.
package pi

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Image is an image attachment of a prompt, base64 encoded.
type Image struct {
	Type     string `json:"type"`
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
}

// NewImage returns an image content block for a prompt.
func NewImage(data []byte, mimeType string) Image {
	return Image{Type: "image", Data: base64Encode(data), MimeType: mimeType}
}

// Cost is the cost of a model call in US dollars.
type Cost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
	Total      float64 `json:"total"`
}

// Usage is the token usage of an assistant message.
type Usage struct {
	Input       int  `json:"input"`
	Output      int  `json:"output"`
	CacheRead   int  `json:"cacheRead"`
	CacheWrite  int  `json:"cacheWrite"`
	Reasoning   int  `json:"reasoning,omitempty"`
	TotalTokens int  `json:"totalTokens"`
	Cost        Cost `json:"cost"`
}

// ContentBlock is one block of a message: text, thinking, a tool call or an
// image.
type ContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Thinking string `json:"thinking,omitempty"`
	Redacted bool   `json:"redacted,omitempty"`

	// Tool call.
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`

	// Image.
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// ToolCall is a completed tool call of an assistant message.
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// ToolResult is the result of a tool execution.
type ToolResult struct {
	Content []ContentBlock  `json:"content"`
	Details json.RawMessage `json:"details,omitempty"`
	IsError bool            `json:"isError,omitempty"`
}

// Text joins the text blocks of the result.
func (r ToolResult) Text() string {
	if r.Content == nil {
		return ""
	}
	var b strings.Builder
	for _, block := range r.Content {
		if block.Type == "text" {
			b.WriteString(block.Text)
		}
	}
	return b.String()
}

// Message is an AgentMessage: a user, assistant, tool result, bash
// execution or summary message.
type Message struct {
	Role      string          `json:"role"`
	Timestamp int64           `json:"timestamp,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`

	// Assistant.
	Model        string `json:"model,omitempty"`
	Provider     string `json:"provider,omitempty"`
	StopReason   string `json:"stopReason,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`
	Usage        *Usage `json:"usage,omitempty"`

	// Tool result.
	ToolCallID string `json:"toolCallId,omitempty"`
	ToolName   string `json:"toolName,omitempty"`
	IsError    bool   `json:"isError,omitempty"`

	// Bash execution.
	Command   string `json:"command,omitempty"`
	Output    string `json:"output,omitempty"`
	ExitCode  *int   `json:"exitCode,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`

	// Summaries.
	Summary    string `json:"summary,omitempty"`
	CustomType string `json:"customType,omitempty"`
}

// Text returns the message's text: its string content, or its text blocks
// joined; "" for messages without text.
func (m Message) Text() string {
	if len(m.Content) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(m.Content, &s) == nil {
		return s
	}
	var blocks []ContentBlock
	if json.Unmarshal(m.Content, &blocks) != nil {
		return ""
	}
	var b strings.Builder
	for _, block := range blocks {
		if block.Type == "text" {
			b.WriteString(block.Text)
		}
	}
	return b.String()
}

// Blocks parses the message's content into content blocks. String content
// becomes one text block.
func (m Message) Blocks() []ContentBlock {
	if len(m.Content) == 0 {
		return nil
	}
	var s string
	if json.Unmarshal(m.Content, &s) == nil {
		return []ContentBlock{{Type: "text", Text: s}}
	}
	var blocks []ContentBlock
	_ = json.Unmarshal(m.Content, &blocks)
	return blocks
}

// Images returns the image blocks of the message.
func (m Message) Images() []Image {
	var result []Image
	for _, block := range m.Blocks() {
		if block.Type == "image" {
			result = append(result, Image{Type: "image", Data: block.Data, MimeType: block.MimeType})
		}
	}
	return result
}

// Model is a model definition as Pi reports it.
type Model struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Provider      string `json:"provider"`
	API           string `json:"api,omitempty"`
	BaseURL       string `json:"baseUrl,omitempty"`
	Reasoning     bool   `json:"reasoning,omitempty"`
	ContextWindow int    `json:"contextWindow,omitempty"`
	MaxTokens     int    `json:"maxTokens,omitempty"`
	Cost          Cost   `json:"cost,omitempty"`
}

// Label returns "provider/name" for menus.
func (m Model) Label() string {
	name := m.Name
	if name == "" {
		name = m.ID
	}
	if m.Provider != "" {
		return m.Provider + " · " + name
	}
	return name
}

// State is the session state returned by get_state.
type State struct {
	Model                 *Model `json:"model,omitempty"`
	ThinkingLevel         string `json:"thinkingLevel,omitempty"`
	IsStreaming           bool   `json:"isStreaming,omitempty"`
	IsCompacting          bool   `json:"isCompacting,omitempty"`
	SteeringMode          string `json:"steeringMode,omitempty"`
	FollowUpMode          string `json:"followUpMode,omitempty"`
	SessionFile           string `json:"sessionFile,omitempty"`
	SessionID             string `json:"sessionId,omitempty"`
	SessionName           string `json:"sessionName,omitempty"`
	AutoCompactionEnabled bool   `json:"autoCompactionEnabled,omitempty"`
	MessageCount          int    `json:"messageCount,omitempty"`
	PendingMessageCount   int    `json:"pendingMessageCount,omitempty"`
}

// ContextUsage is Pi's estimate of the current context window usage.
type ContextUsage struct {
	Tokens        *int     `json:"tokens"`
	ContextWindow int      `json:"contextWindow"`
	Percent       *float64 `json:"percent"`
}

// SessionStats is the token, cost and context usage of a session.
type SessionStats struct {
	SessionFile       string        `json:"sessionFile,omitempty"`
	SessionID         string        `json:"sessionId,omitempty"`
	UserMessages      int           `json:"userMessages,omitempty"`
	AssistantMessages int           `json:"assistantMessages,omitempty"`
	ToolCalls         int           `json:"toolCalls,omitempty"`
	ToolResults       int           `json:"toolResults,omitempty"`
	TotalMessages     int           `json:"totalMessages,omitempty"`
	Tokens            Usage2        `json:"tokens"`
	Cost              float64       `json:"cost"`
	ContextUsage      *ContextUsage `json:"contextUsage,omitempty"`
}

// Usage2 is the token totals of get_session_stats, without a nested cost.
type Usage2 struct {
	Input      int `json:"input"`
	Output     int `json:"output"`
	CacheRead  int `json:"cacheRead"`
	CacheWrite int `json:"cacheWrite"`
	Total      int `json:"total"`
}

// Command is a discoverable command (prompt template, skill or extension
// command).
type Command struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source,omitempty"`
}

// ForkMessage is a user message available for forking.
type ForkMessage struct {
	EntryID string `json:"entryId"`
	Text    string `json:"text"`
}

// AssistantMessageEvent is a streaming update of an assistant message.
type AssistantMessageEvent struct {
	Type         string    `json:"type"`
	ContentIndex int       `json:"contentIndex,omitempty"`
	Delta        string    `json:"delta,omitempty"`
	Content      string    `json:"content,omitempty"`
	ID           string    `json:"id,omitempty"`
	ToolName     string    `json:"toolName,omitempty"`
	ToolCall     *ToolCall `json:"toolCall,omitempty"`
	Reason       string    `json:"reason,omitempty"`
	Error        string    `json:"error,omitempty"`
}

// Event is a session event from stdout. Raw keeps the original record.
type Event struct {
	Type string          `json:"type"`
	Raw  json.RawMessage `json:"-"`

	Usage                 *Usage                 `json:"usage,omitempty"`
	AssistantMessageEvent *AssistantMessageEvent `json:"assistantMessageEvent,omitempty"`

	Message     *Message  `json:"message,omitempty"`
	Messages    []Message `json:"messages,omitempty"`
	ToolResults []Message `json:"toolResults,omitempty"`

	ToolCallID    string          `json:"toolCallId,omitempty"`
	ToolName      string          `json:"toolName,omitempty"`
	Args          json.RawMessage `json:"args,omitempty"`
	PartialResult json.RawMessage `json:"partialResult,omitempty"`
	Result        json.RawMessage `json:"result,omitempty"`
	IsError       bool            `json:"isError,omitempty"`
	DurationMs    *float64        `json:"durationMs,omitempty"`

	WillRetry bool `json:"willRetry,omitempty"`
	Aborted   bool `json:"aborted,omitempty"`

	Steering []string `json:"steering,omitempty"`
	FollowUp []string `json:"followUp,omitempty"`

	Name  *string `json:"name,omitempty"`
	Level string  `json:"level,omitempty"`

	Reason       string `json:"reason,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`

	Attempt     int   `json:"attempt,omitempty"`
	MaxAttempts int   `json:"maxAttempts,omitempty"`
	DelayMs     int   `json:"delayMs,omitempty"`
	Success     *bool `json:"success,omitempty"`

	ID    string `json:"id,omitempty"`
	Delta string `json:"delta,omitempty"`

	Entry json.RawMessage `json:"entry,omitempty"`

	ExtensionPath string `json:"extensionPath,omitempty"`
	Error         string `json:"error,omitempty"`
}

// ExtensionUIRequest is a request from an extension, to be answered with
// RespondExtensionUI (for select, confirm, input and editor) or ignored
// (for the fire-and-forget methods).
type ExtensionUIRequest struct {
	Type   string `json:"type"`
	ID     string `json:"id"`
	Method string `json:"method"`

	Title   string   `json:"title,omitempty"`
	Message string   `json:"message,omitempty"`
	Options []string `json:"options,omitempty"`

	Placeholder string `json:"placeholder,omitempty"`
	Prefill     string `json:"prefill,omitempty"`
	Timeout     int    `json:"timeout,omitempty"`

	NotifyType string `json:"notifyType,omitempty"`

	StatusKey  string  `json:"statusKey,omitempty"`
	StatusText *string `json:"statusText,omitempty"`

	WidgetKey       string   `json:"widgetKey,omitempty"`
	WidgetLines     []string `json:"widgetLines,omitempty"`
	WidgetPlacement string   `json:"widgetPlacement,omitempty"`

	Text string `json:"text,omitempty"`
}

// response is a command response from stdout.
type response struct {
	ID      string          `json:"id,omitempty"`
	Type    string          `json:"type"`
	Command string          `json:"command,omitempty"`
	Success bool            `json:"success"`
	Error   string          `json:"error,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Error is a failed command response.
type Error struct {
	Command string
	Message string
}

func (e *Error) Error() string {
	if e.Command == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Command, e.Message)
}
