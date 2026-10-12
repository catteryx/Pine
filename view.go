package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/rain/pi-client/internal/pi"
)

// extUI is an extension dialog (select, confirm, input, editor) awaiting an
// answer.
type extUI struct {
	req   pi.ExtensionUIRequest
	value string
}

// --- app chrome ------------------------------------------------------------

func (a *app) view(c *ui.Context) {
	c.SetTheme(a.themeFor(c.Theme().Dark))
	t := c.Theme()
	c.Root().Background(t.Background)
	if a.sidebarCollapsed {
		ui.Column(c).Fill().Background(t.Background).Children(func() { a.content(c, t) })
	} else {
		a.clampSidebar(c)
		sp := ui.Split(c, &a.sidebarW, func() { a.sidebar(c, t) }, func() { a.content(c, t) }).Fill()
		if sp.Changed() {
			a.saveState()
		}
	}
	a.savedDialog(c, t)
	a.infoDialog(c, t)
	a.settingsDialog(c, t)
	a.previewOverlay(c, t)
}

// previewOverlay shows an image (a tool result, say) large over the window.
func (a *app) previewOverlay(c *ui.Context, t *ui.Theme) {
	if a.preview == nil {
		return
	}
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Gap(8).Children(func() {
			ui.Text(c, a.previewName).Bold().FontSize(14)
			ui.Image(c, a.preview).Size(560, 560).Radius(10)
			ui.Row(c).Justify(ui.End).Children(func() {
				if ui.Button(c, "Close").Clicked() {
					a.preview = nil
				}
			})
		})
	})
	if !open {
		a.preview = nil
	}
}

func (a *app) content(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Fill().Background(t.Background).Children(func() {
		switch a.page {
		case 1:
			a.mcpPage(c, t)
		case 2:
			a.skillsPage(c, t)
		default:
			if s := a.activeSession(); s != nil {
				s.main(c, t)
			} else {
				a.noSession(c, t)
			}
		}
	})
}

// topHeight is the height of the title bar: the room the window controls
// take, or a bar of its own when there are none.
func topHeight(c *ui.Context) float32 {
	if h := c.TitleBar().Height; h > 0 {
		return h
	}
	return 36
}

// headerButtonsW is the room the title bar keeps on each side of the
// session name, so the name stays centered.
const headerButtonsW = 56

// sidebar is the full-height session list, which spans the title bar.
func (a *app) sidebar(c *ui.Context, t *ui.Theme) {
	if a.sidebarCollapsed {
		return
	}
	ui.Column(c.Key("sidebar")).Fill().Background(sidebarBg(t)).Children(func() {
		// The title bar runs over the sidebar: its buttons sit at the
		// right, clear of the window controls at the left.
		ui.Row(c).Height(topHeight(c)).AlignItems(ui.Center).
			Padding(0, t.Space(2)).DragWindow().Children(func() {
			ui.Spacer(c)
			iconButton(c, "newsession", iconCompose, "New session", 15, func() { a.newSessionTab() })
			iconButton(c, "toggle", iconSidebar, "Hide sidebar", 15, func() { a.sidebarCollapsed = true })
		})
		ui.Column(c).Padding(8, 4).Gap(1).Children(func() {
			sideRow(c, t, "newtask", iconCompose, "New Task", func() { a.newSessionTab() })
			sideNavRow(c, t, "mcp", iconMcp, "MCP", a.page == 1, func() {
				if a.page == 1 {
					a.page = 0
				} else {
					a.page = 1
				}
			})
			sideNavRow(c, t, "skills", iconSkill, "Skills", a.page == 2, func() {
				if a.page == 2 {
					a.page = 0
				} else {
					a.page = 2
				}
			})
		})
		ui.Row(c).Padding(8, 9).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconSearch).FontSize(15).TextColor(t.TextMuted)
			ui.TextInputBase(c.Key("search"), &a.search).Placeholder("Search")
		})
		ui.Scroll(c).Grow(1).Padding(8, 4, 8, 12).Children(func() {
			a.sessionGroups(c, t)
		})
		ui.Row(c).Padding(8, 10).AlignItems(ui.Center).
			BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
			gear := ui.ButtonBase(c.Key("settings")).Size(30, 30).Radius(8).
				AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer)
			gear.Children(func() { ui.Icon(c, iconGear).FontSize(16).TextColor(t.TextMuted) })
			gear.Menu(func(m *ui.Menu) {
				if s := a.activeSession(); s != nil {
					s.actionsMenu(m)
				}
			})
		})
	})
}

func (a *app) noSession(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Fill().Center().Gap(12).Children(func() {
		ui.Text(c, "No session open").FontSize(18).Bold()
		ui.Text(c, "Open a folder or start a new session.").TextColor(t.TextMuted)
		if ui.PrimaryButton(c, "New Task").Clicked() {
			a.newSessionTab()
		}
	})
}

// sessionGroups lists the sessions grouped by working directory, the
// groups and the sessions in them ordered by time, most recent first.
func (a *app) sessionGroups(c *ui.Context, t *ui.Theme) {
	q := strings.ToLower(strings.TrimSpace(a.search))
	type group struct {
		dir   string
		items []int
	}
	byDir := map[string]*group{}
	var groups []*group
	for i, s := range a.sessions {
		if q != "" && !strings.Contains(strings.ToLower(s.displayName()+" "+s.workdir), q) {
			continue
		}
		g := byDir[s.workdir]
		if g == nil {
			g = &group{dir: s.workdir}
			byDir[s.workdir] = g
			groups = append(groups, g)
		}
		g.items = append(g.items, i)
	}
	newest := func(g *group) time.Time {
		var t time.Time
		for _, i := range g.items {
			if c := a.sessions[i].createdAt; c.After(t) {
				t = c
			}
		}
		return t
	}
	sort.Slice(groups, func(i, j int) bool { return newest(groups[i]).After(newest(groups[j])) })
	for _, g := range groups {
		sort.Slice(g.items, func(i, j int) bool {
			return a.sessions[g.items[i]].createdAt.After(a.sessions[g.items[j]].createdAt)
		})
		label := "No project"
		if g.dir != "" {
			label = filepath.Base(g.dir)
		}
		open := a.folderOpen(g.dir)
		parts := ui.CollapsibleBase(c.Key("g:"+g.dir), open)
		parts.Trigger.FillWidth().Gap(7).AlignItems(ui.Center).Padding(8, 10, 4, 4).Children(func() {
			icon := iconFolder
			if *open {
				icon = iconFolderOpen
			}
			ui.Icon(c, icon).FontSize(18).TextColor(t.TextMuted)
			ui.Text(c, label).FontSize(12).Bold().TextColor(t.TextMuted).SingleLine().Grow(1)
		})
		parts.Panel(func() {
			for _, i := range g.items {
				a.sessionRow(c, t, i, a.sessions[i])
			}
		})
	}
}

