package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/rain/pi-client/internal/pi"
)

func testApp() (*app, *session) {
	a := newApp()
	s := newSession(a, "/tmp/project", "")
	a.sessions = []*session{s}
	a.active = 0
	return a, s
}

func TestInlineRuns(t *testing.T) {
	runs := inlineRuns("a **b** and `c` and *d*")
	var text string
	var bold, code, italic bool
	for _, r := range runs {
		text += r.text
		if r.weight == 700 {
			bold = true
		}
		if r.code {
			code = true
		}
		if r.italic {
			italic = true
		}
	}
	if text != "a b and c and d" {
		t.Errorf("text = %q from %#v", text, runs)
	}
	if !bold || !code || !italic {
		t.Errorf("styles bold=%v code=%v italic=%v", bold, code, italic)
	}
}

func TestInlineRunsLinks(t *testing.T) {
	runs := inlineRuns("see [the guide](https://example.com) and https://pi.dev")
	var links []string
	for _, r := range runs {
		if r.url != "" {
			links = append(links, r.text+" -> "+r.url)
		}
	}
	want := []string{"the guide -> https://example.com", "https://pi.dev -> https://pi.dev"}
	if !reflect.DeepEqual(links, want) {
		t.Errorf("links = %#v, want %#v", links, want)
	}
}

func TestImageLine(t *testing.T) {
	alt, src, ok := imageLine(`![a cat](cat.png "title")`)
	if !ok || alt != "a cat" || src != "cat.png" {
		t.Fatalf("imageLine = %q %q %v", alt, src, ok)
	}
	if _, _, ok := imageLine("see ![a cat](cat.png)"); ok {
		t.Fatal("imageLine matched a line with other text")
	}
}

func TestSettingsViewRenders(t *testing.T) {
	a, _ := testApp()
	tt := ui.NewTester(a.settingsView, 560, 520)
	for _, tab := range []int{settingsGeneral, settingsModel, settingsAppearance} {
		a.settingsTab = tab
		tt.Frame()
	}
}

func TestRenderInlineWithLink(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) {
		renderInline(c, customTheme(true), "see [the guide](https://example.com) now")
	}, 400, 80)
	tt.Frame()
}

func TestRenderMarkdownImage(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pic.png"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	a, _ := testApp()
	a.workdir = dir
	tt := ui.NewTester(func(c *ui.Context) {
		a.renderMarkdown(c, customTheme(true), "before\n\n![pic](pic.png)\n\nafter")
	}, 500, 300)
	tt.Frame()
	if len(a.mdImages) == 0 {
		t.Fatal("markdown image was not loaded")
	}
}

func TestComposerEnterSends(t *testing.T) {
	a, s := testApp()
	s.ready = true
	s.focusComposer = true
	tt := ui.NewTester(a.view, 900, 700)
	tt.Frame()
	tt.Type("hello")
	if s.draft != "hello" {
		t.Fatalf("draft after typing = %q", s.draft)
	}
	tt.Key(ui.Shift, ui.KeyEnter)
	if !strings.Contains(s.draft, "\n") {
		t.Fatalf("shift+enter draft = %q", s.draft)
	}
	if s.connErr != "" {
		t.Fatalf("shift+enter sent a message: %q", s.connErr)
	}
	before := s.draft
	tt.Key(0, ui.KeyEnter)
	if s.draft != before {
		t.Errorf("enter changed the draft: %q -> %q", before, s.draft)
	}
	if s.connErr == "" {
		t.Errorf("enter did not try to send")
	}
}

func TestMarkdownBlocks(t *testing.T) {
	if level, ok := setextUnderline("==="); !ok || level != 1 {
		t.Errorf("setext: level=%d ok=%v", level, ok)
	}
	header, rows, _, ok := tableAt([]string{"| a | b |", "| - | - |", "| 1 | 2 |"}, 0)
	if !ok || len(header) != 2 || len(rows) != 1 || rows[0][1] != "2" {
		t.Errorf("table: header=%v rows=%v ok=%v", header, rows, ok)
	}
	if checked, isTask, rest := parseTask("[x] done"); !isTask || !checked || rest != "done" {
		t.Errorf("task: %v %v %q", checked, isTask, rest)
	}
	if got := joinParagraph([]string{"a  ", "b"}); got != "a\nb" {
		t.Errorf("hard break = %q", got)
	}
}

