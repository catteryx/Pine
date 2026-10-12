package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/rain/pi-client/internal/pi"
)

type entryKind int

const (
	kindUser entryKind = iota
	kindAssistant
	kindBash
	kindSummary
	kindNotice
	kindError
)

type blockKind int

const (
	blockText blockKind = iota
	blockThinking
	blockTool
)

// tblock is one rendered content block of an assistant message.
type tblock struct {
	kind      blockKind
	started   bool
	text      string
	callID    string
	name      string
	args      string
	result    string
	resultErr bool
	pending   bool
	duration  *float64
	expanded  bool
	images    []pi.Image
	thumbs    []*ui.Bitmap
}

// entry is one item of the transcript.
type entry struct {
	id        string
	kind      entryKind
	text      string
	images    []pi.Image
	thumbs    []*ui.Bitmap
	blocks    []tblock
	streaming bool
	errMsg    string

	// Bash execution.
	command  string
	output   string
	exitCode *int

	// durationMs is how long the run that produced an assistant entry took,
	// and detailsOpen whether its thinking and tool calls show.
	durationMs  int64
	detailsOpen bool

	ts int64
}

// assistantText joins an assistant entry's text blocks.
func assistantText(e *entry) string {
	var parts []string
	for i := range e.blocks {
		if e.blocks[i].kind == blockText && strings.TrimSpace(e.blocks[i].text) != "" {
			parts = append(parts, e.blocks[i].text)
		}
	}
	return strings.Join(parts, "\n\n")
}

func (e *entry) block(idx int, kind blockKind) *tblock {
	for i := len(e.blocks); i <= idx; i++ {
		e.blocks = append(e.blocks, tblock{})
	}
	b := &e.blocks[idx]
	if !b.started {
		b.kind = kind
		b.started = true
	}
	return b
}

func (s *session) nextID() string {
	s.seq++
	return fmt.Sprintf("e%d", s.seq)
}

func (s *session) addEntry(e *entry) *entry {
	s.entries = append(s.entries, e)
	return e
}

func (s *session) addNotice(text string) {
	s.addEntry(&entry{id: s.nextID(), kind: kindNotice, text: text})
}

func (s *session) addError(text string) {
	s.addEntry(&entry{id: s.nextID(), kind: kindError, text: text})
}

func (s *session) addSummary(text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	s.addEntry(&entry{id: s.nextID(), kind: kindSummary, text: text})
}

// loadMessages rebuilds the transcript from a session's message history,
// grouping each assistant run (its turns and tool results) into one entry.
func (s *session) loadMessages(msgs []pi.Message) {
	s.entries = nil
	s.byTool = map[string]*tblock{}
	s.bashByID = map[string]*entry{}
	s.streaming = nil
	s.lastUser = nil
	s.run = nil
	s.runMsgBase = 0

	var run *entry
	var firstTs, lastTs int64
	flush := func() {
		if run != nil && firstTs > 0 && lastTs > firstTs {
			run.durationMs = lastTs - firstTs
		}
		run, firstTs, lastTs = nil, 0, 0
	}
	for i := range msgs {
		m := &msgs[i]
		if m.Timestamp > 0 {
			lastTs = m.Timestamp
		}
		switch m.Role {
		case "user":
			flush()
			e := s.addEntry(&entry{id: s.nextID(), kind: kindUser, text: m.Text(), images: m.Images(), ts: m.Timestamp})
			s.attachThumbs(e)
			s.lastUser = e
		case "assistant":
			if run == nil {
				run = s.addEntry(&entry{id: s.nextID(), kind: kindAssistant, ts: m.Timestamp})
				firstTs = m.Timestamp
			}
			s.mergeBlocksAt(run, len(run.blocks), m.Blocks())
			if m.ErrorMessage != "" {
				run.errMsg = m.ErrorMessage
			}
		case "toolResult":
			s.applyToolResultMessage(m)
		case "bashExecution":
			flush()
			s.addEntry(&entry{id: s.nextID(), kind: kindBash, command: m.Command, output: m.Output, exitCode: m.ExitCode, ts: m.Timestamp})
		case "custom":
			flush()
			if t := m.Text(); strings.TrimSpace(t) != "" {
				s.addNotice(t)
			}
		case "branchSummary", "compactionSummary":
			flush()
			s.addSummary(m.Summary)
		}
	}
	flush()
}