func (a *app) sessionRow(c *ui.Context, t *ui.Theme, i int, s *session) {
	row := ui.ButtonBase(c.Key(s.id)).FillWidth().Padding(8, 9).Radius(9).
		AlignItems(ui.Stretch).Cursor(ui.CursorPointer)
	if i == a.active {
		row.Background(t.SurfacePressed)
	}
	if row.Clicked() {
		a.switchTo(i)
	}
	row.ContextMenu(func(m *ui.Menu) {
		if m.Item("Rename").Chosen() {
			s.renaming = true
			s.renameDraft = s.displayName()
		}
		m.Separator()
		if m.Item("Delete session…").Chosen() {
			name, path := s.displayName(), s.state.SessionFile
			go a.confirmDelete(s, name, path)
		}
		if m.Item("Close tab").Chosen() {
			a.closeSession(i)
		}
	})
	row.Children(func() {
		if s.renaming {
			in := ui.TextInputBase(c, &s.renameDraft).Grow(1).Focus().FontSize(13).TextColor(t.Text)
			done, keep := false, true
			switch {
			case in.Submitted():
				done = true
			case in.Shortcut(0, ui.KeyEscape):
				done, keep = true, false
			case !in.Focused():
				done = true
			}
			if done {
				s.renaming = false
				if keep && strings.TrimSpace(s.renameDraft) != "" {
					go s.setSessionName(strings.TrimSpace(s.renameDraft))
				}
			}
			return
		}
		ui.Row(c).Grow(1).Gap(6).AlignItems(ui.Center).Children(func() {
			if s.running {
				ui.Spinner(c)
			}
			ui.Text(c, s.displayName()).FontSize(13).TextColor(t.Text).SingleLine().Grow(1)
			ui.Text(c, shortAgo(s.createdAt)).FontSize(11).TextColor(t.TextMuted)
		})
	})
}

// confirmDelete asks, then closes a session and removes its Pi session file.
func (a *app) confirmDelete(s *session, name, path string) {
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent: a.win, Type: mygo.MessageWarning, Message: "Delete this session?",
		Detail: name, Buttons: []string{"Cancel", "Delete"}, DefaultButton: 1, CancelButton: 0,
	})
	if err != nil || res.Button != 1 {
		return
	}
	a.win.Update(func() {
		for i, x := range a.sessions {
			if x == s {
				a.closeSession(i)
				break
			}
		}
		if path != "" {
			_ = os.Remove(path)
		}
	})
}

// pageHeader is the top bar of a sidebar page: a title and extra controls,
// with a drag region between.
func (a *app) pageHeader(c *ui.Context, t *ui.Theme, title string, extra func()) {
	pad := t.Space(2)
	if a.sidebarCollapsed {
		// The window controls sit over the content when the sidebar hides.
		pad = c.TitleBar().Left + t.Space(2)
	}
	ui.Row(c.Key("pageheader")).Height(topHeight(c)).AlignItems(ui.Center).
		Padding(0, 16, 0, pad).Gap(6).Children(func() {
		if a.sidebarCollapsed {
			iconButton(c, "page-toggle", iconSidebar, "Show the sidebar", 15, func() { a.sidebarCollapsed = false })
		}
		ui.Text(c, title).FontSize(14).Bold().SingleLine()
		ui.Row(c).Grow(1).DragWindow()
		extra()
	})
}

// mcpPage is the MCP servers view in the body.
func (a *app) mcpPage(c *ui.Context, t *ui.Theme) {
	if !a.mcpLoaded && !a.mcpLoading {
		a.loadMCP()
	}
	ui.Column(c).Fill().Children(func() {
		a.pageHeader(c, t, "MCP servers", func() {
			iconButton(c, "mcp-reload", iconRefresh, "Reload", 15, func() {
				a.mcpLoaded, a.mcpLoading, a.mcpErr = false, false, ""
				a.loadMCP()
			})
		})
		ui.Scroll(c).Grow(1).Padding(20, 22).Gap(12).Children(func() {
			switch {
			case a.mcpLoading:
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c)
					ui.Text(c, "Loading MCP…").TextColor(t.TextMuted)
				})
			case a.mcpErr != "":
				ui.Text(c, a.mcpErr).TextColor(t.Danger)
			case len(a.mcpServers) == 0:
				ui.Text(c, "No MCP servers configured.").TextColor(t.TextMuted)
			default:
				for _, srv := range a.mcpServers {
					ui.Box(c).Background(sidebarBg(t)).Border(1, t.Border).Radius(10).
						Padding(12, 14).Children(func() {
						ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
							ui.Box(c).Size(8, 8).Radius(4).Background(mcpStateColor(srv.State, t))
							ui.Text(c, srv.Name).FontSize(14).Bold().Grow(1)
							ui.Text(c, srv.State).FontSize(11.5).TextColor(t.TextMuted)
						})
						if len(srv.Tools) == 0 {
							ui.Text(c, "no tools").FontSize(12).TextColor(t.TextMuted)
						} else {
							ui.Row(c).Gap(6).Wrap().Children(func() {
								for _, tool := range srv.Tools {
									ui.Badge(c, tool)
								}
							})
						}
					})
				}
				for _, e := range a.mcpErrors {
					ui.Text(c, "! "+e).FontSize(12).TextColor(t.Danger)
				}
			}
		})
	})
}

func mcpStateColor(state string, t *ui.Theme) ui.Color {
	switch state {
	case "connected", "ready", "ok":
		return t.Success
	case "error", "failed":
		return t.Danger
	default:
		return t.TextMuted
	}
}

// skillsPage is the skills view in the body; a click puts the skill command
// in the composer and returns to the conversation.
func (a *app) skillsPage(c *ui.Context, t *ui.Theme) {
	s := a.activeSession()
	ui.Column(c).Fill().Children(func() {
		a.pageHeader(c, t, "Skills", func() {})
		ui.Scroll(c).Grow(1).Padding(20, 22).Gap(10).Children(func() {
			if s == nil {
				ui.Text(c, "No session").TextColor(t.TextMuted)
				return
			}
			n := 0
			for _, cm := range s.commands {
				if cm.Source != "skill" {
					continue
				}
				cm := cm
				n++
				row := ui.ButtonBase(c.Key("skill:"+cm.Name)).FillWidth().Padding(10, 12).Radius(9).
					Justify(ui.Start).AlignItems(ui.Center).Cursor(ui.CursorPointer)
				if row.Hovered() {
					row.Background(t.SurfaceHover)
				}
				row.OnClick(func() {
					s.draft = "/" + cm.Name + " "
					s.focusComposer = true
					a.page = 0
				})
				row.Children(func() {
					ui.Column(c).Grow(1).Gap(2).Children(func() {
						ui.Text(c, cm.Name).FontSize(13.5).TextColor(t.Text).SingleLine()
						if cm.Description != "" {
							ui.Text(c, cm.Description).FontSize(12).TextColor(t.TextMuted).SingleLine()
						}
					})
				})
			}
			if n == 0 {
				ui.Text(c, "No skills found.").TextColor(t.TextMuted)
			}
		})
	})
}

// --- session main pane -----------------------------------------------------

