package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/rain/pi-client/internal/pi"
)

// app owns the window, the open sessions and the sidebar.
type app struct {
	win     *mygo.Window
	piPath  string
	workdir string

	sessions []*session
	active   int
	seq      int64

	theme            *ui.Theme
	search           string
	sidebarCollapsed bool
	sidebarW         float32
	showInfo         bool
	page             int // 0 the active session, 1 MCP, 2 skills
	mcpServers       []mcpServer
	mcpErrors        []string
	mcpLoading       bool
	mcpLoaded        bool
	mcpErr           string
	settingsWin      *mygo.Window
	settings         settings
	settingsTab      int
	providerRows     []*providerRow
	providerLoaded   bool
	providerErr      string
	addChoice        string
	focused          bool
	unread           int
	preview          *ui.Bitmap // an image shown in the preview overlay
	previewName      string

	// folders is whether each working-directory group in the sidebar is
	// expanded, by directory.
	folders map[string]*bool

	// fileIcons caches native file icons by extension.
	fileIcons map[string]*ui.Bitmap
	iconMu    sync.Mutex

	// markdown images, cached by source: decoded from a file or fetched from
	// a URL once.
	mdImages   map[string]*ui.Bitmap
	mdLoading  map[string]bool
	mdImagesMu sync.Mutex

	// Saved-session picker.
	showSaved     bool
	saved         []sessionInfo
	savedList     ui.ListState
	savedSelected int
	loadingSaved  bool
	savedErr      string
}

// attachment is an image or a file staged to send with the next prompt.
// An image is sent as an image; a file contributes its path to the message.
type attachment struct {
	name   string
	mime   string
	image  pi.Image
	bitmap *ui.Bitmap
	path   string     // a non-image file
	icon   *ui.Bitmap // its native icon, once loaded
}

func newApp() *app {
	wd, _ := os.Getwd()
	return &app{workdir: wd, savedSelected: -1, sidebarW: 288, focused: true}
}

// notifyDone badges the Dock when a run finishes while the window is not
// showing that session.
func (a *app) notifyDone(s *session) {
	if a.focused && a.activeSession() == s {
		return
	}
	a.unread++
	mygo.App.Dock.SetBadge(itoa(a.unread))
}

func (a *app) clearUnread() {
	if a.unread == 0 {
		return
	}
	a.unread = 0
	mygo.App.Dock.SetBadge("")
}

// Sidebar width bounds.
const (
	sidebarMinW = 180
	sidebarMaxW = 420
)

// clampSidebar keeps the sidebar between its bounds and leaves room for
// the content beside it.
func (a *app) clampSidebar(c *ui.Context) {
	max := float32(sidebarMaxW)
	if w, _ := c.Size(); w > 0 {
		if m := w - 260; m < max {
			max = m
		}
	}
	if max < sidebarMinW {
		max = sidebarMinW
	}
	if a.sidebarW < sidebarMinW {
		a.sidebarW = sidebarMinW
	}
	if a.sidebarW > max {
		a.sidebarW = max
	}
}

func (a *app) nextSessionID() int64 {
	a.seq++
	return a.seq
}

// folderOpen returns the expanded state of a sidebar folder group, made
// open by default.
func (a *app) folderOpen(dir string) *bool {
	if a.folders == nil {
		a.folders = map[string]*bool{}
	}
	p := a.folders[dir]
	if p == nil {
		open := true
		p = &open
		a.folders[dir] = p
	}
	return p
}

func (a *app) activeSession() *session {
	if a.active < 0 || a.active >= len(a.sessions) {
		return nil
	}
	return a.sessions[a.active]
}

// addSession opens a new session and makes it active.
func (a *app) addSession(workdir, resumePath string) *session {
	s := a.addSessionAt(workdir, resumePath, time.Time{})
	s.ensureLive()
	return s
}

// addSessionAt opens a new session and makes it active. A non-zero createdAt
// is used as the session's creation time; otherwise the session file's own
// start time (or the current time) decides.
func (a *app) addSessionAt(workdir, resumePath string, createdAt time.Time) *session {
	if workdir == "" {
		workdir = a.workdir
	}
	s := newSession(a, workdir, resumePath)
	if !createdAt.IsZero() {
		s.createdAt = createdAt
	}
	a.sessions = append(a.sessions, s)
	a.active = len(a.sessions) - 1
	a.page = 0
	a.saveState()
	if a.win != nil {
		a.win.Update(func() {})
	}
	return s
}

// defaultDir is the working directory a new session starts in: the active
// session's, or the app's.
func (a *app) defaultDir() string {
	if s := a.activeSession(); s != nil && s.workdir != "" {
		return s.workdir
	}
	return a.workdir
}

// newSessionTab opens a new session in the active session's directory. Its
// directory can be changed from the empty conversation's title.
func (a *app) newSessionTab() {
	a.addSession(a.defaultDir(), "")
}

