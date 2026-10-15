package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/rain/pi-client/internal/pi"
)

// session is one open conversation with its own Pi process. Its state is
// read by the view on the main thread and changed by its reader goroutine
// through Window.Update.
type session struct {
	app *app

	id        string
	title     string
	createdAt time.Time

	workdir    string
	resumePath string // a session file to continue, "" to start fresh

	// The current agent run: one entry across its assistant turns, with
	// runMsgBase the block offset of the assistant message streaming now.
	turnStart  time.Time
	run        *entry
	runMsgBase int

	client   atomic.Pointer[pi.Client]
	gen      atomic.Int64
	starting atomic.Bool
	stopping atomic.Bool
	closed   atomic.Bool

	// Connection.
	ready      bool
	connErr    string
	stderrTail []string

	// Transcript.
	entries   []*entry
	seq       int64
	lastUser  *entry
	streaming *entry
	byTool    map[string]*tblock
	bashByID  map[string]*entry

	running bool
	aborted bool

	// Composer.
	draft  string
	attach []*attachment

	// Pi state.
	state    pi.State
	models   []pi.Model
	levels   []string
	stats    pi.SessionStats
	commands []pi.Command

	steering []string
	followUp []string

	status map[string]string

	// UI state.
	chat          ui.ListState
	composerH     float32
	focusComposer bool
	slashIdx      int
	slashOpen     bool
	autoRetry     bool
	renaming      bool
	renameDraft   string
	pendingToast  string
	ext           *extUI
}

func newSession(a *app, workdir, resumePath string) *session {
	s := &session{
		app:        a,
		id:         fmt.Sprintf("s%d", a.nextSessionID()),
		title:      "New session",
		createdAt:  time.Now(),
		workdir:    workdir,
		resumePath: resumePath,
		byTool:     map[string]*tblock{},
		bashByID:   map[string]*entry{},
		status:     map[string]string{},
	}
	// Keep the session file known before Pi reports it, so saving the open
	// sessions never drops it.
	s.state.SessionFile = resumePath
	// A resumed session keeps the time its conversation actually started
	// and its title, so a tab that is reopened without starting Pi still
	// looks like itself.
	if t := sessionStartTime(resumePath); !t.IsZero() {
		s.createdAt = t
	}
	if resumePath != "" {
		var info sessionInfo
		readSessionMeta(resumePath, &info)
		if info.title != "" {
			s.title = info.title
		}
		if info.id != "" {
			s.state.SessionID = info.id
		}
	}
	return s
}

func (s *session) win() *mygo.Window { return s.app.win }

// deriveTitle names a session from Pi's session name, or from its first
// user message, so a resumed session keeps the name it had.
func (s *session) deriveTitle() {
	if s.state.SessionName != "" {
		s.title = s.state.SessionName
		return
	}
	if s.title != "" && s.title != "New session" {
		return
	}
	for _, e := range s.entries {
		if e.kind == kindUser {
			if t := firstLine(e.text); t != "" {
				s.title = truncate(t, 60)
				return
			}
		}
	}
}

// displayName is the sidebar and header label for this session.
func (s *session) displayName() string {
	if s.state.SessionName != "" {
		return s.state.SessionName
	}
	if s.title != "" && s.title != "New session" {
		return s.title
	}
	if s.workdir != "" {
		return filepath.Base(s.workdir)
	}
	return "Session"
}

// --- lifecycle -------------------------------------------------------------

func (s *session) connect() {
	s.stopping.Store(false)
	s.closed.Store(false)
	s.starting.Store(true)
	s.ready = false
	s.connErr = ""

	// Pi persists sessions by default (under ~/.pi/agent/sessions), so
	// opening a session is just --session <file>. No --no-session.
	args := s.app.settings.sessionArgs(s.resumePath)
	gen := s.gen.Add(1)
	opts := pi.Options{
		Path:          s.app.piPath,
		Dir:           s.workdir,
		Args:          args,
		OnEvent:       func(ev pi.Event) { s.onEvent(gen, ev) },
		OnExtensionUI: func(req pi.ExtensionUIRequest) { s.onExtension(gen, req) },
		OnStderr:      func(line string) { s.onStderr(gen, line) },
		OnExit:        func(err error) { s.onExit(gen, err) },
	}
	go func() {
		c, err := pi.Start(opts)
		if err != nil {
			s.win().Update(func() {
				if s.stale(gen) {
					return
				}
				s.starting.Store(false)
				s.connErr = err.Error() + "\nIs the `pi` CLI (v1.0+) installed and on your PATH?"
			})
			return
		}
		if s.stale(gen) {
			// Closed or suspended while Pi started: nobody adopts it.
			go c.Close()
			return
		}
		s.client.Store(c)
		s.loadInitial(gen, c)
	}()
}

