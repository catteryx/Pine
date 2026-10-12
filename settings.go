package main

import (
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
	FontSize   float32 `json:"fontSize,omitempty"`
}

// sessionArgs returns the extra pi arguments for a session from settings.
func (s settings) sessionArgs(resume string) []string {
	var args []string
	if s.SessionDir != "" {
		args = append(args, "--session-dir", s.SessionDir)
	}
	if resume != "" {
		args = append(args, "--session", resume)
	}
	if s.Provider != "" && s.Model != "" {
		args = append(args, "--provider", s.Provider, "--model", s.Model)
	}
	if s.Thinking != "" {
		args = append(args, "--thinking", s.Thinking)
	}
	return args
}

// applySettings makes the settings take effect and saves them.
func (a *app) applySettings() {
	if a.settings.PiBin != "" {
		a.piPath = a.settings.PiBin
	}
	a.saveState()
}

func (a *app) settingsDialog(c *ui.Context, t *ui.Theme) {
	if !a.showSettings {
		return
	}
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(520).Gap(10).Children(func() {
			ui.Text(c, "Settings").Bold().FontSize(16)
			ui.Text(c, "Applied to new sessions.").FontSize(11.5).TextColor(t.TextMuted)
			settingField(c, t, "Provider", &a.settings.Provider, "anthropic")
			settingField(c, t, "Model", &a.settings.Model, "claude-sonnet-4")
			ui.Text(c, "Thinking").FontSize(12).TextColor(t.TextMuted)
			ui.Select(c, &a.settings.Thinking, []string{"", "off", "minimal", "low", "medium", "high", "xhigh", "max"})
			settingField(c, t, "Session directory", &a.settings.SessionDir, "~/.pi/agent/sessions")
			settingField(c, t, "Pi binary", &a.settings.PiBin, "pi")
			ui.Text(c, "Font size").FontSize(12).TextColor(t.TextMuted)
			fs := float64(a.settings.FontSize)
			if fs == 0 {
				fs = 13
			}
			if ui.Slider(c, &fs, 11, 18).Changed() {
				a.settings.FontSize = float32(fs)
			}
			ui.Row(c).Justify(ui.End).Children(func() {
				if ui.Button(c, "Close").Clicked() {
					a.showSettings = false
					a.applySettings()
				}
			})
		})
	})
	if !open {
		a.showSettings = false
		a.applySettings()
	}
}

func settingField(c *ui.Context, t *ui.Theme, label string, val *string, placeholder string) {
	ui.Text(c, label).FontSize(12).TextColor(t.TextMuted)
	ui.TextInput(c, val).Placeholder(placeholder)
}