func (s *session) main(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Fill().Children(func() {
		s.header(c, t)
		// The transcript fills the body and the composer floats over its
		// bottom, so content shows down to the input's edge; the status bar
		// stays below the body.
		ui.Box(c).Grow(1).Children(func() {
			s.transcript(c, t)
			s.composer(c, t)
		})
		s.statusBar(c, t)
	})
	s.extDialog(c, t)
	if s.pendingToast != "" {
		c.Toast(s.pendingToast)
		s.pendingToast = ""
	}
}

// header is the title bar over the conversation: the session's name,
// centered, opens its info panel; while the sidebar hides, the new-session
// button and the sidebar toggle sit at its left.
func (s *session) header(c *ui.Context, t *ui.Theme) {
	bar := c.TitleBar()
	pad := t.Space(2)
	if s.app.sidebarCollapsed {
		// The window controls sit over the content when the sidebar hides.
		pad = bar.Left + t.Space(2)
	}
	ui.Row(c.Key("header")).Height(topHeight(c)).AlignItems(ui.Center).
		DragWindow().Padding(0, t.Space(2), 0, pad).Children(func() {
		left := ui.Row(c).Width(headerButtonsW).Shrink(0).AlignItems(ui.Center)
		if s.app.sidebarCollapsed {
			left.Children(func() {
				iconButton(c, "new", iconCompose, "New session", 15, func() { s.app.newSessionTab() })
				iconButton(c, "toggle", iconSidebar, "Show sidebar", 15, func() { s.app.sidebarCollapsed = false })
			})
		}
		ui.Spacer(c)

		name := s.displayName()
		nameBtn := ui.ButtonBase(c).Padding(t.Space(1.5), t.Space(2.5)).Radius(t.Space(1)).
			Shrink(0).Label(name).Tooltip("Session info")
		if nameBtn.Hovered() {
			nameBtn.Background(t.Text.Alpha(0.08))
		}
		nameBtn.Children(func() {
			ui.Text(c, name).FontSize(t.FontSize).FontWeight(600).TextColor(t.Text).SingleLine()
		})
		if nameBtn.Clicked() {
			s.app.showInfo = !s.app.showInfo
		}
		ui.Spacer(c)

		right := ui.Row(c).Width(headerButtonsW).Shrink(0).Justify(ui.End).AlignItems(ui.Center)
		right.Children(func() {
			iconButton(c, "info", iconPanel, "Session info", 16, func() { s.app.showInfo = true })
		})
	})
}

func (s *session) actionsMenu(m *ui.Menu) {
	if m.Item("New session").Chosen() {
		s.app.newSessionTab()
	}
	if m.Item("Resume session…").Chosen() {
		s.app.openSaved()
	}
	if m.Item("Open project folder…").Chosen() {
		go s.app.openFolder()
	}
	m.Separator()
	if m.Item("Reset conversation").Chosen() {
		s.resetSession()
	}
	if m.Item("Compact context").Chosen() {
		s.compact()
	}
	if m.Item("Export as HTML…").Chosen() {
		go s.exportHTML()
	}
	m.Separator()
	if m.Item("Reconnect to Pi").Chosen() {
		s.disconnect()
		s.connect()
	}
	if m.Item("Close session").Chosen() {
		for i, x := range s.app.sessions {
			if x == s {
				s.app.closeSession(i)
				break
			}
		}
	}
}

func (s *session) modelMenu(m *ui.Menu) {
	if len(s.models) == 0 {
		m.Item("No models configured").Disabled(true)
		return
	}
	if s.state.Model != nil {
		m.Item("Current: " + s.state.Model.Label()).Disabled(true)
		m.Separator()
	}
	type group struct {
		provider string
		models   []pi.Model
	}
	var groups []*group
	index := map[string]*group{}
	for _, mo := range s.models {
		g := index[mo.Provider]
		if g == nil {
			g = &group{provider: mo.Provider}
			index[mo.Provider] = g
			groups = append(groups, g)
		}
		g.models = append(g.models, mo)
	}
	for _, g := range groups {
		g := g
		if len(groups) == 1 {
			s.modelItems(m, g.models)
			continue
		}
		m.Submenu(g.provider, func(sm *ui.Menu) { s.modelItems(sm, g.models) })
	}
}

func (s *session) modelItems(m *ui.Menu, models []pi.Model) {
	for _, mo := range models {
		mo := mo
		it := m.Item(modelItemLabel(mo))
		if s.state.Model != nil && s.state.Model.ID == mo.ID && s.state.Model.Provider == mo.Provider {
			it.Checked(true)
		}
		if it.Chosen() {
			go s.setModel(mo)
		}
	}
}

func modelItemLabel(mo pi.Model) string {
	name := mo.Name
	if name == "" {
		name = mo.ID
	}
	return name
}

func (s *session) thinkMenu(m *ui.Menu) {
	for _, level := range s.levels {
		level := level
		it := m.Item(capitalize(level))
		if level == s.state.ThinkingLevel {
			it.Checked(true)
		}
		if it.Chosen() {
			go s.setThinking(level)
		}
	}
}

func (s *session) commandMenu(m *ui.Menu) {
	for _, cmd := range s.commands {
		cmd := cmd
		label := "/" + cmd.Name
		if cmd.Description != "" {
			label += " — " + cmd.Description
		}
		if m.Item(label).Chosen() {
			s.draft = "/" + cmd.Name + " "
			s.focusComposer = true
		}
	}
}

// --- transcript ------------------------------------------------------------

func (s *session) transcript(c *ui.Context, t *ui.Theme) {
	if len(s.entries) == 0 {
		s.emptyState(c, t)
		return
	}
	// The composer floats over the list, so the content ends where the
	// composer begins; the list itself reaches the composer's bottom edge.
	bottom := s.composerH
	if s.composerH == 0 {
		bottom = 130
	}
	s.chat.Key = func(i int) any { return s.entries[i].id }
	s.chat.FollowEnd = true
	ui.List(c.Key("chat"), &s.chat, len(s.entries), func(i int) {
		s.renderEntry(c, t, s.entries[i])
	}).Fill().Padding(24, 28, bottom, 28).Gap(20).Justify(ui.End)
}

// emptyState is the placeholder of a conversation with no messages: its
// working directory, which a click changes, and a hint.
func (s *session) emptyState(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Fill().Center().Gap(12).Padding(24).Children(func() {
		dirBtn := ui.ButtonBase(c.Key("dir")).Padding(6, 12).Radius(10).Gap(9).
			AlignItems(ui.Center).Cursor(ui.CursorPointer)
		if dirBtn.Hovered() {
			dirBtn.Background(t.SurfaceHover)
		}
		if dirBtn.Clicked() {
			go s.changeDir()
		}
		dirBtn.Children(func() {
			ui.Icon(c, iconFolder).FontSize(16).TextColor(t.TextMuted)
			ui.Text(c, shortPath(s.workdir)).FontSize(20).Bold().TextColor(t.Text).SingleLine()
		})
		ui.Text(c, s.emptyHint()).TextColor(t.TextMuted).TextAlign(ui.Center).MaxWidth(460).LineHeight(1.5)
	})
}