func (s *session) stale(gen int64) bool {
	return s.closed.Load() || gen != s.gen.Load()
}

func (s *session) loadInitial(gen int64, c *pi.Client) {
	ctx := context.Background()
	state, err := c.GetState(ctx)
	if err != nil {
		s.win().Update(func() {
			if s.stale(gen) {
				return
			}
			s.starting.Store(false)
			s.connErr = "connected, but get_state failed: " + err.Error()
		})
		return
	}
	msgs, _ := c.GetMessages(ctx)
	models, _ := c.GetAvailableModels(ctx)
	levels, _ := c.GetAvailableThinkingLevels(ctx)
	stats, _ := c.GetSessionStats(ctx)
	cmds, _ := c.GetCommands(ctx)
	s.win().Update(func() {
		if s.stale(gen) {
			return
		}
		s.starting.Store(false)
		s.ready = true
		s.connErr = ""
		s.state = state
		// Pi reports the session file on connect; adopt its real start
		// time so the sidebar shows when the conversation began rather
		// than when the app launched.
		if t := sessionStartTime(state.SessionFile); !t.IsZero() {
			s.createdAt = t
		}
		s.models = models
		s.levels = levels
		s.stats = stats
		s.commands = cmds
		s.loadMessages(msgs)
		s.deriveTitle()
		s.app.saveState()
	})
}

// disconnect stops this session's Pi. Close waits for the process to exit,
// so it runs in the background and the window never blocks on it.
func (s *session) disconnect() {
	if c := s.client.Swap(nil); c != nil {
		s.stopping.Store(true)
		go c.Close()
		return
	}
	if s.starting.Load() {
		// Pi is still starting: make that start stale, so the client it
		// produces is closed instead of adopted.
		s.gen.Add(1)
		s.starting.Store(false)
		s.stopping.Store(true)
	}
}

// busy reports whether a task is in flight. A busy session keeps its Pi
// however long it stays off screen, so background tasks keep running.
func (s *session) busy() bool {
	return s.running || s.streaming != nil || s.starting.Load()
}

// suspend stops the Pi of a session with nothing in flight. Working
// sessions, and the one on screen, are the only ones that hold a Pi; the
// rest start again when they are shown.
func (s *session) suspend() {
	if s.busy() {
		return
	}
	s.disconnect()
}

// ensureLive starts Pi for a session that has none, continuing its session
// file when it has one.
func (s *session) ensureLive() {
	if s.getClient() != nil || s.starting.Load() {
		return
	}
	if s.state.SessionFile != "" {
		s.resumePath = s.state.SessionFile
	}
	s.connect()
}

// reconnect restarts this session's pi, so credentials or the pi runtime a
// setting changed take effect. The conversation resumes from its session
// file when it has one.
func (s *session) reconnect() {
	s.disconnect()
	if s.state.SessionFile != "" {
		s.resumePath = s.state.SessionFile
	}
	s.connect()
}

func (s *session) shutdown() {
	s.closed.Store(true)
	s.stopping.Store(true)
	if c := s.client.Swap(nil); c != nil {
		go c.Close()
	}
}

func (s *session) getClient() *pi.Client { return s.client.Load() }

// --- callbacks -------------------------------------------------------------

func (s *session) onEvent(gen int64, ev pi.Event) {
	s.win().Update(func() {
		if s.stale(gen) {
			return
		}
		s.applyEvent(ev)
	})
}

func (s *session) onExtension(gen int64, req pi.ExtensionUIRequest) {
	s.win().Update(func() {
		if s.stale(gen) {
			return
		}
		s.applyExtension(req)
	})
}

func (s *session) onStderr(gen int64, line string) {
	s.win().Update(func() {
		if s.stale(gen) {
			return
		}
		s.stderrTail = append(s.stderrTail, line)
		if len(s.stderrTail) > 50 {
			s.stderrTail = s.stderrTail[len(s.stderrTail)-50:]
		}
	})
}

