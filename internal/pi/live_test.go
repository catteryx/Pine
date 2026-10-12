package pi

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLiveRPC drives a real Pi process. It runs only when PI_LIVE=1 and
// PI_BIN point at a pi executable, so the default `go test ./...` stays
// hermetic.
func TestLiveRPC(t *testing.T) {
	if os.Getenv("PI_LIVE") != "1" {
		t.Skip("set PI_LIVE=1 and PI_BIN to run against a real pi")
	}
	bin := os.Getenv("PI_BIN")
	if bin == "" {
		t.Fatal("PI_BIN is not set")
	}

	events := make(chan Event, 128)
	exited := make(chan struct{})
	c, err := Start(Options{
		Path: bin,
		Dir:  t.TempDir(),
		Args: []string{"--no-session"},
		OnEvent: func(ev Event) {
			select {
			case events <- ev:
			default:
			}
		},
		OnExit: func(error) { close(exited) },
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	state, err := c.GetState(ctx)
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	t.Logf("state: streaming=%v session=%q", state.IsStreaming, state.SessionID)

	models, err := c.GetAvailableModels(ctx)
	if err != nil {
		t.Fatalf("GetAvailableModels: %v", err)
	}
	t.Logf("models: %d", len(models))

	levels, err := c.GetAvailableThinkingLevels(ctx)
	if err != nil {
		t.Fatalf("GetAvailableThinkingLevels: %v", err)
	}
	t.Logf("thinking levels: %v", levels)

	cmds, err := c.GetCommands(ctx)
	if err != nil {
		t.Fatalf("GetCommands: %v", err)
	}
	t.Logf("commands: %d", len(cmds))

	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("pi did not exit after Close")
	}
}
