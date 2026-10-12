//go:build !darwin

package main

import "github.com/egoist/mygo/ui"

// osFileIcon is unavailable off macOS.
func osFileIcon(string) *ui.Bitmap { return nil }