func (s *session) renderEntry(c *ui.Context, t *ui.Theme, e *entry) {
	switch e.kind {
	case kindUser:
		s.renderUser(c, t, e)
	case kindAssistant:
		s.renderAssistant(c, t, e)
	case kindBash:
		s.renderBash(c, t, e)
	case kindSummary:
		s.renderSummary(c, t, e)
	case kindNotice:
		s.renderNotice(c, t, e, false)
	case kindError:
		s.renderNotice(c, t, e, true)
	}
}

func (s *session) renderUser(c *ui.Context, t *ui.Theme, e *entry) {
	ui.Column(c).AlignItems(ui.End).Gap(6).Children(func() {
		s.renderImages(c, t, e)
		if strings.TrimSpace(e.text) != "" {
			box := ui.Box(c).Background(userBubble(t)).Radius(16).Padding(10, 15).
				MaxWidthPercent(78)
			box.ContextMenu(func(m *ui.Menu) {
				if m.Item("Copy").Chosen() {
					mygo.Clipboard.WriteText(e.text)
				}
				if m.Item("Edit & resend").Chosen() {
					s.draft = e.text
					s.focusComposer = true
				}
				m.Separator()
				m.EditItems()
			})
			box.Children(func() {
				ui.Text(c, e.text).FontSize(14).LineHeight(1.5).Selectable()
			})
		}
	})
}

func (s *session) renderImages(c *ui.Context, t *ui.Theme, e *entry) {
	if len(e.images) == 0 {
		return
	}
	ui.Row(c).Gap(8).Wrap().Children(func() {
		for i := range e.images {
			if i < len(e.thumbs) && e.thumbs[i] != nil {
				ui.Image(c, e.thumbs[i]).Size(132, 132).Radius(10)
			} else {
				ui.Badge(c, "image")
			}
		}
	})
}

func (s *session) renderAssistant(c *ui.Context, t *ui.Theme, e *entry) {
	col := ui.Column(c).Gap(10)
	col.ContextMenu(func(m *ui.Menu) {
		if m.Item("Copy").Chosen() {
			mygo.Clipboard.WriteText(assistantText(e))
		}
		m.Separator()
		m.EditItems()
	})
	col.Children(func() {
		if e.durationMs > 0 && !e.streaming && hasDetails(e) {
			// Done: thinking and tool calls fold into "Worked for …",
			// and only the conversation text stays out.
			s.renderWorkedFor(c, t, e)
			for i := range e.blocks {
				if e.blocks[i].kind == blockText && strings.TrimSpace(e.blocks[i].text) != "" {
					renderMarkdown(c, t, e.blocks[i].text)
				}
			}
		} else {
			if e.durationMs > 0 {
				workedRule(c, t, e.durationMs)
			}
			for i := range e.blocks {
				s.renderBlock(c, t, e, i)
			}
		}
		if e.streaming && len(e.blocks) == 0 {
			ui.Spinner(c)
		}
		if e.errMsg != "" {
			ui.Box(c).Background(ui.RGBA(255, 59, 48, 0.10)).Radius(8).Padding(8, 10).Children(func() {
				ui.Text(c, e.errMsg).FontSize(12.5).TextColor(t.Danger).Selectable()
			})
		}
	})
}

// hasDetails reports whether an assistant entry holds thinking or tool
// calls, which fold into its "Worked for …" disclosure.
func hasDetails(e *entry) bool {
	for i := range e.blocks {
		if e.blocks[i].kind != blockText {
			return true
		}
	}
	return false
}

// workedRule is the centered "Worked for …" rule, for a run without
// details.
func workedRule(c *ui.Context, t *ui.Theme, ms int64) {
	ui.Row(c).Gap(12).AlignItems(ui.Center).Padding(2, 0, 6, 0).Children(func() {
		ui.Box(c).Grow(1).Height(1).Background(t.Border)
		ui.Text(c, "Worked for "+humanDuration(ms)).FontSize(11).TextColor(t.TextMuted)
		ui.Box(c).Grow(1).Height(1).Background(t.Border)
	})
}

// renderWorkedFor is the disclosure holding a finished run's thinking and
// tool calls, toggled by its "Worked for …" header.
func (s *session) renderWorkedFor(c *ui.Context, t *ui.Theme, e *entry) {
	parts := ui.CollapsibleBase(c.Key(e.id+":worked"), &e.detailsOpen)
	parts.Trigger.FillWidth().Gap(12).AlignItems(ui.Center).Padding(2, 0, 6, 0).Children(func() {
		ui.Box(c).Grow(1).Height(1).Background(t.Border)
		ui.Text(c, "Worked for "+humanDuration(e.durationMs)).FontSize(11).TextColor(t.TextMuted)
		ui.Text(c, chevron(e.detailsOpen)).FontSize(10).TextColor(t.TextMuted)
		ui.Box(c).Grow(1).Height(1).Background(t.Border)
	})
	parts.Panel(func() {
		ui.Column(c).Gap(10).Padding(2, 0, 6, 0).Children(func() {
			for i := range e.blocks {
				b := &e.blocks[i]
				switch b.kind {
				case blockThinking:
					if strings.TrimSpace(b.text) == "" {
						continue
					}
					ui.Column(c).Gap(3).Children(func() {
						ui.Text(c, "Thinking").FontSize(11).Bold().TextColor(t.TextMuted)
						ui.Text(c, b.text).FontSize(12.5).TextColor(t.TextMuted).LineHeight(1.5).Selectable()
					})
				case blockTool:
					s.renderTool(c, t, b, e.id+":d:"+fmt.Sprint(i))
				}
			}
		})
	})
}

func (s *session) renderBlock(c *ui.Context, t *ui.Theme, e *entry, i int) {
	b := &e.blocks[i]
	key := e.id + ":" + fmt.Sprint(i)
	switch b.kind {
	case blockText:
		if strings.TrimSpace(b.text) == "" {
			return
		}
		renderMarkdown(c, t, b.text)
	case blockThinking:
		if strings.TrimSpace(b.text) == "" {
			return
		}
		s.renderThinking(c, t, b, key)
	case blockTool:
		s.renderTool(c, t, b, key)
	}
}

func (s *session) renderThinking(c *ui.Context, t *ui.Theme, b *tblock, key string) {
	parts := ui.CollapsibleBase(c.Key(key), &b.expanded)
	parts.Trigger.Gap(6).AlignItems(ui.Center).Padding(3, 0).Children(func() {
		ui.Text(c, chevron(b.expanded)).FontSize(10).TextColor(t.TextMuted)
		ui.Text(c, "Thinking").FontSize(12).TextColor(t.TextMuted)
	})
	parts.Panel(func() {
		ui.Box(c).Margin(0, 0, 2, 0).Padding(2, 12).BorderWidth(0, 0, 0, 2).
			BorderColor(t.Border).Children(func() {
			ui.Text(c, b.text).FontSize(12.5).TextColor(t.TextMuted).LineHeight(1.5).Selectable()
		})
	})
}