// parseBlocks turns content blocks into transcript blocks, carrying over
// the outcome and disclosure state of tool calls already seen.
func (s *session) parseBlocks(blocks []pi.ContentBlock) []tblock {
	var out []tblock
	for _, cb := range blocks {
		switch cb.Type {
		case "text":
			out = append(out, tblock{kind: blockText, started: true, text: cb.Text})
		case "thinking":
			out = append(out, tblock{kind: blockThinking, started: true, text: cb.Thinking})
		case "toolCall":
			b := tblock{kind: blockTool, started: true, callID: cb.ID, name: cb.Name, args: prettyJSON(cb.Arguments), pending: true}
			if old := s.byTool[cb.ID]; old != nil {
				b.result = old.result
				b.resultErr = old.resultErr
				b.pending = old.pending
				b.duration = old.duration
				b.expanded = old.expanded
				b.images = old.images
				b.thumbs = old.thumbs
			}
			out = append(out, b)
		}
	}
	return out
}

// mergeBlocksAt replaces an assistant entry's blocks from start with the
// authoritative content of a completed message and re-registers its tool
// calls.
func (s *session) mergeBlocksAt(e *entry, start int, blocks []pi.ContentBlock) {
	if start > len(e.blocks) {
		start = len(e.blocks)
	}
	e.blocks = append(e.blocks[:start], s.parseBlocks(blocks)...)
	for i := range e.blocks {
		if e.blocks[i].kind == blockTool && e.blocks[i].callID != "" {
			s.byTool[e.blocks[i].callID] = &e.blocks[i]
		}
	}
}

func (s *session) applyEvent(ev pi.Event) {
	switch ev.Type {
	case "agent_start":
		s.running = true
		s.turnStart = time.Now()
		s.run = nil
		s.runMsgBase = 0
	case "agent_end":
		s.running = false
		if ev.WillRetry {
			s.addNotice("Retrying…")
		}
	case "agent_settled":
		s.running = false
		s.aborted = ev.Aborted
		if s.run != nil {
			if !s.turnStart.IsZero() {
				s.run.durationMs = time.Since(s.turnStart).Milliseconds()
			}
			s.run.streaming = false
		}
		s.turnStart = time.Time{}
		s.run = nil
		if ev.Aborted {
			s.addNotice("Aborted")
		}
		s.app.notifyDone(s)
		s.refreshStats()
	case "message_start":
		if ev.Message != nil {
			s.startMessage(ev.Message)
		}
	case "message_update":
		s.updateAssistant(ev)
	case "message_end":
		if ev.Message != nil {
			s.endMessage(ev.Message)
		}
	case "turn_end":
		if ev.Message != nil {
			s.finishAssistant(ev.Message)
		}
		for i := range ev.ToolResults {
			s.applyToolResultMessage(&ev.ToolResults[i])
		}
	case "tool_execution_start":
		s.toolStart(ev)
	case "tool_execution_update":
		s.toolUpdate(ev)
	case "tool_execution_end":
		s.toolEnd(ev)
	case "queue_update":
		s.steering = ev.Steering
		s.followUp = ev.FollowUp
	case "session_info_changed":
		if ev.Name != nil {
			s.state.SessionName = *ev.Name
		} else {
			s.state.SessionName = ""
		}
	case "thinking_level_changed":
		s.state.ThinkingLevel = ev.Level
	case "compaction_start":
		s.addNotice("Compacting context… (" + ev.Reason + ")")
	case "compaction_end":
		switch {
		case ev.Aborted:
			s.addNotice("Compaction aborted")
		case ev.ErrorMessage != "":
			s.addError("Compaction failed: " + ev.ErrorMessage)
		default:
			var d struct {
				Summary string `json:"summary"`
			}
			_ = json.Unmarshal(ev.Result, &d)
			s.addSummary(d.Summary)
		}
	case "auto_retry_start":
		s.addNotice(fmt.Sprintf("Retrying (%d/%d) in %.1fs: %s", ev.Attempt, ev.MaxAttempts, float64(ev.DelayMs)/1000, ev.ErrorMessage))
	case "auto_retry_end":
		if ev.Success != nil && !*ev.Success {
			s.addError("Retry failed: " + ev.ErrorMessage)
		}
	case "bash_execution_update":
		s.bashUpdate(ev)
	case "extension_error":
		s.addError(fmt.Sprintf("Extension error (%s): %s", ev.ExtensionPath, ev.Error))
	}
}

