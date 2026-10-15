// Command pine is a desktop client for the Pi coding agent
// (github.com/earendil-works/pi, v1.0+), built with MyGo's native UI. It
// drives one `pi --mode rpc` process per session and shows the sessions in a
// full-height sidebar, with the active conversation beside it.
package main

import (
	_ "embed"
	"log"
	"os"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	updaternative "github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
)

//go:embed resources/icon.png
var appIcon []byte

func main() {
	// The update window in native UI: signed updates from GitHub releases.
	mygo.Use(updaternative.Plugin)
	a := newApp()
	if p := os.Getenv("PI_BIN"); p != "" {
		a.piPath = p
	}
	mygo.App.WhenReady(func() {
		_ = mygo.App.Dock.SetIcon(appIcon)
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title:         "Pine",
			Width:         1120,
			Height:        800,
			MinWidth:      720,
			MinHeight:     520,
			StateKey:      "main",
			TitleBarStyle: mygo.TitleBarHidden,
			Content:       ui.View(a.view),
		})
		a.win.OnClosed(a.shutdown)
		a.win.OnFocus(func() { a.focused = true; a.clearUnread() })
		a.win.OnBlur(func() { a.focused = false })
		a.setMenu()
		a.restore()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func (a *app) setMenu() {
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Label: "Pine", Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout},
			updater.MenuItem(),
			mygo.Separator(),
			{Label: "Settings…", Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) {
				a.openSettings()
			}},
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		}},
		{Role: mygo.RoleEditMenu},
		{Label: "Session", Submenu: []*mygo.MenuItem{
			{Label: "New Session", Accelerator: "CmdOrCtrl+N", Click: func(*mygo.MenuItem, *mygo.Window) {
				a.newSessionTab()
			}},
			{Label: "Resume Session…", Accelerator: "CmdOrCtrl+O", Click: func(*mygo.MenuItem, *mygo.Window) {
				a.openSaved()
			}},
			mygo.Separator(),
			{Label: "Open Project Folder…", Click: func(*mygo.MenuItem, *mygo.Window) {
				go a.openFolder()
			}},
			{Label: "Close Session", Accelerator: "CmdOrCtrl+W", Click: func(*mygo.MenuItem, *mygo.Window) {
				a.closeSession(a.active)
			}},
		}},
		{Role: mygo.RoleViewMenu},
		{Role: mygo.RoleWindowMenu},
	}))
}
