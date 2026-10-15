package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// imageLine parses a line that is just an image: "![alt](src)". A line with
// other text around the image is a paragraph instead.
func imageLine(s string) (alt, src string, ok bool) {
	if !strings.HasPrefix(s, "![") {
		return "", "", false
	}
	label, url, next, ok := linkParts(s, 1)
	if !ok || strings.TrimSpace(s[next:]) != "" {
		return "", "", false
	}
	return label, url, true
}

// renderImage shows a Markdown image at its size, capped to the column, or a
// link to it until a remote one loads.
func (a *app) renderImage(c *ui.Context, t *ui.Theme, alt, src string) {
	if bm := a.markdownImage(src); bm != nil {
		w, h := bm.Size()
		dw, dh := fitSize(float64(w), float64(h), 520, 520)
		ui.Column(c).AlignItems(ui.Start).Children(func() {
			ui.Image(c, bm).Size(float32(dw), float32(dh)).Radius(10)
			if alt != "" {
				ui.Text(c, alt).FontSize(11.5).TextColor(t.TextMuted).Margin(3, 0)
			}
		})
		return
	}
	label := alt
	if label == "" {
		label = src
	}
	ui.RichText(c).Children(func() {
		ui.Text(c, "🖼 ")
		ui.Link(c, label, src)
	})
}

// fitSize scales w×h to fit within maxW×maxH, keeping the aspect ratio.
func fitSize(w, h, maxW, maxH float64) (float64, float64) {
	if w <= 0 || h <= 0 {
		return maxW, maxH
	}
	scale := 1.0
	if w > maxW {
		scale = maxW / w
	}
	if h*scale > maxH {
		scale = maxH / h
	}
	return w * scale, h * scale
}

// markdownImage returns the bitmap for a Markdown image source: a local file
// is decoded at once, a remote URL is fetched in the background. It returns
// nil until the image is ready, and remembers a source it could not load.
func (a *app) markdownImage(src string) *ui.Bitmap {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil
	}
	a.mdImagesMu.Lock()
	if bm, ok := a.mdImages[src]; ok {
		a.mdImagesMu.Unlock()
		return bm
	}
	if a.mdLoading[src] {
		a.mdImagesMu.Unlock()
		return nil
	}
	if a.mdImages == nil {
		a.mdImages = map[string]*ui.Bitmap{}
	}
	if a.mdLoading == nil {
		a.mdLoading = map[string]bool{}
	}
	a.mdLoading[src] = true
	a.mdImagesMu.Unlock()

	if isRemoteURL(src) {
		go a.fetchMarkdownImage(src)
		return nil
	}
	data, err := a.readImageSource(src)
	if err != nil {
		return nil
	}
	bm, err := ui.DecodeBitmap(data)
	if err != nil {
		return nil
	}
	a.mdImagesMu.Lock()
	a.mdImages[src] = bm
	a.mdImagesMu.Unlock()
	return bm
}

// fetchMarkdownImage downloads a remote image and shows it when it arrives.
func (a *app) fetchMarkdownImage(src string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 12<<20))
	if err != nil {
		return
	}
	bm, err := ui.DecodeBitmap(data)
	if err != nil {
		return
	}
	a.mdImagesMu.Lock()
	a.mdImages[src] = bm
	a.mdImagesMu.Unlock()
	if a.win != nil {
		a.win.Invalidate()
	}
}

func isRemoteURL(src string) bool {
	return strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://")
}

// readImageSource reads a local image: a data: URL, a file:// URL, or a path
// relative to the working directory.
func (a *app) readImageSource(src string) ([]byte, error) {
	if rest, ok := strings.CutPrefix(src, "data:"); ok {
		marker := ";base64,"
		i := strings.Index(rest, marker)
		if i < 0 {
			return nil, fmt.Errorf("unsupported data URL")
		}
		return base64.StdEncoding.DecodeString(rest[i+len(marker):])
	}
	path := src
	if rest, ok := strings.CutPrefix(path, "file://"); ok {
		path = rest
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(a.workdir, path)
	}
	return os.ReadFile(path)
}
