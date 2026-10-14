package pi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

func base64Encode(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// Options configures Start.
type Options struct {
	// Path is the pi executable; "" looks up "pi" on PATH.
	Path string
	// Dir is the working directory Pi runs in.
	Dir string
	// Args are extra arguments after "--mode rpc".
	Args []string
	// Env replaces the process environment when set.
	Env []string

	// OnEvent receives session events, on the client's reader goroutine.
	OnEvent func(Event)
	// OnExtensionUI receives extension UI requests, on the reader goroutine.
	OnExtensionUI func(ExtensionUIRequest)
	// OnStderr receives complete lines of Pi's stderr diagnostics.
	OnStderr func(line string)
	// OnExit is called once when Pi exits or its streams end.
	OnExit func(err error)
}

// Client is a long-lived Pi RPC subprocess. Its methods are safe for
// concurrent use.
type Client struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser

	writeMu sync.Mutex
	idSeq   atomic.Int64

	pendingMu sync.Mutex
	pending   map[string]chan response

	opts Options

	closed     atomic.Bool
	finishOnce sync.Once
	done       chan struct{}

	// exitErr is the error Pi exited with, when known.
	exitMu  sync.Mutex
	exitErr error
}

// Start launches pi in RPC mode and returns a connected client.
func Start(opts Options) (*Client, error) {
	resolved, err := Resolve(opts.Path)
	if err != nil {
		return nil, err
	}
	args := append([]string{"--mode", "rpc"}, opts.Args...)
	cmd := exec.Command(resolved, args...)
	cmd.Env = childEnv(opts.Env, filepath.Dir(resolved))
	if opts.Dir != "" {
		cmd.Dir = opts.Dir
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := &Client{
		cmd:     cmd,
		stdin:   stdin,
		opts:    opts,
		pending: make(map[string]chan response),
		done:    make(chan struct{}),
	}
	go c.readLoop(stdout)
	if opts.OnStderr != nil {
		go c.stderrLoop(stderr)
	} else {
		go io.Copy(io.Discard, stderr)
	}
	go c.waitLoop()
	return c, nil
}

// Probe runs `pi --version` and returns its output, to check a path before
// starting a session.
func Probe(ctx context.Context, path string) (string, error) {
	resolved, err := Resolve(path)
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, resolved, "--version")
	cmd.Env = childEnv(nil, filepath.Dir(resolved))
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (c *Client) readLoop(r io.Reader) {
	br := bufio.NewReaderSize(r, 1<<16)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			c.handleLine(bytes.TrimRight(line, "\r\n"))
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				c.finish(err)
			} else {
				c.finish(nil)
			}
			return
		}
	}
}

func (c *Client) stderrLoop(r io.Reader) {
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			c.opts.OnStderr(strings.TrimRight(string(line), "\r\n"))
		}
		if err != nil {
			return
		}
	}
}

func (c *Client) waitLoop() {
	err := c.cmd.Wait()
	c.exitMu.Lock()
	c.exitErr = err
	c.exitMu.Unlock()
	c.finish(err)
}

func (c *Client) handleLine(line []byte) {
	if len(line) == 0 {
		return
	}
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(line, &head); err != nil {
		return
	}
	switch head.Type {
	case "response":
		var resp response
		if err := json.Unmarshal(line, &resp); err != nil {
			return
		}
		if resp.ID == "" {
			return
		}
		c.pendingMu.Lock()
		ch := c.pending[resp.ID]
		delete(c.pending, resp.ID)
		c.pendingMu.Unlock()
		if ch != nil {
			ch <- resp
		}
	case "extension_ui_request":
		var req ExtensionUIRequest
		if err := json.Unmarshal(line, &req); err != nil {
			return
		}
		if c.opts.OnExtensionUI != nil {
			c.opts.OnExtensionUI(req)
		}
	default:
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			return
		}
		ev.Raw = append(json.RawMessage(nil), line...)
		if c.opts.OnEvent != nil {
			c.opts.OnEvent(ev)
		}
	}
}

// finish marks the client closed exactly once.
func (c *Client) finish(err error) {
	c.finishOnce.Do(func() {
		c.closed.Store(true)
		c.pendingMu.Lock()
		for id, ch := range c.pending {
			delete(c.pending, id)
			close(ch)
		}
		c.pendingMu.Unlock()
		close(c.done)
		if c.opts.OnExit != nil {
			c.opts.OnExit(err)
		}
	})
}