func (a *app) switchTo(i int) {
	if i >= 0 && i < len(a.sessions) {
		a.active = i
		a.page = 0
	}
	a.keepLive()
}

// keepLive starts Pi for the session on screen and stops it for every other
// session with nothing in flight, so an open tab only holds a Pi while it is
// running a task or actually being looked at.
func (a *app) keepLive() {
	for i, s := range a.sessions {
		if i == a.active {
			s.ensureLive()
			continue
		}
		s.suspend()
	}
}

func (a *app) closeSession(i int) {
	if i < 0 || i >= len(a.sessions) {
		return
	}
	a.sessions[i].shutdown()
	a.sessions = append(a.sessions[:i], a.sessions[i+1:]...)
	switch {
	case len(a.sessions) == 0:
		a.active = 0
	case a.active > i:
		a.active--
	case a.active >= len(a.sessions):
		a.active = len(a.sessions) - 1
	}
	a.saveState()
	a.keepLive()
}

// --- persistence of the open sessions --------------------------------------

// savedTab is one open session, remembered across launches: its working
// directory and the Pi session file it continues, if it has one yet.
type savedTab struct {
	Dir       string `json:"dir"`
	File      string `json:"file,omitempty"`
	CreatedAt int64  `json:"createdAt,omitempty"` // unix milliseconds
}

type appState struct {
	Active   int        `json:"active"`
	SidebarW float32    `json:"sidebarW,omitempty"`
	Settings settings   `json:"settings,omitempty"`
	Tabs     []savedTab `json:"tabs"`
}

func (a *app) statePath() string {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "open-sessions.json")
}

// saveState writes the open sessions so the next launch reopens them.
func (a *app) saveState() {
	path := a.statePath()
	if path == "" {
		return
	}
	state := appState{Active: a.active, SidebarW: a.sidebarW, Settings: a.settings}
	for _, s := range a.sessions {
		tab := savedTab{Dir: s.workdir, File: s.state.SessionFile}
		if !s.createdAt.IsZero() {
			tab.CreatedAt = s.createdAt.UnixMilli()
		}
		state.Tabs = append(state.Tabs, tab)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

// loadState reads the open sessions of the last launch.
func (a *app) loadState() (appState, bool) {
	var state appState
	path := a.statePath()
	if path == "" {
		return state, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return state, false
	}
	if json.Unmarshal(data, &state) != nil || len(state.Tabs) == 0 {
		return state, false
	}
	return state, true
}

// restore reopens the sessions of the last launch, or starts one new
// session in the current directory.
func (a *app) restore() {
	state, ok := a.loadState()
	if state.SidebarW > 0 {
		a.sidebarW = state.SidebarW
	}
	a.settings = state.Settings
	mygo.Theme.SetSource(appearanceSource(a.settings.Appearance))
	a.applyPiSettings()
	if !ok {
		a.addSession(a.workdir, "")
		return
	}
	for _, tab := range state.Tabs {
		dir := tab.Dir
		if dir == "" {
			dir = a.workdir
		}
		var created time.Time
		if tab.CreatedAt > 0 {
			created = time.UnixMilli(tab.CreatedAt)
		}
		a.addSessionAt(dir, tab.File, created)
	}
	if state.Active >= 0 && state.Active < len(a.sessions) {
		a.active = state.Active
	} else {
		a.active = 0
	}
	// Only the tab on screen starts Pi now; the others start when they are
	// first shown.
	a.keepLive()
}

func (a *app) shutdown() {
	for _, s := range a.sessions {
		s.shutdown()
	}
}

func (a *app) themeFor(dark bool) *ui.Theme {
	if a.theme == nil || a.theme.Dark != dark {
		a.theme = customTheme(dark)
	}
	if a.settings.FontSize > 0 {
		a.theme.FontSize = a.settings.FontSize
	}
	return a.theme
}

// openSaved loads the saved-session list for the resume picker.
func (a *app) openSaved() {
	a.showSaved = true
	a.loadingSaved = true
	a.savedErr = ""
	a.savedSelected = -1
	go func() {
		sessions, err := scanSessions()
		a.win.Update(func() {
			a.loadingSaved = false
			if err != nil {
				a.savedErr = err.Error()
				return
			}
			a.saved = sessions
		})
	}()
}

func (a *app) openFolder() {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:    a.win,
		Title:     "Choose a project folder",
		Directory: true,
		Multiple:  false,
	})
	if err != nil || len(paths) == 0 {
		return
	}
	dir := paths[0]
	a.win.Update(func() { a.addSession(dir, "") })
}

// --- helpers ---------------------------------------------------------------

func encodeBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

func decodeBase64(s string) ([]byte, error) {
	if i := strings.IndexByte(s, ','); i >= 0 && strings.Contains(s[:i], "base64") {
		s = s[i+1:]
	}
	return base64.StdEncoding.DecodeString(s)
}

// shortPath shortens the user's home directory in p to "~".
func shortPath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(p, home+string(os.PathSeparator)); ok {
		return "~/" + rest
	}
	return p
}

func timeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
