package main

import (
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
)

// fileIcon returns a file's native icon, cached by its extension. It loads
// on a first use; call it off the main thread.
func (a *app) fileIcon(path string) *ui.Bitmap {
	ext := strings.ToLower(filepath.Ext(path))
	a.iconMu.Lock()
	if a.fileIcons == nil {
		a.fileIcons = map[string]*ui.Bitmap{}
	}
	cached, ok := a.fileIcons[ext]
	a.iconMu.Unlock()
	if ok {
		return cached
	}
	icon := osFileIcon(path)
	a.iconMu.Lock()
	a.fileIcons[ext] = icon
	a.iconMu.Unlock()
	return icon
}