// Done is closed when Pi exits.
func (c *Client) Done() <-chan struct{} { return c.done }

// Exited reports whether the process has ended.
func (c *Client) Exited() bool { return c.closed.Load() }

// writeJSON writes one JSON record, framed with a newline.
func (c *Client) writeJSON(v any) error {
	if c.closed.Load() {
		return errors.New("pi: client is closed")
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

const defaultTimeout = 60 * time.Second

// do sends a command and waits for its response data.
func (c *Client) do(ctx context.Context, typ string, extra map[string]any) (json.RawMessage, error) {
	if c.closed.Load() {
		return nil, errors.New("pi: client is closed")
	}
	id := fmt.Sprintf("req-%d", c.idSeq.Add(1))
	cmd := make(map[string]any, len(extra)+2)
	for k, v := range extra {
		cmd[k] = v
	}
	cmd["id"] = id
	cmd["type"] = typ

	ch := make(chan response, 1)
	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()

	if err := c.writeJSON(cmd); err != nil {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, err
	}

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
	}

	select {
	case <-ctx.Done():
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, ctx.Err()
	case <-c.done:
		return nil, errors.New("pi: process exited")
	case resp, ok := <-ch:
		if !ok {
			return nil, errors.New("pi: process exited")
		}
		if !resp.Success {
			return nil, &Error{Command: resp.Command, Message: resp.Error}
		}
		return resp.Data, nil
	}
}

func decodeInto(data json.RawMessage, v any) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	return json.Unmarshal(data, v)
}

// Prompt sends a user prompt. behavior is "", "steer" or "followUp";
// steer/followUp are needed while Pi is streaming.
func (c *Client) Prompt(ctx context.Context, message string, images []Image, behavior string) (string, error) {
	extra := map[string]any{"message": message}
	if len(images) > 0 {
		extra["images"] = images
	}
	if behavior != "" {
		extra["streamingBehavior"] = behavior
	}
	data, err := c.do(ctx, "prompt", extra)
	if err != nil {
		return "", err
	}
	var d struct {
		Disposition string `json:"disposition"`
	}
	_ = decodeInto(data, &d)
	return d.Disposition, nil
}

// Steer queues a steering message during a run.
func (c *Client) Steer(ctx context.Context, message string, images []Image) error {
	extra := map[string]any{"message": message}
	if len(images) > 0 {
		extra["images"] = images
	}
	_, err := c.do(ctx, "steer", extra)
	return err
}

// FollowUp queues a message for after the agent stops.
func (c *Client) FollowUp(ctx context.Context, message string, images []Image) error {
	extra := map[string]any{"message": message}
	if len(images) > 0 {
		extra["images"] = images
	}
	_, err := c.do(ctx, "follow_up", extra)
	return err
}

// Abort stops the current operation.
func (c *Client) Abort(ctx context.Context) error {
	_, err := c.do(ctx, "abort", nil)
	return err
}

// ClearQueue removes queued steering and follow-up messages.
func (c *Client) ClearQueue(ctx context.Context) (steering, followUp []string, err error) {
	data, err := c.do(ctx, "clear_queue", nil)
	if err != nil {
		return nil, nil, err
	}
	var d struct {
		Steering []string `json:"steering"`
		FollowUp []string `json:"followUp"`
	}
	_ = decodeInto(data, &d)
	return d.Steering, d.FollowUp, nil
}

// NewSession starts a fresh session.
func (c *Client) NewSession(ctx context.Context) (bool, error) {
	data, err := c.do(ctx, "new_session", nil)
	if err != nil {
		return false, err
	}
	var d struct {
		Cancelled bool `json:"cancelled"`
	}
	_ = decodeInto(data, &d)
	return d.Cancelled, nil
}

// GetState returns the current session state.
func (c *Client) GetState(ctx context.Context) (State, error) {
	var s State
	data, err := c.do(ctx, "get_state", nil)
	if err != nil {
		return s, err
	}
	err = decodeInto(data, &s)
	return s, err
}

// GetMessages returns all messages in the conversation.
func (c *Client) GetMessages(ctx context.Context) ([]Message, error) {
	data, err := c.do(ctx, "get_messages", nil)
	if err != nil {
		return nil, err
	}
	var d struct {
		Messages []Message `json:"messages"`
	}
	_ = decodeInto(data, &d)
	return d.Messages, nil
}