func (s *session) startMessage(m *pi.Message) {
	switch m.Role {
	case "user":
		e := s.addEntry(&entry{id: s.nextID(), kind: kindUser, text: m.Text(), images: m.Images(), ts: m.Timestamp})
		s.attachThumbs(e)
		s.lastUser = e
		if s.title == "New session" {
			if t := firstLine(m.Text()); t != "" {
				s.title = truncate(t, 60)
			}
		}
	case "assistant":
		if s.run == nil {
			s.run = s.addEntry(&entry{id: s.nextID(), kind: kindAssistant, streaming: true, ts: m.Timestamp})
		}
		s.run.streaming = true
		s.runMsgBase = len(s.run.blocks)
		s.streaming = s.run
		s.running = true
	}
}

func (s *session) endMessage(m *pi.Message) {
	switch m.Role {
	case "user":
		if s.lastUser != nil {
			s.lastUser.text = m.Text()
			s.lastUser.images = m.Images()
			s.attachThumbs(s.lastUser)
		}
	case "assistant":
		s.finishAssistant(m)
	case "toolResult":
		s.applyToolResultMessage(m)
	case "bashExecution":
		s.addEntry(&entry{id: s.nextID(), kind: kindBash, command: m.Command, output: m.Output, exitCode: m.ExitCode, ts: m.Timestamp})
	case "custom":
		if t := m.Text(); strings.TrimSpace(t) != "" {
			s.addNotice(t)
		}
	case "branchSummary", "compactionSummary":
		s.addSummary(m.Summary)
	}
}

func (s *session) updateAssistant(ev pi.Event) {
	if s.run == nil {
		s.run = s.addEntry(&entry{id: s.nextID(), kind: kindAssistant, streaming: true})
		s.runMsgBase = 0
	}
	e := s.run
	s.streaming = e
	base := s.runMsgBase
	ame := ev.AssistantMessageEvent
	if ame == nil {
		return
	}
	idx := base + ame.ContentIndex
	switch ame.Type {
	case "text_start":
		e.block(idx, blockText).text = ""
	case "text_delta":
		e.block(idx, blockText).text += ame.Delta
	case "text_end":
		e.block(idx, blockText).text = ame.Content
	case "thinking_start":
		e.block(idx, blockThinking)
	case "thinking_delta":
		e.block(idx, blockThinking).text += ame.Delta
	case "thinking_end":
		e.block(idx, blockThinking).text = ame.Content
	case "toolcall_start":
		b := e.block(idx, blockTool)
		b.callID = ame.ID
		b.name = ame.ToolName
		b.pending = true
		if b.callID != "" {
			s.byTool[b.callID] = b
		}
	case "toolcall_delta":
		e.block(idx, blockTool).args += ame.Delta
	case "toolcall_end":
		b := e.block(idx, blockTool)
		if ame.ToolCall != nil {
			b.callID = ame.ToolCall.ID
			b.name = ame.ToolCall.Name
			b.args = prettyJSON(ame.ToolCall.Arguments)
		}
		if b.callID != "" {
			s.byTool[b.callID] = b
		}
	case "error":
		if ame.Error != "" {
			e.errMsg = ame.Error
		}
	}
}

func (s *session) finishAssistant(m *pi.Message) {
	if s.run == nil {
		s.run = s.addEntry(&entry{id: s.nextID(), kind: kindAssistant})
		s.runMsgBase = 0
	}
	s.mergeBlocksAt(s.run, s.runMsgBase, m.Blocks())
	if m.ErrorMessage != "" {
		s.run.errMsg = m.ErrorMessage
	}
	s.streaming = nil
}

func (s *session) toolStart(ev pi.Event) {
	b := s.byTool[ev.ToolCallID]
	if b == nil {
		e := s.addEntry(&entry{id: s.nextID(), kind: kindAssistant})
		b = e.block(0, blockTool)
		b.callID = ev.ToolCallID
		b.name = ev.ToolName
		b.args = prettyJSON(ev.Args)
		s.byTool[ev.ToolCallID] = b
	}
	if b.name == "" {
		b.name = ev.ToolName
	}
	if b.args == "" {
		b.args = prettyJSON(ev.Args)
	}
	b.pending = true
}

func (s *session) toolUpdate(ev pi.Event) {
	b := s.byTool[ev.ToolCallID]
	if b == nil {
		return
	}
	var tr pi.ToolResult
	if json.Unmarshal(ev.PartialResult, &tr) == nil && len(tr.Content) > 0 {
		b.result = tr.Text()
		b.resultErr = tr.IsError
	}
}