func TestStreamingEventApplication(t *testing.T) {
	_, s := testApp()

	s.applyEvent(pi.Event{Type: "message_start", Message: &pi.Message{Role: "user", Content: json.RawMessage(`"do it"`)}})
	s.applyEvent(pi.Event{Type: "message_start", Message: &pi.Message{Role: "assistant"}})
	s.applyEvent(pi.Event{Type: "message_update", AssistantMessageEvent: &pi.AssistantMessageEvent{Type: "text_start", ContentIndex: 0}})
	s.applyEvent(pi.Event{Type: "message_update", AssistantMessageEvent: &pi.AssistantMessageEvent{Type: "text_delta", ContentIndex: 0, Delta: "Hel"}})
	s.applyEvent(pi.Event{Type: "message_update", AssistantMessageEvent: &pi.AssistantMessageEvent{Type: "text_delta", ContentIndex: 0, Delta: "lo"}})
	s.applyEvent(pi.Event{Type: "message_update", AssistantMessageEvent: &pi.AssistantMessageEvent{
		Type: "toolcall_start", ContentIndex: 1, ID: "t1", ToolName: "bash",
	}})
	s.applyEvent(pi.Event{Type: "message_update", AssistantMessageEvent: &pi.AssistantMessageEvent{
		Type: "toolcall_end", ContentIndex: 1, ToolCall: &pi.ToolCall{ID: "t1", Name: "bash", Arguments: json.RawMessage(`{"command":"ls"}`)},
	}})

	res, _ := json.Marshal(pi.ToolResult{Content: []pi.ContentBlock{{Type: "text", Text: "file.txt"}}})
	s.applyEvent(pi.Event{Type: "tool_execution_end", ToolCallID: "t1", Result: res})

	if len(s.entries) != 2 {
		t.Fatalf("entries = %d", len(s.entries))
	}
	if s.entries[0].kind != kindUser || s.entries[0].text != "do it" {
		t.Errorf("user entry = %#v", s.entries[0])
	}
	as := s.entries[1]
	if as.kind != kindAssistant || len(as.blocks) != 2 {
		t.Fatalf("assistant = %#v", as)
	}
	if as.blocks[0].text != "Hello" {
		t.Errorf("text block = %q", as.blocks[0].text)
	}
	if as.blocks[1].name != "bash" || as.blocks[1].pending || as.blocks[1].result != "file.txt" {
		t.Errorf("tool block = %#v", as.blocks[1])
	}
}

func TestRunFolding(t *testing.T) {
	_, s := testApp()
	s.applyEvent(pi.Event{Type: "agent_start"})
	s.applyEvent(pi.Event{Type: "message_start", Message: &pi.Message{Role: "user", Content: json.RawMessage(`"hi"`)}})
	s.applyEvent(pi.Event{Type: "message_start", Message: &pi.Message{Role: "assistant"}})
	s.applyEvent(pi.Event{Type: "message_update", AssistantMessageEvent: &pi.AssistantMessageEvent{Type: "thinking_start", ContentIndex: 0}})
	s.applyEvent(pi.Event{Type: "message_update", AssistantMessageEvent: &pi.AssistantMessageEvent{Type: "thinking_delta", ContentIndex: 0, Delta: "thought"}})
	s.applyEvent(pi.Event{Type: "message_update", AssistantMessageEvent: &pi.AssistantMessageEvent{Type: "toolcall_start", ContentIndex: 1, ID: "t1", ToolName: "bash"}})
	s.applyEvent(pi.Event{Type: "message_update", AssistantMessageEvent: &pi.AssistantMessageEvent{
		Type: "toolcall_end", ContentIndex: 1, ToolCall: &pi.ToolCall{ID: "t1", Name: "bash", Arguments: json.RawMessage(`{"command":"ls"}`)},
	}})
	// A second assistant turn in the same run, holding the answer text.
	s.applyEvent(pi.Event{Type: "message_start", Message: &pi.Message{Role: "assistant"}})
	s.applyEvent(pi.Event{Type: "message_update", AssistantMessageEvent: &pi.AssistantMessageEvent{Type: "text_start", ContentIndex: 0}})
	s.applyEvent(pi.Event{Type: "message_update", AssistantMessageEvent: &pi.AssistantMessageEvent{Type: "text_delta", ContentIndex: 0, Delta: "done"}})
	time.Sleep(2 * time.Millisecond)
	s.applyEvent(pi.Event{Type: "agent_settled"})

	if len(s.entries) != 2 {
		t.Fatalf("entries = %d", len(s.entries))
	}
	run := s.entries[1]
	if run.kind != kindAssistant || len(run.blocks) != 3 {
		t.Fatalf("run = %#v", run)
	}
	if run.blocks[0].kind != blockThinking || run.blocks[1].kind != blockTool || run.blocks[2].kind != blockText {
		t.Errorf("block kinds = %d %d %d", run.blocks[0].kind, run.blocks[1].kind, run.blocks[2].kind)
	}
	if run.blocks[2].text != "done" || run.blocks[1].name != "bash" {
		t.Errorf("blocks = %#v", run.blocks)
	}
	if !hasDetails(run) || run.durationMs <= 0 || run.streaming {
		t.Errorf("fold state: details=%v duration=%d streaming=%v", hasDetails(run), run.durationMs, run.streaming)
	}
}