func (s *session) renderTool(c *ui.Context, t *ui.Theme, b *tblock, key string) {
	ui.Box(c).Background(sidebarBg(t)).Border(1, t.Border).Radius(10).Clip().Children(func() {
		parts := ui.CollapsibleBase(c.Key(key), &b.expanded)
		parts.Trigger.Padding(8, 11).Gap(9).AlignItems(ui.Center).Children(func() {
			dot := t.Success
			switch {
			case b.pending:
				dot = t.Warning
			case b.resultErr:
				dot = t.Danger
			}
			ui.Box(c).Size(7, 7).Radius(4).Background(dot)
			ui.Text(c, b.name).Font("monospace").FontSize(12.5).Bold()
			if s := argSummary(b.name, b.args); s != "" {
				ui.Text(c, s).FontSize(12).TextColor(t.TextMuted).SingleLine().Grow(1)
			} else {
				ui.Spacer(c)
			}
			if b.pending {
				ui.Spinner(c)
			} else {
				ui.Text(c, toolStatus(b)).FontSize(11).TextColor(t.TextMuted)
			}
			ui.Text(c, chevron(b.expanded)).FontSize(10).TextColor(t.TextMuted)
		})
		parts.Panel(func() {
			ui.Column(c).Padding(0, 11, 10, 11).Gap(8).Children(func() {
				if p := toolPath(b); p != "" {
					pathRow(c, t, p)
				}
				if strings.TrimSpace(b.args) != "" && b.args != "{}" {
					codeBox(c, t, "json", b.args)
				}
				if len(b.images) > 0 {
					ui.Row(c).Gap(8).Wrap().Children(func() {
						for i := range b.images {
							if i < len(b.thumbs) && b.thumbs[i] != nil {
								img := b.thumbs[i]
								ui.Image(c, img).Size(120, 120).Radius(8).
									Cursor(ui.CursorPointer).OnClick(func() {
									s.app.preview = img
									s.app.previewName = b.name
								})
							}
						}
					})
				}
				if strings.TrimSpace(b.result) != "" {
					if b.name == "edit" {
						codeBoxDiff(c, t, b.result)
					} else {
						codeBox(c, t, toolResultLang(b.name), b.result)
					}
				}
			})
		})
	})
}

func toolStatus(b *tblock) string {
	switch {
	case b.pending:
		return "running"
	case b.resultErr:
		return "failed"
	default:
		if b.duration != nil {
			return fmt.Sprintf("%.1fs", *b.duration/1000)
		}
		return "done"
	}
}

func toolResultLang(name string) string {
	if name == "bash" {
		return "bash"
	}
	return ""
}

func chevron(open bool) string {
	if open {
		return "▾"
	}
	return "▸"
}

// codeBox is a bordered, highlighted code block without the language header.
func codeBox(c *ui.Context, t *ui.Theme, lang, code string) {
	ui.Box(c).Background(codeBg(t)).Border(1, t.Border).Radius(8).Padding(8, 10).Children(func() {
		renderCodeLines(c, t, lang, code)
	})
}

// codeBoxDiff shows text with added, removed and hunk lines colored.
func codeBoxDiff(c *ui.Context, t *ui.Theme, text string) {
	ui.Box(c).Background(codeBg(t)).Border(1, t.Border).Radius(8).Padding(8, 10).Children(func() {
		ui.Column(c).Gap(0).Children(func() {
			for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
				col := t.Text
				switch {
				case strings.HasPrefix(line, "+"):
					col = t.Success
				case strings.HasPrefix(line, "-"):
					col = t.Danger
				case strings.HasPrefix(line, "@@"):
					col = t.TextMuted
				}
				ui.Text(c, line).Font("monospace").FontSize(12).FixedLineHeight(18).TextColor(col)
			}
		})
	})
}

// toolPath returns the file a tool acted on, from its arguments.
func toolPath(b *tblock) string {
	var m map[string]any
	if json.Unmarshal([]byte(b.args), &m) != nil {
		return ""
	}
	for _, k := range []string{"path", "file_path", "file"} {
		if v, ok := m[k].(string); ok {
			return v
		}
	}
	return ""
}

// pathRow shows a tool's file, with a menu to reveal or copy it.
func pathRow(c *ui.Context, t *ui.Theme, p string) {
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, iconDoc).FontSize(12).TextColor(t.TextMuted)
		el := ui.Text(c, p).FontSize(11.5).TextColor(t.TextMuted).SingleLine().Grow(1)
		el.ContextMenu(func(m *ui.Menu) {
			if m.Item("Reveal in Finder").Chosen() {
				mygo.Shell.ShowItemInFolder(p)
			}
			if m.Item("Copy path").Chosen() {
				mygo.Clipboard.WriteText(p)
			}
		})
	})
}

func (s *session) renderBash(c *ui.Context, t *ui.Theme, e *entry) {
	ui.Box(c).Background(sidebarBg(t)).Border(1, t.Border).Radius(10).Clip().Children(func() {
		ui.Row(c).Padding(8, 11).Gap(8).AlignItems(ui.Center).
			BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
			ui.Text(c, "$").Font("monospace").FontSize(12.5).TextColor(t.Success)
			ui.Text(c, e.command).Font("monospace").FontSize(12.5).Grow(1).SingleLine()
			if e.exitCode != nil && *e.exitCode != 0 {
				ui.Text(c, fmt.Sprintf("exit %d", *e.exitCode)).FontSize(11).TextColor(t.Danger)
			}
		})
		if strings.TrimSpace(e.output) != "" {
			ui.Box(c).Padding(9, 11).Children(func() {
				renderCodeLines(c, t, "", e.output)
			})
		}
	})
}

func (s *session) renderSummary(c *ui.Context, t *ui.Theme, e *entry) {
	ui.Box(c).Background(sidebarBg(t)).Radius(10).Padding(10, 12).Children(func() {
		ui.Text(c, "SUMMARY").FontSize(10.5).Bold().TextColor(t.TextMuted)
		ui.Text(c, e.text).FontSize(12.5).TextColor(t.TextMuted).LineHeight(1.5).Selectable()
	})
}

func (s *session) renderNotice(c *ui.Context, t *ui.Theme, e *entry, isErr bool) {
	col := t.TextMuted
	if isErr {
		col = t.Danger
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(6, 6).Radius(3).Background(col)
		ui.Text(c, e.text).FontSize(12).TextColor(col).Selectable()
	})
}

// --- composer --------------------------------------------------------------