func (s *session) onExit(gen int64, err error) {
	s.win().Update(func() {
		if s.stale(gen) {
			return
		}
		s.running = false
		s.ready = false
		s.streaming = nil
		if s.stopping.Load() {
			return
		}
		msg := "pi exited"
		if err != nil {
			msg = "pi exited: " + err.Error()
		}
		if len(s.stderrTail) > 0 {
			msg += " — " + s.stderrTail[len(s.stderrTail)-1]
		}
		s.connErr = msg
	})
}

// --- actions ---------------------------------------------------------------

func (s *session) send() bool {
	if s.sendOrCommand() {
		return true
	}
	text := strings.TrimSpace(s.draft)
	var images []pi.Image
	var files []string
	for _, at := range s.attach {
		switch {
		case at.image.Data != "":
			images = append(images, at.image)
		case at.path != "":
			files = append(files, quotePath(at.path))
		}
	}
	if len(files) > 0 {
		if text != "" {
			text += " "
		}
		text += strings.Join(files, " ")
	}
	if text == "" && len(images) == 0 {
		return false
	}
	c := s.getClient()
	if c == nil || c.Exited() {
		s.connErr = "pi is not running"
		return false
	}
	behavior := ""
	if s.running {
		behavior = "steer"
	}
	s.draft = ""
	s.attach = nil
	go func() {
		if _, err := c.Prompt(context.Background(), text, images, behavior); err != nil {
			s.win().Update(func() { s.addError("prompt: " + err.Error()) })
		}
	}()
	return true
}

func (s *session) stop() {
	if c := s.getClient(); c != nil {
		go func() { _ = c.Abort(context.Background()) }()
	}
}

func (s *session) resetSession() {
	c := s.getClient()
	if c == nil {
		return
	}
	go func() {
		if _, err := c.NewSession(context.Background()); err != nil {
			s.win().Update(func() { s.addError("new session: " + err.Error()) })
			return
		}
		s.reloadSession()
	}()
}

func (s *session) compact() {
	c := s.getClient()
	if c == nil {
		return
	}
	go func() {
		if _, err := c.Compact(context.Background(), ""); err != nil {
			s.win().Update(func() { s.addError("compact: " + err.Error()) })
		}
	}()
}

func (s *session) exportHTML() {
	c := s.getClient()
	if c == nil {
		return
	}
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent:      s.win(),
		Title:       "Export session as HTML",
		Filters:     []mygo.FileFilter{{Name: "HTML", Extensions: []string{"html"}}},
		ButtonLabel: "Export",
	})
	if err != nil || path == "" {
		return
	}
	go func() {
		out, err := c.ExportHTML(context.Background(), path)
		s.win().Update(func() {
			if err != nil {
				s.addError("export: " + err.Error())
				return
			}
			s.pendingToast = "Exported to " + out
		})
	}()
}

func (s *session) reloadSession() {
	c := s.getClient()
	if c == nil {
		return
	}
	ctx := context.Background()
	state, err := c.GetState(ctx)
	if err != nil {
		return
	}
	msgs, _ := c.GetMessages(ctx)
	stats, _ := c.GetSessionStats(ctx)
	levels, _ := c.GetAvailableThinkingLevels(ctx)
	s.win().Update(func() {
		s.state = state
		s.stats = stats
		s.levels = levels
		s.loadMessages(msgs)
		s.deriveTitle()
	})
}

func (s *session) refreshStats() {
	c := s.getClient()
	if c == nil {
		return
	}
	go func() {
		stats, err := c.GetSessionStats(context.Background())
		if err != nil {
			return
		}
		state, _ := c.GetState(context.Background())
		s.win().Update(func() {
			s.stats = stats
			if state.SessionID != "" {
				s.state = state
			}
		})
	}()
}

func (s *session) setModel(mo pi.Model) {
	c := s.getClient()
	if c == nil {
		return
	}
	go func() {
		ctx := context.Background()
		if _, err := c.SetModel(ctx, mo.Provider, mo.ID); err != nil {
			s.win().Update(func() { s.addError("set model: " + err.Error()) })
			return
		}
		state, _ := c.GetState(ctx)
		levels, _ := c.GetAvailableThinkingLevels(ctx)
		s.win().Update(func() {
			s.state = state
			s.levels = levels
			// Remember the choice so new sessions start with it too.
			s.rememberModel(mo.Provider, mo.ID)
		})
	}()
}

// rememberModel saves the chosen model as the default for new sessions.
func (s *session) rememberModel(provider, id string) {
	s.app.settings.Provider = provider
	s.app.settings.Model = id
	s.app.saveState()
}

