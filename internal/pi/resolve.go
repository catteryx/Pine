package pi

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Resolve returns the absolute path to the pi executable.
//
// A path containing a separator is used as given. Otherwise it is looked up on
// PATH; for the default "pi" name it then asks the user's login shell, and
// finally scans the usual install locations, verifying each candidate with
// `pi --version`. Apps launched from Finder get a minimal PATH that omits nvm,
// Homebrew and other user directories, so those two fallbacks are what let the
// app find pi without any extra configuration.
func Resolve(path string) (string, error) {
	if path == "" {
		path = "pi"
	}
	if strings.ContainsRune(path, filepath.Separator) {
		resolved, err := exec.LookPath(path)
		if err != nil {
			return "", fmt.Errorf("pi not found: %w", err)
		}
		return resolved, nil
	}
	if path == "pi" {
		resolveOnce.Do(func() { resolvePath, resolveErr = discoverPi() })
		return resolvePath, resolveErr
	}
	if resolved, err := exec.LookPath(path); err == nil {
		return resolved, nil
	}
	for _, candidate := range fallbackCandidates(path) {
		if isExecutable(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("pi not found: exec: %q: executable file not found in $PATH", path)
}

var (
	resolveOnce sync.Once
	resolvePath string
	resolveErr  error
)

// discoverPi resolves the default "pi" name: the login shell first, then PATH,
// then the fallback locations. The login shell is authoritative because pi may
// be installed through npm, bun, Homebrew, or a version manager.
func discoverPi() (string, error) {
	if located := shellWhich("pi"); located != "" && verify(located) {
		return located, nil
	}
	if located, err := exec.LookPath("pi"); err == nil {
		return located, nil
	}
	for _, candidate := range fallbackCandidates("pi") {
		if isExecutable(candidate) && verify(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("pi not found: exec: %q: executable file not found in $PATH", "pi")
}

// NewCommand builds an exec.Cmd for pi, resolving path the same way Start
// does and setting the child's PATH so pi's Node shebang finds node.
func NewCommand(path string, args ...string) (*exec.Cmd, error) {
	resolved, err := Resolve(path)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(resolved, args...)
	cmd.Env = childEnv(nil, resolved)
	return cmd, nil
}

// verify runs `<exe> --version` and reports whether it answers. A short name
// like `pi` is easy to collide with (a stale Python launcher, say), so a
// candidate is only accepted once it actually runs.
func verify(exe string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "--version")
	cmd.Env = childEnv(nil, exe)
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// shellWhich asks the user's login shell where a binary is. The interactive
// (-i) form is tried first because version managers such as nvm are usually
// loaded from ~/.zshrc, which a non-interactive login shell skips.
func shellWhich(binary string) string {
	shell := loginShell()
	for _, flag := range []string{"-lic", "-lc"} {
		if out, err := runShell(shell, flag, "command -v "+binary); err == nil {
			if path := lastAbsPath(out); path != "" {
				return path
			}
		}
	}
	return ""
}

// ShellPath returns the PATH of the user's login shell, or "" when it cannot
// be determined. It is cached, so the shell is only started once.
func ShellPath() string {
	shellPathOnce.Do(func() { shellPathValue = readShellPath() })
	return shellPathValue
}

var (
	shellPathOnce  sync.Once
	shellPathValue string
)

func readShellPath() string {
	shell := loginShell()
	for _, flag := range []string{"-lic", "-lc"} {
		if out, err := runShell(shell, flag, `printf %s "$PATH"`); err == nil {
			if path := strings.TrimSpace(out); path != "" {
				return path
			}
		}
	}
	return ""
}

func loginShell() string {
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell
	}
	return "/bin/zsh"
}

func runShell(shell, flag, script string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, flag, script)
	out, err := cmd.Output()
	return string(out), err
}

// lastAbsPath returns the last non-empty line when it is an absolute path,
// since an interactive shell may print a banner before the answer.
func lastAbsPath(out string) string {
	lines := strings.Split(out, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "/") {
			return line
		}
		return ""
	}
	return ""
}

// fallbackCandidates lists candidate executables for binary when PATH and the
// login shell cannot answer: the usual package-manager locations, every
// directory the login shell reported, then the version managers (newest
// version first).
func fallbackCandidates(binary string) []string {
	home, _ := os.UserHomeDir()
	var candidates []string
	if binary == "pi" {
		candidates = append(candidates, "/opt/homebrew/bin/pi", "/usr/local/bin/pi", "/usr/bin/pi")
	}
	if home != "" {
		candidates = append(candidates,
			filepath.Join(home, ".bun", "bin", binary),
			filepath.Join(home, ".local", "bin", binary),
			filepath.Join(home, ".npm-global", "bin", binary),
			filepath.Join(home, ".volta", "bin", binary),
			filepath.Join(home, ".yarn", "bin", binary),
			filepath.Join(home, ".config", "yarn", "global", "node_modules", ".bin", binary),
			filepath.Join(home, "Library", "pnpm", binary),
			filepath.Join(home, ".local", "share", "mise", "shims", binary),
			filepath.Join(home, ".asdf", "shims", binary),
		)
	}
	if binary == "pi" {
		for _, dir := range filepath.SplitList(ShellPath()) {
			if dir != "" {
				candidates = append(candidates, filepath.Join(dir, binary))
			}
		}
	}
	if home != "" {
		for _, tc := range versionedToolchains(home) {
			for _, version := range versionsNewestFirst(tc.dir) {
				candidates = append(candidates, filepath.Join(tc.dir, version, tc.suffix, binary))
			}
		}
	}
	return dedupe(candidates)
}

// toolchain is a directory laid out as root/<version>/<suffix>.
type toolchain struct{ dir, suffix string }

func versionedToolchains(home string) []toolchain {
	if home == "" {
		return nil
	}
	return []toolchain{
		{filepath.Join(home, ".nvm", "versions", "node"), "bin"},
		{filepath.Join(home, "Library", "Application Support", "fnm", "node-versions"), filepath.Join("installation", "bin")},
	}
}

// preferredNodeBin returns the directory of the newest Node runtime the user
// has, used after pi's own directory so a stale shell Node cannot shadow the
// one pi was installed with.
func preferredNodeBin() string {
	home, _ := os.UserHomeDir()
	for _, tc := range versionedToolchains(home) {
		for _, version := range versionsNewestFirst(tc.dir) {
			dir := filepath.Join(tc.dir, version, tc.suffix)
			if isExecutable(filepath.Join(dir, "node")) {
				return dir
			}
		}
	}
	for _, dir := range []string{
		filepath.Join(home, ".volta", "bin"),
		"/opt/homebrew/opt/node/bin",
		"/usr/local/opt/node/bin",
	} {
		if isExecutable(filepath.Join(dir, "node")) {
			return dir
		}
	}
	return ""
}

// childEnv returns the environment for a pi subprocess. PATH puts the
// executable's own directory first, then the preferred Node runtime, then the
// login-shell PATH, so pi's "#!/usr/bin/env node" shebang finds the Node it was
// installed with. Nothing outside the child's environment is changed.
func childEnv(base []string, exe string) []string {
	if base == nil {
		base = os.Environ()
	}
	var basePath string
	env := make([]string, 0, len(base)+1)
	for _, kv := range base {
		if strings.HasPrefix(kv, "PATH=") {
			basePath = kv[len("PATH="):]
			continue
		}
		env = append(env, kv)
	}
	return append(env, "PATH="+launchPath(exe, basePath))
}

func launchPath(exe, basePath string) string {
	shellPath := ShellPath()
	if shellPath == "" {
		shellPath = basePath
	}
	parts := []string{filepath.Dir(exe)}
	if runtimeDir := preferredNodeBin(); runtimeDir != "" {
		parts = append(parts, runtimeDir)
	}
	parts = append(parts, shellPath)
	return joinUnique(parts...)
}

// joinUnique joins path lists, dropping empty entries and duplicates.
func joinUnique(lists ...string) string {
	var entries []string
	seen := make(map[string]bool)
	for _, list := range lists {
		for _, entry := range filepath.SplitList(list) {
			if entry == "" || seen[entry] {
				continue
			}
			seen[entry] = true
			entries = append(entries, entry)
		}
	}
	return strings.Join(entries, string(os.PathListSeparator))
}

func dedupe(items []string) []string {
	seen := make(map[string]bool, len(items))
	out := items[:0]
	for _, item := range items {
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

// versionsNewestFirst lists the version directories under root, newest first.
func versionsNewestFirst(root string) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var versions []string
	for _, e := range entries {
		if e.IsDir() {
			versions = append(versions, e.Name())
		}
	}
	sort.Slice(versions, func(i, j int) bool {
		return compareVersions(versions[i], versions[j]) > 0
	})
	return versions
}

// compareVersions compares version directory names numerically, so v10 sorts
// after v9 rather than before it.
func compareVersions(a, b string) int {
	as, bs := versionNumbers(a), versionNumbers(b)
	for i := 0; i < len(as) || i < len(bs); i++ {
		var av, bv int
		if i < len(as) {
			av = as[i]
		}
		if i < len(bs) {
			bv = bs[i]
		}
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}
	return 0
}

func versionNumbers(version string) []int {
	fields := strings.Split(strings.TrimPrefix(version, "v"), ".")
	numbers := make([]int, 0, len(fields))
	for _, field := range fields {
		number, err := strconv.Atoi(field)
		if err != nil {
			break
		}
		numbers = append(numbers, number)
	}
	return numbers
}
