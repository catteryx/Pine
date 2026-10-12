package pi

import (
	"encoding/json"
	"testing"
)

func TestMessageContent(t *testing.T) {
	var m Message
	if err := json.Unmarshal([]byte(`{"role":"user","content":"hello","timestamp":1}`), &m); err != nil {
		t.Fatal(err)
	}
	if m.Text() != "hello" {
		t.Errorf("Text() = %q", m.Text())
	}
	if len(m.Blocks()) != 1 || m.Blocks()[0].Type != "text" {
		t.Errorf("Blocks() = %#v", m.Blocks())
	}

	var am Message
	raw := `{"role":"assistant","content":[{"type":"text","text":"hi"},{"type":"toolCall","id":"c1","name":"bash","arguments":{"command":"ls"}}]}`
	if err := json.Unmarshal([]byte(raw), &am); err != nil {
		t.Fatal(err)
	}
	if am.Text() != "hi" {
		t.Errorf("assistant Text() = %q", am.Text())
	}
	blocks := am.Blocks()
	if len(blocks) != 2 || blocks[1].Name != "bash" {
		t.Fatalf("assistant blocks = %#v", blocks)
	}
	if got := string(blocks[1].Arguments); got != `{"command":"ls"}` {
		t.Errorf("arguments = %s", got)
	}
}

func TestToolResultText(t *testing.T) {
	r := ToolResult{Content: []ContentBlock{{Type: "text", Text: "a"}, {Type: "text", Text: "b"}}}
	if r.Text() != "ab" {
		t.Errorf("Text() = %q", r.Text())
	}
}

func TestHandleLineResponse(t *testing.T) {
	c := &Client{pending: map[string]chan response{}}
	ch := make(chan response, 1)
	c.pending["req-1"] = ch
	c.handleLine([]byte(`{"id":"req-1","type":"response","command":"get_state","success":true,"data":{"isStreaming":true}}`))
	select {
	case r := <-ch:
		if !r.Success || r.Command != "get_state" {
			t.Errorf("response = %#v", r)
		}
		var s State
		if err := json.Unmarshal(r.Data, &s); err != nil || !s.IsStreaming {
			t.Errorf("state = %#v err %v", s, err)
		}
	default:
		t.Fatal("response was not delivered")
	}
	if _, ok := c.pending["req-1"]; ok {
		t.Error("pending entry not removed")
	}
}

func TestHandleLineEvent(t *testing.T) {
	var got Event
	c := &Client{opts: Options{OnEvent: func(ev Event) { got = ev }}}
	c.handleLine([]byte(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"Hi"}}`))
	if got.Type != "message_update" || got.AssistantMessageEvent == nil || got.AssistantMessageEvent.Delta != "Hi" {
		t.Fatalf("event = %#v", got)
	}
	if len(got.Raw) == 0 {
		t.Error("Raw not set")
	}
}

func TestHandleLineExtensionUI(t *testing.T) {
	var got ExtensionUIRequest
	c := &Client{opts: Options{OnExtensionUI: func(r ExtensionUIRequest) { got = r }}}
	c.handleLine([]byte(`{"type":"extension_ui_request","id":"u1","method":"confirm","title":"Go?","message":"sure"}`))
	if got.ID != "u1" || got.Method != "confirm" || got.Title != "Go?" {
		t.Fatalf("request = %#v", got)
	}
}