func (s *session) setThinking(level string) {
	c := s.getClient()
	if c == nil {
		return
	}
	go func() {
		if err := c.SetThinkingLevel(context.Background(), level); err != nil {
			s.win().Update(func() { s.addError("thinking level: " + err.Error()) })
			return
		}
		s.win().Update(func() {
			s.state.ThinkingLevel = level
			s.app.settings.Thinking = level
			s.app.saveState()
		})
	}()
}

func (s *session) pickFiles() {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:   s.win(),
		Title:    "Add files",
		Multiple: true,
	})
	if err != nil || len(paths) == 0 {
		return
	}
	s.win().Update(func() { s.addFiles(paths) })
}

// addFiles stages files to send with the next prompt: images as images,
// other files as their path. Main thread only.
func (s *session) addFiles(paths []string) {
	for _, p := range paths {
		if isImageFile(p) {
			if at, err := loadAttachment(p); err == nil {
				s.attach = append(s.attach, &at)
			}
			continue
		}
		at := &attachment{name: filepath.Base(p), path: p}
		s.attach = append(s.attach, at)
		go s.loadIcon(at)
	}
	s.focusComposer = true
}

// loadIcon loads a file's native icon, on the main thread, and shows it
// when it is ready.
func (s *session) loadIcon(at *attachment) {
	if s.win() == nil {
		return
	}
	s.win().Update(func() { at.icon = s.app.fileIcon(at.path) })
}

func isImageFile(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return true
	}
	return false
}

func quotePath(p string) string {
	return "'" + strings.ReplaceAll(p, "'", `'\''`) + "'"
}

func (s *session) applyExtension(req pi.ExtensionUIRequest) {
	switch req.Method {
	case "notify":
		s.pendingToast = req.Message
	case "setStatus":
		if req.StatusText == nil || *req.StatusText == "" {
			delete(s.status, req.StatusKey)
		} else {
			s.status[req.StatusKey] = *req.StatusText
		}
	case "setTitle":
		if s.win() != nil {
			s.win().SetTitle(req.Title)
		}
	case "set_editor_text":
		s.draft = req.Text
		s.focusComposer = true
	case "setWidget":
	case "select", "confirm", "input", "editor":
		value := ""
		if req.Method == "editor" {
			value = req.Prefill
		}
		s.ext = &extUI{req: req, value: value}
	}
}

func (s *session) respondExt(data map[string]any) {
	if s.ext == nil {
		return
	}
	id := s.ext.req.ID
	s.ext = nil
	c := s.getClient()
	if c == nil {
		return
	}
	go func() { _ = c.RespondExtensionUI(id, data) }()
}

// --- status text -----------------------------------------------------------

func (s *session) statusText() string {
	if s.running {
		if s.streaming != nil {
			return "Streaming…"
		}
		return "Working…"
	}
	if s.state.SessionID != "" {
		return "session " + shortID(s.state.SessionID)
	}
	return "Ready"
}

// contextPercent returns how full the context window is, when known.
func (s *session) contextPercent() (float64, bool) {
	if u := s.stats.ContextUsage; u != nil && u.Percent != nil {
		return *u.Percent, true
	}
	return 0, false
}

func (s *session) emptyHint() string {
	if s.starting.Load() {
		return "Starting Pi…"
	}
	if s.connErr != "" {
		return s.connErr
	}
	return "Ask Pi to read, write, edit and run code.\nType below and press ⏎ to send."
}

// changeDir picks a new working directory for an empty session and
// reconnects Pi there.
func (s *session) changeDir() {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent: s.win(), Title: "Choose a project folder", Directory: true, Multiple: false,
	})
	if err != nil || len(paths) == 0 {
		return
	}
	dir := paths[0]
	s.win().Update(func() {
		s.disconnect()
		s.workdir = dir
		s.connect()
		s.app.saveState()
	})
}

// --- helpers ---------------------------------------------------------------

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// loadAttachment reads an image file and stages it for the next prompt.
func loadAttachment(path string) (attachment, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return attachment{}, err
	}
	mime := mimeForFile(path)
	at := attachment{
		name:  filepath.Base(path),
		mime:  mime,
		image: pi.Image{Type: "image", Data: encodeBase64(data), MimeType: mime},
	}
	if bm, err := ui.DecodeBitmap(data); err == nil {
		at.bitmap = bm
	}
	return at, nil
}

func mimeForFile(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	default:
		return "application/octet-stream"
	}
}
