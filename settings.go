package main

import (
	"fmt"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// settings are the client's own settings, applied to new sessions and the
// theme, and saved with the open sessions.
type settings struct {
	Provider   string  `json:"provider,omitempty"`
	Model      string  `json:"model,omitempty"`
	Thinking   string  `json:"thinking,omitempty"`
	SessionDir string  `json:"sessionDir,omitempty"`
	PiBin      string  `json:"piBin,omitempty"`
	Appearance string  `json:"appearance,omitempty"`
	FontSize   float32 `json:"fontSize,omitempty"`
}

// sessionArgs returns the extra pi arguments for a session from settings.
func (s settings) sessionArgs(resume string) []string {
	var args []string
	if s.SessionDir != "" {
		args = append(args, "--session-dir", s.SessionDir)
	}
	if resume != "" {
		// A resumed session records its own model and thinking level; let Pi
		// restore them instead of overriding them with the last-used values.
		args = append(args, "--session", resume)
		return args
	}
	if s.Provider != "" && s.Model != "" {
		args = append(args, "--provider", s.Provider, "--model", s.Model)
	}
	if s.Thinking != "" {
		args = append(args, "--thinking", s.Thinking)
	}
	return args
}

// The settings window's tabs.
const (
	settingsGeneral = iota
	settingsModel
	settingsAppearance
)

// Appearance choices; "Auto" follows the system.
const (
	appearanceAuto  = "Auto"
	appearanceDark  = "Dark"
	appearanceLight = "Light"
)

// thinkingLevels are the choices of the thinking dropdown. "Default" leaves
// the level to Pi.
var thinkingLevels = []string{"Default", "off", "minimal", "low", "medium", "high", "xhigh", "max"}

// settingsBarHeight is the height of the settings window's title bar, taller
// than the window controls' own so the tabs have room. settingsTrafficX and
// settingsTrafficY put the top-left corner of the macOS window controls where
// AppKit puts them by default, vertically centered.
const (
	settingsBarHeight = 48
	settingsTrafficX  = 9
	settingsTrafficY  = (settingsBarHeight - 14) / 2
)

func appearanceSource(appearance string) mygo.ThemeSource {
	switch appearance {
	case appearanceDark:
		return mygo.ThemeDark
	case appearanceLight:
		return mygo.ThemeLight
	}
	return mygo.ThemeSystem
}

func appearanceIndex(appearance string) int {
	switch appearance {
	case appearanceDark:
		return 1
	case appearanceLight:
		return 2
	}
	return 0
}

func appearanceAt(index int) string {
	switch index {
	case 1:
		return appearanceDark
	case 2:
		return appearanceLight
	}
	return appearanceAuto
}

// applySettings makes the settings take effect and saves them.
func (a *app) applySettings() {
	if a.settings.PiBin != "" {
		a.piPath = a.settings.PiBin
	}
	mygo.Theme.SetSource(appearanceSource(a.settings.Appearance))
	a.saveState()
	if a.win != nil {
		a.win.Invalidate()
	}
	if a.settingsWin != nil {
		a.settingsWin.Invalidate()
	}
}

// openSettings shows the settings in their own window, from the menu or a
// shortcut: it opens the window the first time, and brings it forward after.
func (a *app) openSettings() {
	mygo.RunOnMain(func() {
		if a.settingsWin == nil || a.settingsWin.IsDestroyed() {
			w := mygo.NewWindow(mygo.WindowOptions{
				Title:                "Settings",
				Width:                560,
				Height:               520,
				MinWidth:             460,
				MinHeight:            360,
				TitleBarStyle:        mygo.TitleBarHidden,
				TrafficLightPosition: &mygo.Point{X: settingsTrafficX, Y: settingsTrafficY},
				StateKey:             "settings",
				Content:              ui.View(a.settingsView),
			})
			w.OnClosed(func() {
				a.settingsWin = nil
				a.applySettings()
			})
			a.settingsWin = w
		}
		a.settingsWin.Show()
		a.settingsWin.Focus()
	})
}

// settingsView is the settings window: its tabs live in the title bar, past
// the window controls, and each chooses a group of settings. A group shows as
// rows in a panel, the name of a setting and what it does at the left and its
// control at the right, as macOS groups them.
func (a *app) settingsView(c *ui.Context) {
	c.SetTheme(a.themeFor(c.Theme().Dark))
	t := c.Theme()
	changed := false
	bar := c.TitleBar()
	// The page is a shade off the window in a light appearance, so the panel
	// reads as a card on it, as macOS draws a group of settings.
	page := t.Background
	if !t.Dark {
		page = t.Background.Mix(t.Text, 0.05)
	}
	ui.Column(c).Fill().Background(page).Children(func() {
		side := max(bar.Left, bar.Right) + t.Space(4)
		ui.Row(c).Height(max(topHeight(c), settingsBarHeight)).AlignItems(ui.Center).Justify(ui.Center).
			DragWindow().Padding(0, side, 0, side).Children(func() {
			ui.Segmented(c, &a.settingsTab, "General", "Model", "Appearance")
		})
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).Padding(t.Space(3), t.Space(5), t.Space(5), t.Space(5)).
				Gap(t.Space(3)).MaxWidth(720).Children(func() {
				switch a.settingsTab {
				case settingsModel:
					settingsGroup(c, t, func() {
						settingsRow(c, t, "Provider", "Sent to new sessions, e.g. anthropic.", func() {
							if ui.TextInput(c, &a.settings.Provider).Placeholder("anthropic").Width(200).Changed() {
								changed = true
							}
						})
						settingsRow(c, t, "Model", "The model new sessions start with.", func() {
							if ui.TextInput(c, &a.settings.Model).Placeholder("claude-sonnet-4").Width(200).Changed() {
								changed = true
							}
						})
						settingsRow(c, t, "Thinking", "The reasoning level new sessions start with.", func() {
							level := a.settings.Thinking
							if level == "" {
								level = "Default"
							}
							if ui.Combobox(c, &level, thinkingLevels).Width(160).Changed() {
								if level == "Default" {
									level = ""
								}
								a.settings.Thinking = level
								changed = true
							}
						})
					})
				case settingsAppearance:
					settingsGroup(c, t, func() {
						settingsRow(c, t, "Appearance", "Light, dark, or follow the system.", func() {
							index := appearanceIndex(a.settings.Appearance)
							if ui.Segmented(c, &index, appearanceAuto, appearanceDark, appearanceLight).Changed() {
								a.settings.Appearance = appearanceAt(index)
								changed = true
							}
						})
						settingsRow(c, t, "Font size", "The size of text in the app.", func() {
							size := float64(a.settings.FontSize)
							if size == 0 {
								size = 13
							}
							ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
								if ui.Slider(c, &size, 11, 18).Width(160).Changed() {
									a.settings.FontSize = float32(size)
									changed = true
								}
								ui.Text(c, fmt.Sprintf("%.0f", size)).FontSize(12).TextColor(t.TextMuted).Width(22)
							})
						})
					})
				default:
					settingsGroup(c, t, func() {
						settingsRow(c, t, "Pi binary", "The pi CLI Pine runs. Leave empty to find it automatically.", func() {
							if ui.TextInput(c, &a.settings.PiBin).Placeholder("pi").Width(240).Changed() {
								changed = true
							}
						})
						settingsRow(c, t, "Session directory", "Where Pi keeps its sessions. Leave empty for Pi's default.", func() {
							if ui.TextInput(c, &a.settings.SessionDir).Placeholder("~/.pi/agent/sessions").Width(240).Changed() {
								changed = true
							}
						})
					})
				}
			})
		})
	})
	if changed {
		a.applySettings()
	}
}

// settingsGroup is a panel holding settings rows, on the theme's surface, with
// a line between each two: the look macOS gives a group of settings.
func settingsGroup(c *ui.Context, t *ui.Theme, fn func()) {
	face := t.Surface
	if !t.Dark {
		face = t.Background
	}
	ui.Column(c).Shrink(0).Gap(t.Space(0.5)).Radius(t.Radius+4).
		Background(face).Border(1, t.Border).Clip().
		Dividers(1, t.Border).Children(fn)
}

// settingsRow is one row of a settings group: the name of the setting and,
// below it, what it does, at the left, growing, and its control at the right,
// which control builds.
func settingsRow(c *ui.Context, t *ui.Theme, title, desc string, control func()) {
	ui.Row(c).AlignItems(ui.Center).Gap(t.Space(4)).Padding(t.Space(3), t.Space(4)).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).Gap(t.Space(0.5)).Children(func() {
			ui.Text(c, title).FontWeight(500).SingleLine()
			if desc != "" {
				ui.Text(c, desc).FontSize(t.FontSize - 1).TextColor(t.TextMuted)
			}
		})
		ui.Row(c).Shrink(0).AlignItems(ui.Center).Justify(ui.End).Children(control)
	})
}