func (s *session) composer(c *ui.Context, t *ui.Theme) {
	col := ui.Column(c.Key("composer")).Absolute().Left(0).Right(0).Bottom(0).
		PassThrough().Padding(16, 22, 6)
	composerFocused := false
	col.Children(func() {
		card := ui.Box(c).Background(t.Background).Border(1, t.Border).Radius(16).
			Padding(12, 14).Shadow(0, 6, 20, 0, ui.RGBA(0, 0, 0, 0.06))
		if card.FileDragOver() {
			card.Border(1, t.Accent)
		}
		if files := card.DroppedFiles(); len(files) > 0 {
			s.addFiles(files)
		}
		card.Children(func() {
			ui.Column(c).Gap(10).Children(func() {
				if len(s.attach) > 0 {
					ui.Row(c).Gap(8).Wrap().Children(func() {
						for i := range s.attach {
							i := i
							at := s.attach[i]
							ui.Row(c).Gap(6).AlignItems(ui.Center).Background(t.Surface).
								Radius(8).Padding(4, 6).Children(func() {
								switch {
								case at.bitmap != nil:
									ui.Image(c, at.bitmap).Size(22, 22).Radius(5)
								case at.icon != nil:
									ui.Image(c, at.icon).Size(18, 18)
								default:
									ui.Icon(c, iconDoc).FontSize(15).TextColor(t.TextMuted)
								}
								ui.Text(c, truncate(at.name, 22)).FontSize(12).TextColor(t.TextMuted)
								ui.Text(c, "✕").FontSize(12).TextColor(t.TextMuted).
									Cursor(ui.CursorPointer).OnClick(func() {
									if i < len(s.attach) {
										s.attach = append(s.attach[:i], s.attach[i+1:]...)
									}
								})
							})
						}
					})
				}

				input := ui.TextAreaBase(c.Key("input"), &s.draft).Lines(1, 10).
					Placeholder("Do anything…  (drop files, ⏎ to send)")
				input.HandleInput(s.composerKey)
				if s.focusComposer {
					input.AutoFocus()
					s.focusComposer = false
				}
				composerFocused = input.Focused()

				ui.Row(c).Gap(6).AlignItems(ui.Center).Wrap().Children(func() {
					iconButton(c, "add", iconPlus, "Add files", 16, func() { go s.pickFiles() })
					model := "Model"
					if s.state.Model != nil && s.state.Model.Name != "" {
						model = s.state.Model.Name
					}
					chip(c, "model", nil, truncate(model, 22), func(m *ui.Menu) { s.modelMenu(m) }).
						Disabled(len(s.models) == 0)
					if s.state.ThinkingLevel != "" {
						chip(c, "think", nil, capitalize(s.state.ThinkingLevel), func(m *ui.Menu) { s.thinkMenu(m) }).
							Disabled(len(s.levels) == 0)
					}
					ui.Spacer(c)
					if s.running {
						roundButton(c, "stop", iconSquare, t.Surface, t.Text, func() { go s.stop() })
					} else {
						enabled := strings.TrimSpace(s.draft) != "" || len(s.attach) > 0
						bg, fg := t.Surface, t.TextMuted
						if enabled {
							bg, fg = t.Accent, t.AccentText
						}
						roundButton(c, "send", iconArrowUp, bg, fg, func() {
							if enabled {
								s.send()
							}
						})
					}
				})
			})
			s.slashPopup(c, t, composerFocused)
		})
	})
	if h := col.Bounds().H; h > 0 {
		s.composerH = h
	}
}

// slashPopup shows the slash command menu above the composer while a command
// name is being typed.
func (s *session) slashPopup(c *ui.Context, t *ui.Theme, focused bool) {
	q, ok := slashQuery(s.draft)
	if !ok || !focused {
		s.slashOpen = false
		return
	}
	matches := s.slashMatches(q)
	if len(matches) == 0 {
		s.slashOpen = false
		return
	}
	s.slashOpen = true
	if s.slashIdx >= len(matches) {
		s.slashIdx = len(matches) - 1
	}
	if s.slashIdx < 0 {
		s.slashIdx = 0
	}
	ui.Box(c).Absolute().Left(0).Right(0).BottomPercent(100).Margin(0, 0, 6, 0).Children(func() {
		ui.Box(c).Background(t.Background).Border(1, t.Border).Radius(12).
			Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.16)).Padding(4).Children(func() {
			ui.Scroll(c).MaxHeight(300).Children(func() {
				for i := range matches {
					s.slashRow(c, t, i, matches[i])
				}
			})
		})
	})
}

func (s *session) slashRow(c *ui.Context, t *ui.Theme, i int, cm cmd) {
	row := ui.ButtonBase(c.Key("slash:"+cm.name)).FillWidth().Padding(6, 10).Radius(8).
		Justify(ui.Start).Gap(10).AlignItems(ui.Center).Cursor(ui.CursorPointer)
	if row.Hovered() {
		s.slashIdx = i
	}
	if i == s.slashIdx {
		row.Background(t.SurfaceHover)
		row.ScrollIntoView()
	}
	row.OnClick(func() {
		s.slashIdx = i
		s.completeSlash(true)
	})
	row.Children(func() {
		ui.Text(c, "/"+cm.name).Font("monospace").FontSize(12.5).TextColor(t.Text)
		if cm.args != "" {
			ui.Text(c, cm.args).FontSize(11).TextColor(t.TextMuted)
		}
		ui.Text(c, cm.desc).FontSize(11.5).TextColor(t.TextMuted).SingleLine().Grow(1)
		if cm.source != "client" {
			ui.Badge(c, cm.source)
		}
	})
}

// composerKey handles the composer's keyboard: Enter (or Cmd+Enter) sends,
// Shift+Enter inserts a newline.
func (s *session) composerKey(ev ui.InputEvent) bool {
	if ev.Kind == ui.InputCommand && ev.Text == "paste" {
		if png := mygo.Clipboard.ReadImage(); len(png) > 0 {
			s.addImageBytes("pasted image", png)
			return true
		}
		return false
	}
	if ev.Kind != ui.InputKeyDown {
		return false
	}
	if s.slashOpen {
		switch {
		case ev.Key == ui.KeyDown && ev.Mods == 0:
			s.slashIdx++
			return true
		case ev.Key == ui.KeyUp && ev.Mods == 0:
			s.slashIdx--
			return true
		case ev.Key == ui.KeyTab && (ev.Mods == 0 || ev.Mods == ui.Shift):
			s.completeSlash(false)
			return true
		case ev.Key == ui.KeyEscape && ev.Mods == 0:
			s.slashOpen = false
			return true
		case ev.Key == ui.KeyEnter && (ev.Mods == 0 || ev.Mods == ui.Cmd):
			s.completeSlash(true)
			return true
		}
		return false
	}
	if ev.Key != ui.KeyEnter {
		return false
	}
	switch ev.Mods {
	case 0, ui.Cmd:
		s.send()
		return true
	case ui.Shift:
		s.draft += "\n"
		s.focusComposer = true
		return true
	default:
		return false
	}
}

// completeSlash fills the draft from the slash menu: a client command runs
// (when execute), others are completed to "/name " for arguments.
func (s *session) completeSlash(execute bool) {
	query, ok := slashQuery(s.draft)
	if !ok {
		s.slashOpen = false
		return
	}
	matches := s.slashMatches(query)
	if len(matches) == 0 {
		s.slashOpen = false
		return
	}
	i := min(max(s.slashIdx, 0), len(matches)-1)
	c := matches[i]
	s.slashOpen = false
	s.slashIdx = 0
	if c.client && execute {
		s.draft = "/" + c.name
		s.sendOrCommand()
		return
	}
	s.draft = "/" + c.name + " "
	s.focusComposer = true
}