func TestAddFiles(t *testing.T) {
	_, s := testApp()
	s.addFiles([]string{"/tmp/notes.txt", "/tmp/a b.txt"})
	if len(s.attach) != 2 {
		t.Fatalf("attachments = %d", len(s.attach))
	}
	if s.attach[0].path != "/tmp/notes.txt" || s.attach[1].path != "/tmp/a b.txt" {
		t.Errorf("paths = %q %q", s.attach[0].path, s.attach[1].path)
	}
	if !isImageFile("x.PNG") || isImageFile("x.txt") {
		t.Error("isImageFile")
	}
}

func TestSendFileOnly(t *testing.T) {
	_, s := testApp()
	s.attach = []*attachment{{name: "x.txt", path: "/tmp/x.txt"}}
	if s.send() {
		t.Error("send should fail without a client")
	}
	if s.connErr == "" {
		t.Error("a file-only prompt should pass the empty check")
	}
}

func TestMCPParse(t *testing.T) {
	var res struct {
		Servers []mcpServer `json:"servers"`
	}
	data := `{"servers":[{"name":"fs","state":"connected","tools":["read","write"]}],"errors":[]}`
	if err := json.Unmarshal([]byte(data), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Servers) != 1 || len(res.Servers[0].Tools) != 2 || res.Servers[0].Tools[0] != "read" {
		t.Errorf("parsed %#v", res.Servers)
	}
}

func TestSlash(t *testing.T) {
	if q, ok := slashQuery("/co"); !ok || q != "co" {
		t.Errorf("query = %q, %v", q, ok)
	}
	if _, ok := slashQuery("/compact now"); ok {
		t.Error("a command with arguments is not a query")
	}
	if _, ok := slashQuery("hello"); ok {
		t.Error("a plain message is not a query")
	}
	_, s := testApp()
	found := false
	for _, c := range s.slashMatches("ba") {
		if c.name == "bash" {
			found = true
		}
	}
	if !found {
		t.Errorf("bash not among the matches")
	}
}

func TestDeriveTitle(t *testing.T) {
	_, s := testApp()
	s.loadMessages([]pi.Message{{Role: "user", Content: json.RawMessage(`"Fix the login bug"`)}})
	s.deriveTitle()
	if s.title != "Fix the login bug" {
		t.Errorf("title = %q", s.title)
	}
	s.state.SessionName = "My session"
	s.deriveTitle()
	if s.title != "My session" {
		t.Errorf("title with a Pi name = %q", s.title)
	}
}