func (s *session) toolEnd(ev pi.Event) {
	b := s.byTool[ev.ToolCallID]
	if b == nil {
		e := s.addEntry(&entry{id: s.nextID(), kind: kindAssistant})
		b = e.block(0, blockTool)
		b.callID = ev.ToolCallID
		b.name = ev.ToolName
		s.byTool[ev.ToolCallID] = b
	}
	var tr pi.ToolResult
	if json.Unmarshal(ev.Result, &tr) == nil {
		b.result = tr.Text()
		if tr.IsError {
			b.resultErr = true
		}
		s.attachToolImages(b, tr)
	}
	if ev.IsError {
		b.resultErr = true
	}
	b.pending = false
	b.duration = ev.DurationMs
}

// attachToolImages decodes the image blocks of a tool result, once.
func (s *session) attachToolImages(b *tblock, tr pi.ToolResult) {
	for _, cb := range tr.Content {
		if cb.Type == "image" {
			b.images = append(b.images, pi.Image{Type: "image", Data: cb.Data, MimeType: cb.MimeType})
		}
	}
	if len(b.thumbs) == len(b.images) {
		return
	}
	b.thumbs = nil
	for _, img := range b.images {
		if data, err := decodeBase64(img.Data); err == nil {
			if bm, err := ui.DecodeBitmap(data); err == nil {
				b.thumbs = append(b.thumbs, bm)
				continue
			}
		}
		b.thumbs = append(b.thumbs, nil)
	}
}

func (s *session) applyToolResultMessage(m *pi.Message) {
	b := s.byTool[m.ToolCallID]
	if b == nil {
		return
	}
	b.result = m.Text()
	b.resultErr = m.IsError
	b.pending = false
	s.attachToolImages(b, pi.ToolResult{Content: m.Blocks(), IsError: m.IsError})
}

func (s *session) bashUpdate(ev pi.Event) {
	e := s.bashByID[ev.ID]
	if e == nil {
		e = s.addEntry(&entry{id: s.nextID(), kind: kindBash, command: ev.ToolName})
		s.bashByID[ev.ID] = e
	}
	e.output += ev.Delta
}

// attachThumbs decodes an entry's image attachments once, for display.
func (s *session) attachThumbs(e *entry) {
	if len(e.images) == 0 || len(e.thumbs) == len(e.images) {
		return
	}
	e.thumbs = nil
	for _, img := range e.images {
		data, err := decodeBase64(img.Data)
		if err != nil {
			e.thumbs = append(e.thumbs, nil)
			continue
		}
		bm, err := ui.DecodeBitmap(data)
		if err != nil {
			e.thumbs = append(e.thumbs, nil)
			continue
		}
		e.thumbs = append(e.thumbs, bm)
	}
}

// --- helpers ---------------------------------------------------------------

// argSummary turns a tool's JSON arguments into a short, human summary for
// the collapsed tool card: the command, path, pattern or similar.
func argSummary(name, args string) string {
	if strings.TrimSpace(args) == "" {
		return ""
	}
	var m map[string]any
	if json.Unmarshal([]byte(args), &m) != nil {
		return firstLine(args)
	}
	preferred := map[string][]string{
		"bash":        {"command"},
		"read":        {"path", "file_path", "file"},
		"write":       {"path", "file_path", "file"},
		"edit":        {"path", "file_path", "file"},
		"grep":        {"pattern", "query"},
		"find":        {"pattern", "glob"},
		"ls":          {"path"},
		"codemode":    {"code", "script"},
		"tool_search": {"query"},
	}
	keys := preferred[name]
	keys = append(keys, "command", "path", "file_path", "file", "url", "query", "pattern", "prompt", "message", "name", "id")
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return compactValue(v)
		}
	}
	all := make([]string, 0, len(m))
	for k := range m {
		all = append(all, k)
	}
	sort.Strings(all)
	var parts []string
	for _, k := range all {
		parts = append(parts, k+"="+compactValue(m[k]))
		if len(parts) >= 3 {
			break
		}
	}
	return truncate(strings.Join(parts, "  "), 90)
}

func compactValue(v any) string {
	if s, ok := v.(string); ok {
		return truncate(strings.Join(strings.Fields(s), " "), 100)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return truncate(string(b), 100)
}

func prettyJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return string(raw)
	}
	return buf.String()
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 80 {
		s = s[:77] + "…"
	}
	return s
}

func humanInt(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