// addImageBytes stages an image from raw PNG bytes.
func (s *session) addImageBytes(name string, png []byte) {
	at := &attachment{
		name:  name,
		mime:  "image/png",
		image: pi.Image{Type: "image", Data: encodeBase64(png), MimeType: "image/png"},
	}
	if bm, err := ui.DecodeBitmap(png); err == nil {
		at.bitmap = bm
	}
	s.attach = append(s.attach, at)
	s.focusComposer = true
}

// --- status bar ------------------------------------------------------------

func (s *session) statusBar(c *ui.Context, t *ui.Theme) {
	ui.Row(c.Key("status")).Padding(4, 24, 12).Gap(16).AlignItems(ui.Center).Children(func() {
		statusItem(c, t, iconFolder, filepath.Base(s.workdir))
		statusItem(c, t, iconMonitor, "Local")
		if s.state.Model != nil {
			statusItem(c, t, iconBranch, s.state.Model.Provider)
		}
		ui.Spacer(c)
		if s.running {
			ui.Spinner(c)
		}
		if pct, ok := s.contextPercent(); ok {
			ring := contextRing(c, t, pct)
			ui.TooltipBase(c, ring, func(tip ui.Element) {
				tip.AttachTo(ring, ui.AnchorTopRight, ui.AnchorBottomRight).Margin(0, 0, 8, 0).
					Padding(12, 14).Radius(12).Background(t.Background).Border(1, t.Border).
					Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.12)).MinWidth(220)
				tip.Children(func() { usageTip(c, t, s, pct) })
			})
		}
	})
}

// usageTip is the panel shown over the context ring: how full the context
// window is and a breakdown of the tokens and cost.
func usageTip(c *ui.Context, t *ui.Theme, s *session, pct float64) {
	u := s.stats.Tokens
	ui.Column(c).Gap(9).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Context").FontSize(12).TextColor(t.TextMuted).Grow(1)
			ui.Text(c, fmt.Sprintf("%.0f%%", pct)).FontSize(12).Bold()
		})
		if u.Total > 0 {
			ui.Row(c).Height(8).Radius(4).Clip().AlignItems(ui.Stretch).Children(func() {
				seg := func(v int, col ui.Color) {
					if v > 0 {
						ui.Box(c).Grow(float32(v)).Background(col)
					}
				}
				seg(u.Input, t.Accent)
				seg(u.Output, t.Success)
				seg(u.CacheRead, t.Text.Alpha(0.30))
				seg(u.CacheWrite, t.Warning)
			})
		}
		ui.Column(c).Gap(4).Children(func() {
			usageRow(c, t, "Input", humanInt(u.Input), t.Accent)
			usageRow(c, t, "Output", humanInt(u.Output), t.Success)
			usageRow(c, t, "Cache read", humanInt(u.CacheRead), t.Text.Alpha(0.30))
			usageRow(c, t, "Cache write", humanInt(u.CacheWrite), t.Warning)
		})
		ui.Divider(c)
		usageTotal(c, t, "Total", humanInt(u.Total))
		usageTotal(c, t, "Cost", fmt.Sprintf("$%.4f", s.stats.Cost))
	})
}

func usageRow(c *ui.Context, t *ui.Theme, label, value string, col ui.Color) {
	ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(8, 8).Radius(2).Background(col)
		ui.Text(c, label).FontSize(11.5).TextColor(t.TextMuted).Grow(1)
		ui.Text(c, value).FontSize(11.5)
	})
}

func usageTotal(c *ui.Context, t *ui.Theme, label, value string) {
	ui.Row(c).Gap(7).Children(func() {
		ui.Text(c, label).FontSize(11.5).TextColor(t.TextMuted).Grow(1)
		ui.Text(c, value).FontSize(11.5).Bold()
	})
}

// contextRing draws the context-window usage as a circular progress ring.
func contextRing(c *ui.Context, t *ui.Theme, percent float64) ui.Element {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	e := ui.Box(c).Size(18, 18)
	e.Draw(func(p *ui.Painter, r ui.Rect) {
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		rad := r.W/2 - 2
		var bg ui.Path
		arcPath(&bg, cx, cy, rad, 0, 1)
		p.StrokePath(&bg, 3, t.Text.Alpha(0.15))
		if percent > 0 {
			var arc ui.Path
			arcPath(&arc, cx, cy, rad, 0, float32(percent/100))
			p.StrokePath(&arc, 3, t.Accent)
		}
	})
	return e
}

// arcPath traces an arc of the circle centered at (cx, cy) with radius rad,
// from the top clockwise, spanning the fraction span of a full turn.
func arcPath(path *ui.Path, cx, cy, rad, start, span float32) {
	if span <= 0 {
		return
	}
	n := int(72*span) + 1
	for i := 0; i <= n; i++ {
		ang := float64(start+span*float32(i)/float32(n))*2*math.Pi - math.Pi/2
		x := cx + rad*float32(math.Cos(ang))
		y := cy + rad*float32(math.Sin(ang))
		if i == 0 {
			path.MoveTo(x, y)
		} else {
			path.LineTo(x, y)
		}
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// --- shared widgets --------------------------------------------------------

func iconButton(c *ui.Context, key string, svg *ui.SVG, label string, size float32, onClick func()) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c.Key(key)).Padding(t.Space(1)).Radius(t.Space(1)).
		Label(label).Tooltip(label).TextColor(t.TextMuted).Shrink(0).
		Cursor(ui.CursorPointer).OnClick(onClick)
	if b.Hovered() {
		b.Background(t.SurfaceHover).TextColor(t.Text)
	}
	b.Children(func() { ui.Icon(c, svg).FontSize(size) })
	return b
}

func sideRow(c *ui.Context, t *ui.Theme, key string, svg *ui.SVG, label string, onClick func()) {
	e := ui.ButtonBase(c.Key(key)).FillWidth().Padding(8, 9).Gap(10).Radius(8).
		Justify(ui.Start).AlignItems(ui.Center).Cursor(ui.CursorPointer).OnClick(onClick)
	e.Children(func() {
		ui.Icon(c, svg).FontSize(15).TextColor(t.Text)
		ui.Text(c, label).FontSize(13.5)
	})
}

// sideNavRow is a sidebar row that selects a page, highlighted while it is
// the one shown.
func sideNavRow(c *ui.Context, t *ui.Theme, key string, svg *ui.SVG, label string, selected bool, onClick func()) {
	e := ui.ButtonBase(c.Key(key)).FillWidth().Padding(8, 9).Gap(10).Radius(8).
		Justify(ui.Start).AlignItems(ui.Center).Cursor(ui.CursorPointer).OnClick(onClick)
	if e.Hovered() {
		e.Background(t.SurfaceHover)
	}
	if selected {
		e.Background(t.SurfacePressed)
	}
	e.Children(func() {
		ui.Icon(c, svg).FontSize(15).TextColor(t.Text)
		ui.Text(c, label).FontSize(13.5)
	})
}

