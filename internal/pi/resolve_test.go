package pi

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestResolveExplicitPath(t *testing.T) {
	got, err := Resolve("/bin/sh")
	if err != nil {
		t.Fatalf("Resolve(/bin/sh): %v", err)
	}
	if got != "/bin/sh" {
		t.Fatalf("Resolve(/bin/sh) = %q", got)
	}
}

func TestResolveMissing(t *testing.T) {
	if _, err := Resolve("pine-nonexistent-binary-zzz"); err == nil {
		t.Fatal("expected an error for a missing binary")
	}
}

func TestResolveSearchesExtraDirs(t *testing.T) {
	home := t.TempDir()
	nvmBin := filepath.Join(home, ".nvm", "versions", "node", "v20.0.0", "bin")
	if err := os.MkdirAll(nvmBin, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(nvmBin, "pine-test-pi")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	got, err := Resolve("pine-test-pi")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != bin {
		t.Fatalf("Resolve = %q, want %q", got, bin)
	}
}

func TestFallbackCandidatesNewestFirst(t *testing.T) {
	home := t.TempDir()
	for _, version := range []string{"v18.0.0", "v22.0.0"} {
		if err := os.MkdirAll(filepath.Join(home, ".nvm", "versions", "node", version), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)

	candidates := fallbackCandidates("x")
	newer := filepath.Join(home, ".nvm", "versions", "node", "v22.0.0", "bin", "x")
	older := filepath.Join(home, ".nvm", "versions", "node", "v18.0.0", "bin", "x")
	iNewer, iOlder := indexOf(candidates, newer), indexOf(candidates, older)
	if iNewer < 0 || iOlder < 0 {
		t.Fatalf("candidates = %v, want both %q and %q", candidates, newer, older)
	}
	if iNewer > iOlder {
		t.Fatalf("candidates = %v, want %q before %q", candidates, newer, older)
	}
	if indexOf(candidates, filepath.Join(home, ".local", "bin", "x")) < 0 {
		t.Fatalf("candidates = %v, want a ~/.local/bin entry", candidates)
	}
}

func TestVersionsNewestFirstNumeric(t *testing.T) {
	root := t.TempDir()
	for _, version := range []string{"v9.11.2", "v10.0.0", "v18.20.4", "v22.21.0"} {
		if err := os.MkdirAll(filepath.Join(root, version), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got := versionsNewestFirst(root)
	want := []string{"v22.21.0", "v18.20.4", "v10.0.0", "v9.11.2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("versionsNewestFirst = %v, want %v", got, want)
	}
}

func TestLastAbsPath(t *testing.T) {
	cases := map[string]string{
		"Welcome to zsh!\n/opt/homebrew/bin/pi\n": "/opt/homebrew/bin/pi",
		"/usr/local/bin/pi":                       "/usr/local/bin/pi",
		"Welcome!\nnot a path\n":                  "",
		"":                                        "",
	}
	for in, want := range cases {
		if got := lastAbsPath(in); got != want {
			t.Errorf("lastAbsPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestJoinUnique(t *testing.T) {
	if got := joinUnique("/a:/b", "/b:/c", ""); got != "/a:/b:/c" {
		t.Fatalf("joinUnique = %q, want /a:/b:/c", got)
	}
}

func indexOf(items []string, want string) int {
	for i, item := range items {
		if item == want {
			return i
		}
	}
	return -1
}