// SetModel switches to a specific model.
func (c *Client) SetModel(ctx context.Context, provider, modelID string) (Model, error) {
	var m Model
	data, err := c.do(ctx, "set_model", map[string]any{"provider": provider, "modelId": modelID})
	if err != nil {
		return m, err
	}
	err = decodeInto(data, &m)
	return m, err
}

// CycleModel switches to the next available model.
func (c *Client) CycleModel(ctx context.Context) (*Model, string, error) {
	data, err := c.do(ctx, "cycle_model", nil)
	if err != nil {
		return nil, "", err
	}
	var d struct {
		Model         *Model `json:"model"`
		ThinkingLevel string `json:"thinkingLevel"`
	}
	_ = decodeInto(data, &d)
	return d.Model, d.ThinkingLevel, nil
}

// GetAvailableModels lists the configured models.
func (c *Client) GetAvailableModels(ctx context.Context) ([]Model, error) {
	data, err := c.do(ctx, "get_available_models", nil)
	if err != nil {
		return nil, err
	}
	var d struct {
		Models []Model `json:"models"`
	}
	_ = decodeInto(data, &d)
	return d.Models, nil
}

// SetThinkingLevel sets the reasoning level.
func (c *Client) SetThinkingLevel(ctx context.Context, level string) error {
	_, err := c.do(ctx, "set_thinking_level", map[string]any{"level": level})
	return err
}

// CycleThinkingLevel cycles the reasoning level.
func (c *Client) CycleThinkingLevel(ctx context.Context) (string, error) {
	data, err := c.do(ctx, "cycle_thinking_level", nil)
	if err != nil {
		return "", err
	}
	var d struct {
		Level string `json:"level"`
	}
	_ = decodeInto(data, &d)
	return d.Level, nil
}

// GetAvailableThinkingLevels lists the levels the current model supports.
func (c *Client) GetAvailableThinkingLevels(ctx context.Context) ([]string, error) {
	data, err := c.do(ctx, "get_available_thinking_levels", nil)
	if err != nil {
		return nil, err
	}
	var d struct {
		Levels []string `json:"levels"`
	}
	_ = decodeInto(data, &d)
	return d.Levels, nil
}

// SetSteeringMode sets how steering messages are delivered.
func (c *Client) SetSteeringMode(ctx context.Context, mode string) error {
	_, err := c.do(ctx, "set_steering_mode", map[string]any{"mode": mode})
	return err
}

// SetFollowUpMode sets how follow-up messages are delivered.
func (c *Client) SetFollowUpMode(ctx context.Context, mode string) error {
	_, err := c.do(ctx, "set_follow_up_mode", map[string]any{"mode": mode})
	return err
}

// Compact manually compacts the conversation context.
func (c *Client) Compact(ctx context.Context, customInstructions string) (string, error) {
	extra := map[string]any{}
	if customInstructions != "" {
		extra["customInstructions"] = customInstructions
	}
	data, err := c.do(ctx, "compact", extra)
	if err != nil {
		return "", err
	}
	var d struct {
		Summary string `json:"summary"`
	}
	_ = decodeInto(data, &d)
	return d.Summary, nil
}

// SetAutoCompaction enables or disables automatic compaction.
func (c *Client) SetAutoCompaction(ctx context.Context, enabled bool) error {
	_, err := c.do(ctx, "set_auto_compaction", map[string]any{"enabled": enabled})
	return err
}

// SetAutoRetry enables or disables automatic retry.
func (c *Client) SetAutoRetry(ctx context.Context, enabled bool) error {
	_, err := c.do(ctx, "set_auto_retry", map[string]any{"enabled": enabled})
	return err
}

// AbortRetry aborts an in-progress retry.
func (c *Client) AbortRetry(ctx context.Context) error {
	_, err := c.do(ctx, "abort_retry", nil)
	return err
}

// Bash executes a shell command through Pi and returns its output.
func (c *Client) Bash(ctx context.Context, command string, excludeFromContext bool) (string, int, error) {
	extra := map[string]any{"command": command}
	if excludeFromContext {
		extra["excludeFromContext"] = true
	}
	data, err := c.do(ctx, "bash", extra)
	if err != nil {
		return "", 0, err
	}
	var d struct {
		Output   string `json:"output"`
		ExitCode int    `json:"exitCode"`
	}
	_ = decodeInto(data, &d)
	return d.Output, d.ExitCode, nil
}

