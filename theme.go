package main

import "github.com/egoist/mygo/ui"

// customTheme is the app's palette: a light, near-monochrome look in the
// manner of a modern macOS agent app, with a matching dark appearance.
func customTheme(dark bool) *ui.Theme {
	if dark {
		return &ui.Theme{
			Dark:           true,
			Background:     ui.Hex("#1c1c1e"),
			Surface:        ui.Hex("#2c2c2e"),
			SurfaceHover:   ui.Hex("#3a3a3c"),
			SurfacePressed: ui.Hex("#48484a"),
			Border:         ui.Hex("#3a3a3c"),
			Text:           ui.Hex("#f2f2f7"),
			TextMuted:      ui.Hex("#98989f"),
			Accent:         ui.Hex("#f2f2f7"),
			AccentHover:    ui.Hex("#ffffff"),
			AccentPressed:  ui.Hex("#d1d1d6"),
			AccentText:     ui.Hex("#1c1c1e"),
			Danger:         ui.Hex("#ff453a"),
			Warning:        ui.Hex("#ff9f0a"),
			Success:        ui.Hex("#32d74b"),
			Selection:      ui.RGBA(255, 255, 255, 0.20),
			Focus:          ui.RGBA(255, 255, 255, 0.40),
			Inverse:        ui.Hex("#f2f2f7"),
			InverseText:    ui.Hex("#1c1c1e"),
			Scrollbar:      ui.RGBA(255, 255, 255, 0.22),
			ScrollbarWidth: 8,
			Radius:         10,
			Spacing:        4,
			FontSize:       13,
		}
	}
	return &ui.Theme{
		Background:     ui.Hex("#ffffff"),
		Surface:        ui.Hex("#f5f5f7"),
		SurfaceHover:   ui.Hex("#ececef"),
		SurfacePressed: ui.Hex("#e3e3e8"),
		Border:         ui.Hex("#e5e5ea"),
		Text:           ui.Hex("#1c1c1e"),
		TextMuted:      ui.Hex("#8a8a8e"),
		Accent:         ui.Hex("#1c1c1e"),
		AccentHover:    ui.Hex("#2c2c2e"),
		AccentPressed:  ui.Hex("#000000"),
		AccentText:     ui.Hex("#ffffff"),
		Danger:         ui.Hex("#ff3b30"),
		Warning:        ui.Hex("#ff9500"),
		Success:        ui.Hex("#34c759"),
		Selection:      ui.RGBA(0, 0, 0, 0.12),
		Focus:          ui.RGBA(0, 0, 0, 0.35),
		Inverse:        ui.Hex("#1c1c1e"),
		InverseText:    ui.Hex("#ffffff"),
		Scrollbar:      ui.RGBA(0, 0, 0, 0.20),
		ScrollbarWidth: 8,
		Radius:         10,
		Spacing:        4,
		FontSize:       13,
	}
}

// sidebarBg is the sidebar's fill (the content beside it is Background).
func sidebarBg(t *ui.Theme) ui.Color { return t.Surface }

// chipBg fills the small pills of the composer and the inline code runs.
func chipBg(t *ui.Theme) ui.Color {
	if t.Dark {
		return ui.RGBA(255, 255, 255, 0.09)
	}
	return ui.RGBA(0, 0, 0, 0.055)
}

// userBubble is the fill of the user's message bubble.
func userBubble(t *ui.Theme) ui.Color {
	if t.Dark {
		return ui.RGBA(255, 255, 255, 0.09)
	}
	return ui.Hex("#f0f0f2")
}

// codeBg is the background of code blocks.
func codeBg(t *ui.Theme) ui.Color {
	if t.Dark {
		return ui.Hex("#232326")
	}
	return ui.Hex("#f6f6f8")
}

// syntax is the set of colors the code highlighter uses.
type syntaxSet struct {
	keyword ui.Color
	str     ui.Color
	comment ui.Color
	number  ui.Color
}

func syntaxColors(dark bool) syntaxSet {
	if dark {
		return syntaxSet{
			keyword: ui.Hex("#c792ea"),
			str:     ui.Hex("#a5e075"),
			comment: ui.Hex("#7c7c82"),
			number:  ui.Hex("#f78c6c"),
		}
	}
	return syntaxSet{
		keyword: ui.Hex("#8b40c8"),
		str:     ui.Hex("#2e7d32"),
		comment: ui.Hex("#9a9aa0"),
		number:  ui.Hex("#c2410c"),
	}
}
