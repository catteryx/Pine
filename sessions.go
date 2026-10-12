package main

import (
	"bufio"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// sessionInfo describes one saved Pi session file, for the resume picker.
type sessionInfo struct {
	path  string
	rel   string
	id    string
	cwd   string
	name  string
	title string
	mtime time.Time
}

// piSessionDir returns the directory Pi stores sessions in.
func piSessionDir() string {
	if dir := os.Getenv("PI_CODING_AGENT_SESSION_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".pi", "agent", "sessions")
}

// scanSessions lists saved sessions, most recent first.
func scanSessions() ([]sessionInfo, error) {
	root := piSessionDir()
	if root == "" {
		return nil, nil
	}
	var out []sessionInfo
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		s := sessionInfo{path: path, mtime: info.ModTime()}
		if rel, err := filepath.Rel(root, path); err == nil {
			s.rel = rel
		}
		readSessionMeta(path, &s)
		out = append(out, s)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].mtime.After(out[j].mtime) })
	return out, nil
}

// sessionStartTime returns when a saved Pi session began, taken from the
// timestamp on its first record. It returns the zero time when the file is
// missing or unreadable.
func sessionStartTime(path string) time.Time {
	if path == "" {
		return time.Time{}
	}
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	if !sc.Scan() {
		return time.Time{}
	}
	var rec struct {
		Timestamp string `json:"timestamp"`
	}
	if json.Unmarshal(sc.Bytes(), &rec) != nil {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, rec.Timestamp)
	if err != nil {
		return time.Time{}
	}
	return t
}

// readSessionMeta reads the session header and the first user message for a
// display name and title.
func readSessionMeta(path string, s *sessionInfo) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	first := true
	for sc.Scan() {
		line := sc.Bytes()
		if first {
			first = false
			var header struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Cwd  string `json:"cwd"`
			}
			if json.Unmarshal(line, &header) == nil {
				s.id = header.ID
				s.cwd = header.Cwd
			}
		}
		if s.title != "" && s.name != "" {
			return
		}
		if s.title == "" && strings.Contains(string(line), `"role":"user"`) {
			var rec struct {
				Message struct {
					Role    string          `json:"role"`
					Content json.RawMessage `json:"content"`
				} `json:"message"`
			}
			if json.Unmarshal(line, &rec) == nil && rec.Message.Role == "user" {
				s.title = firstLine(truncate(textOf(rec.Message.Content), 90))
			}
		}
	}
}

func textOf(content json.RawMessage) string {
	if len(content) == 0 {
		return ""
	}
	var str string
	if json.Unmarshal(content, &str) == nil {
		return str
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(content, &blocks) == nil {
		for _, b := range blocks {
			if b.Type == "text" {
				return b.Text
			}
		}
	}
	return ""
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