// AbortBash aborts a running bash command.
func (c *Client) AbortBash(ctx context.Context) error {
	_, err := c.do(ctx, "abort_bash", nil)
	return err
}

// GetSessionStats returns token and cost statistics.
func (c *Client) GetSessionStats(ctx context.Context) (SessionStats, error) {
	var s SessionStats
	data, err := c.do(ctx, "get_session_stats", nil)
	if err != nil {
		return s, err
	}
	err = decodeInto(data, &s)
	return s, err
}

// ExportHTML writes the session as HTML and returns the path.
func (c *Client) ExportHTML(ctx context.Context, outputPath string) (string, error) {
	extra := map[string]any{}
	if outputPath != "" {
		extra["outputPath"] = outputPath
	}
	data, err := c.do(ctx, "export_html", extra)
	if err != nil {
		return "", err
	}
	var d struct {
		Path string `json:"path"`
	}
	_ = decodeInto(data, &d)
	return d.Path, nil
}

// SwitchSession loads another session file.
func (c *Client) SwitchSession(ctx context.Context, sessionPath string) (bool, error) {
	data, err := c.do(ctx, "switch_session", map[string]any{"sessionPath": sessionPath})
	if err != nil {
		return false, err
	}
	var d struct {
		Cancelled bool `json:"cancelled"`
	}
	_ = decodeInto(data, &d)
	return d.Cancelled, nil
}

// Fork creates a new fork from a previous user message.
func (c *Client) Fork(ctx context.Context, entryID string) (string, bool, error) {
	data, err := c.do(ctx, "fork", map[string]any{"entryId": entryID})
	if err != nil {
		return "", false, err
	}
	var d struct {
		Text      string `json:"text"`
		Cancelled bool   `json:"cancelled"`
	}
	_ = decodeInto(data, &d)
	return d.Text, d.Cancelled, nil
}

// Clone duplicates the active branch into a new session.
func (c *Client) Clone(ctx context.Context) (bool, error) {
	data, err := c.do(ctx, "clone", nil)
	if err != nil {
		return false, err
	}
	var d struct {
		Cancelled bool `json:"cancelled"`
	}
	_ = decodeInto(data, &d)
	return d.Cancelled, nil
}

// GetForkMessages lists user messages available for forking.
func (c *Client) GetForkMessages(ctx context.Context) ([]ForkMessage, error) {
	data, err := c.do(ctx, "get_fork_messages", nil)
	if err != nil {
		return nil, err
	}
	var d struct {
		Messages []ForkMessage `json:"messages"`
	}
	_ = decodeInto(data, &d)
	return d.Messages, nil
}

// GetLastAssistantText returns the text of the last assistant message.
func (c *Client) GetLastAssistantText(ctx context.Context) (string, error) {
	data, err := c.do(ctx, "get_last_assistant_text", nil)
	if err != nil {
		return "", err
	}
	var d struct {
		Text string `json:"text"`
	}
	_ = decodeInto(data, &d)
	return d.Text, nil
}

// SetSessionName sets the session's display name.
func (c *Client) SetSessionName(ctx context.Context, name string) error {
	_, err := c.do(ctx, "set_session_name", map[string]any{"name": name})
	return err
}

// GetCommands lists discoverable commands.
func (c *Client) GetCommands(ctx context.Context) ([]Command, error) {
	data, err := c.do(ctx, "get_commands", nil)
	if err != nil {
		return nil, err
	}
	var d struct {
		Commands []Command `json:"commands"`
	}
	_ = decodeInto(data, &d)
	return d.Commands, nil
}

// RespondExtensionUI answers an extension dialog request.
func (c *Client) RespondExtensionUI(id string, value map[string]any) error {
	rec := map[string]any{"type": "extension_ui_response", "id": id}
	for k, v := range value {
		rec[k] = v
	}
	return c.writeJSON(rec)
}

// Close closes Pi's stdin for an orderly shutdown and waits for it to exit.
func (c *Client) Close() error {
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	select {
	case <-c.done:
		return nil
	case <-time.After(5 * time.Second):
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
		<-c.done
		return nil
	}
}