// chip is a borderless pill with a label and chevron that opens a menu, and
// an optional leading icon.
func chip(c *ui.Context, key string, svg *ui.SVG, label string, build func(m *ui.Menu)) ui.Element {
	t := c.Theme()
	e := ui.ButtonBase(c.Key(key)).Padding(5, 9).Gap(6).Radius(9).
		AlignItems(ui.Center).Cursor(ui.CursorPointer)
	if e.Hovered() {
		e.Background(t.SurfaceHover)
	}
	e.Children(func() {
		if svg != nil {
			ui.Icon(c, svg).FontSize(14).TextColor(t.Text)
		}
		ui.Text(c, label).FontSize(12.5).TextColor(t.Text)
		ui.Icon(c, iconChevron).FontSize(10).TextColor(t.TextMuted)
	})
	e.Menu(build)
	return e
}

func roundButton(c *ui.Context, key string, svg *ui.SVG, bg, fg ui.Color, onClick func()) ui.Element {
	e := ui.ButtonBase(c.Key(key)).Size(30, 30).Radius(15).AlignItems(ui.Center).
		Justify(ui.Center).Background(bg).Cursor(ui.CursorPointer).OnClick(onClick)
	e.Children(func() { ui.Icon(c, svg).FontSize(15).TextColor(fg) })
	return e
}

func statusItem(c *ui.Context, t *ui.Theme, svg *ui.SVG, label string) {
	ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, svg).FontSize(12.5).TextColor(t.TextMuted)
		ui.Text(c, label).FontSize(11.5).TextColor(t.TextMuted).SingleLine()
	})
}

// --- dialogs ---------------------------------------------------------------

func (a *app) savedDialog(c *ui.Context, t *ui.Theme) {
	if !a.showSaved {
		return
	}
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(560).Gap(10).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "Resume session").Bold().FontSize(16).Grow(1)
				if ui.Button(c, "Cancel").Clicked() {
					a.showSaved = false
				}
			})
			switch {
			case a.loadingSaved:
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c)
					ui.Text(c, "Loading sessions…").TextColor(t.TextMuted)
				})
			case a.savedErr != "":
				ui.Text(c, a.savedErr).TextColor(t.Danger)
			case len(a.saved) == 0:
				ui.Text(c, "No saved sessions found in "+piSessionDir()).TextColor(t.TextMuted)
			default:
				a.savedList.Selected = &a.savedSelected
				lst := ui.List(c, &a.savedList, len(a.saved), func(i int) {
					s := a.saved[i]
					ui.Column(c).Padding(6, 8).Gap(2).Children(func() {
						title := s.name
						if title == "" {
							title = s.title
						}
						if title == "" {
							title = s.id
						}
						if title == "" {
							title = s.rel
						}
						ui.Text(c, title).SingleLine()
						meta := s.cwd
						if meta == "" {
							meta = s.rel
						}
						if !s.mtime.IsZero() {
							meta += " · " + timeAgo(s.mtime)
						}
						ui.Text(c, meta).FontSize(11).TextColor(t.TextMuted).SingleLine()
					})
				}).Height(380).Grow(1)
				if lst.Submitted() && a.savedSelected >= 0 && a.savedSelected < len(a.saved) {
					s := a.saved[a.savedSelected]
					a.showSaved = false
					a.addSession(s.cwd, s.path)
				}
			}
		})
	})
	if !open {
		a.showSaved = false
	}
}

func (a *app) infoDialog(c *ui.Context, t *ui.Theme) {
	if !a.showInfo {
		return
	}
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(480).Gap(9).Children(func() {
			ui.Text(c, "Session").Bold().FontSize(16)
			if s := a.activeSession(); s != nil {
				infoRow(c, t, "Title", s.displayName())
				if s.state.Model != nil {
					infoRow(c, t, "Model", s.state.Model.Label())
				}
				infoRow(c, t, "Thinking", s.state.ThinkingLevel)
				infoRow(c, t, "Session", shortID(s.state.SessionID))
				infoRow(c, t, "Folder", s.workdir)
				infoRow(c, t, "Tokens", humanInt(s.stats.Tokens.Total))
				infoRow(c, t, "Cost", fmt.Sprintf("$%.4f", s.stats.Cost))
				if u := s.stats.ContextUsage; u != nil && u.Percent != nil {
					infoRow(c, t, "Context", fmt.Sprintf("%.0f%%", *u.Percent))
				}
			} else {
				ui.Text(c, "No session").TextColor(t.TextMuted)
			}
			ui.Row(c).Justify(ui.End).Children(func() {
				if ui.Button(c, "Close").Clicked() {
					a.showInfo = false
				}
			})
		})
	})
	if !open {
		a.showInfo = false
	}
}

func infoRow(c *ui.Context, t *ui.Theme, label, value string) {
	ui.Row(c).Gap(12).AlignItems(ui.Start).Children(func() {
		ui.Text(c, label).FontSize(12.5).TextColor(t.TextMuted).Width(76)
		ui.Text(c, value).FontSize(12.5).Selectable().Grow(1)
	})
}

func (s *session) extDialog(c *ui.Context, t *ui.Theme) {
	if s.ext == nil {
		return
	}
	ext := s.ext
	req := ext.req
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(460).Gap(10).Children(func() {
			title := req.Title
			if title == "" {
				title = "Pi"
			}
			ui.Text(c, title).Bold().FontSize(16)
			submitted := false
			switch req.Method {
			case "confirm":
				if req.Message != "" {
					ui.Text(c, req.Message).Selectable()
				}
			case "select":
				if req.Message != "" {
					ui.Text(c, req.Message).TextColor(t.TextMuted)
				}
				for _, opt := range req.Options {
					opt := opt
					if ui.Button(c, opt).Clicked() {
						s.respondExt(map[string]any{"value": opt})
					}
				}
			case "input":
				submitted = ui.TextInput(c, &ext.value).Placeholder(req.Placeholder).AutoFocus().Submitted()
			case "editor":
				ui.TextArea(c, &ext.value).Height(220).AutoFocus()
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "Cancel").Clicked() {
					s.respondExt(map[string]any{"cancelled": true})
				}
				switch req.Method {
				case "confirm":
					if ui.PrimaryButton(c, "OK").Clicked() {
						s.respondExt(map[string]any{"confirmed": true})
					}
				case "input", "editor":
					value := ext.value
					if ui.PrimaryButton(c, "OK").Clicked() {
						s.respondExt(map[string]any{"value": value})
					}
				}
			})
			if submitted && s.ext != nil {
				s.respondExt(map[string]any{"value": ext.value})
			}
		})
	})
	if !open && s.ext != nil {
		s.respondExt(map[string]any{"cancelled": true})
	}
}

// --- small helpers ---------------------------------------------------------

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func shortAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func humanDuration(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	switch {
	case d < time.Second:
		return "less than a second"
	case d < time.Minute:
		s := int(d.Seconds())
		if s == 1 {
			return "1 second"
		}
		return fmt.Sprintf("%d seconds", s)
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", m)
	default:
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	}
}