func TestLoadMessagesFromHistory(t *testing.T) {
	_, s := testApp()
	msgs := []pi.Message{
		{Role: "user", Content: json.RawMessage(`"hi"`)},
		{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"hello"},{"type":"toolCall","id":"c1","name":"read","arguments":{"path":"a.txt"}}]`)},
		{Role: "toolResult", ToolCallID: "c1", ToolName: "read", Content: json.RawMessage(`[{"type":"text","text":"contents"}]`)},
	}
	s.loadMessages(msgs)
	if len(s.entries) != 2 {
		t.Fatalf("entries = %d", len(s.entries))
	}
	block := s.entries[1].blocks[1]
	if block.name != "read" || block.result != "contents" || block.pending {
		t.Errorf("tool block = %#v", block)
	}
}

func TestViewBuilds(t *testing.T) {
	a, s := testApp()
	s.ready = true
	s.entries = []*entry{
		{id: "1", kind: kindUser, text: "Hello Pi"},
		{id: "2", kind: kindAssistant, blocks: []tblock{
			{kind: blockText, started: true, text: "Hi there!"},
			{kind: blockTool, started: true, callID: "c1", name: "bash", args: `{"command":"ls"}`, result: "a\nb"},
		}},
	}
	tt := ui.NewTester(a.view, 1000, 780)
	tt.Frame()
	if !tt.HasText("Hello Pi") {
		t.Errorf("user text missing; texts: %q", tt.Texts())
	}
	if !tt.HasText("Hi there!") {
		t.Errorf("assistant text missing; texts: %q", tt.Texts())
	}
}

func TestFirstLineAndHumanInt(t *testing.T) {
	if got := firstLine("one\ntwo"); got != "one" {
		t.Errorf("firstLine = %q", got)
	}
	if got := humanInt(1500); got != "1.5k" {
		t.Errorf("humanInt = %q", got)
	}
}

func TestPiSource(t *testing.T) {
	t.Setenv("PI_BIN", "")

	dir := t.TempDir()
	binDir := filepath.Join(dir, "pi")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(binDir, "pi")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "package.json"), []byte(`{"version":"9.9.9"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	mygo.App.SetPath(mygo.PathResources, dir)
	defer mygo.App.SetPath(mygo.PathResources, "")

	a, _ := testApp()

	a.settings.PiSource = piSourceSystem
	a.applyPiSettings()
	if a.piPath != "pi" {
		t.Errorf("system piPath = %q, want \"pi\"", a.piPath)
	}

	a.settings.PiSource = piSourceBundled
	a.applyPiSettings()
	if a.piPath != bin {
		t.Errorf("bundled piPath = %q, want %q", a.piPath, bin)
	}

	a.settings.PiSource = piSourceAuto
	a.applyPiSettings()
	if a.piPath != bin {
		t.Errorf("auto piPath = %q, want bundled %q", a.piPath, bin)
	}

	a.settings.PiBin = "/custom/pi"
	a.applyPiSettings()
	if a.piPath != "/custom/pi" {
		t.Errorf("custom piPath = %q, want /custom/pi", a.piPath)
	}

	if got := bundledPiVersion(); got != "9.9.9" {
		t.Errorf("bundledPiVersion = %q, want 9.9.9", got)
	}
}

func TestProviders(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", dir)

	a, _ := testApp()
	a.loadProviderRows()
	if a.providerErr != "" {
		t.Fatalf("providerErr = %q", a.providerErr)
	}
	if len(a.providerRows) != 0 {
		t.Fatalf("rows = %d, want none", len(a.providerRows))
	}

	// Add a built-in provider, type a key, save it to auth.json.
	a.addProvider("anthropic")
	if len(a.providerRows) != 1 || !a.providerRows[0].fresh {
		t.Fatalf("addProvider did not add a fresh row: %#v", a.providerRows)
	}
	a.providerRows[0].draftKey = "sk-test-123"
	if err := a.saveProviders(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("auth.json mode = %v, want 0600", info.Mode().Perm())
	}
	a.loadProviderRows()
	if len(a.providerRows) != 1 || a.providerRows[0].id != "anthropic" || a.providerRows[0].fresh {
		t.Fatalf("after reload: %#v", a.providerRows)
	}

	// Add a custom endpoint: it goes to models.json.
	a.addProvider(customMarker)
	last := a.providerRows[len(a.providerRows)-1]
	if !last.custom || !last.fresh {
		t.Fatalf("custom row = %#v", last)
	}
	last.id = "ollama"
	last.baseURL = "http://localhost:11434/v1"
	last.api = "openai-completions"
	last.models = []*modelRow{{id: "qwen2.5-coder:7b"}, {id: "llama3"}}
	if err := a.saveProviders(); err != nil {
		t.Fatal(err)
	}
	custom, err := readCustomProviders()
	if err != nil {
		t.Fatal(err)
	}
	p, ok := custom["ollama"]
	if !ok {
		t.Fatalf("ollama not written: %#v", custom)
	}
	if p.BaseURL != "http://localhost:11434/v1" || len(p.Models) != 2 {
		t.Errorf("custom = %#v", p)
	}

	// Metadata of an existing model entry survives a save.
	rich := `{"providers":{"ollama":{"baseUrl":"http://localhost:11434/v1","models":[{"id":"qwen2.5-coder:7b","name":"Qwen","cost":{"input":1}}]}}}`
	if err := os.WriteFile(filepath.Join(dir, "models.json"), []byte(rich), 0o600); err != nil {
		t.Fatal(err)
	}
	a.providerRows = nil
	a.loadProviderRows()
	if err := a.saveProviders(); err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Providers map[string]struct {
			Models []map[string]any `json:"models"`
		} `json:"providers"`
	}
	data, err := os.ReadFile(filepath.Join(dir, "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if ms := saved.Providers["ollama"].Models; len(ms) != 1 || ms[0]["name"] != "Qwen" {
		t.Errorf("model metadata lost: %#v", ms)
	}

	// An OAuth entry Pine does not understand survives and keeps its kind.
	if err := os.WriteFile(filepath.Join(dir, "auth.json"),
		[]byte(`{"radius":{"type":"oauth","refresh":"r","access":"a"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	a.providerRows = nil
	a.loadProviderRows()
	var radius *providerRow
	for _, r := range a.providerRows {
		if r.id == "radius" {
			radius = r
		}
	}
	if radius == nil || !radius.oauth {
		t.Fatalf("radius oauth row = %#v", radius)
	}
	if err := a.saveProviders(); err != nil {
		t.Fatal(err)
	}
	stored, err := readCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if got := credentialKind(stored["radius"]); got != "oauth" {
		t.Errorf("oauth entry lost: kind = %q", got)
	}

	// Removing the built-in credential deletes it.
	if err := clearAuthRow("anthropic"); err != nil {
		t.Fatal(err)
	}
	stored, err = readCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := stored["anthropic"]; ok {
		t.Error("anthropic was not cleared")
	}
}

func TestProviderRowsRender(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	models := `{"providers":{"ollama":{"name":"Ollama","api":"openai-completions","baseUrl":"http://localhost:11434/v1","models":[{"id":"qwen2.5-coder:7b","contextWindow":32000}]}}}`
	if err := os.WriteFile(filepath.Join(dir, "models.json"), []byte(models), 0o600); err != nil {
		t.Fatal(err)
	}

	a, _ := testApp()
	a.loadProviderRows()
	a.addProvider("anthropic")
	a.addProvider(customMarker)
	for _, r := range a.providerRows {
		r.expanded = true
	}

	a.settingsTab = settingsModel
	tt := ui.NewTester(a.settingsView, 560, 720)
	tt.Frame()
	if a.providerErr != "" {
		t.Fatalf("providerErr = %q", a.providerErr)
	}
	if len(a.providerRows) != 3 {
		t.Fatalf("rows = %d, want 3", len(a.providerRows))
	}

	// A fetched list fills context windows when the endpoint reports them.
	r := &providerRow{models: []*modelRow{{id: "qwen2.5-coder:7b"}}}
	r.mergeModels([]fetchedModel{
		{id: "qwen2.5-coder:7b", context: 32000, maxTokens: 8000},
		{id: "llama3", context: 128000},
	})
	if len(r.models) != 2 {
		t.Fatalf("merged %d models, want 2", len(r.models))
	}
	if m := r.models[0]; m.context != "32000" || m.maxTokens != "8000" {
		t.Errorf("existing model = %#v", m)
	}
	if m := r.models[1]; m.id != "llama3" || m.context != "128000" {
		t.Errorf("new model = %#v", m)
	}
}

func TestHighlightLine(t *testing.T) {
	sc := syntaxColors(true)
	spans := highlightLine("func main() { x := 1 } // hi", "go", sc)
	var kw, comment, num bool
	for _, s := range spans {
		if s.Color == sc.keyword {
			kw = true
		}
		if s.Color == sc.comment {
			comment = true
		}
		if s.Color == sc.number {
			num = true
		}
	}
	if !kw || !comment || !num {
		t.Errorf("highlight missed: kw=%v comment=%v num=%v spans=%#v", kw, comment, num, spans)
	}
}
