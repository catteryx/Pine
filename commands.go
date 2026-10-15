package main

import (
	"context"
	"strings"
)

// cmd is a slash command: one the client runs itself, or one Pi expands
// (a prompt template, skill or extension command).
type cmd struct {
	name   string
	desc   string
	source string // "client", "skill", "prompt", "extension"
	args   string // argument hint, e.g. "<name>"
	client bool
}

// clientCommands are the commands the client handles itself.
var clientCommands = []cmd{
	{"new", "Start a new session", "client", "", true},
	{"resume", "Resume a saved session", "client", "", true},
	{"settings", "Open settings", "client", "", true},
	{"rename", "Rename this session", "client", "<name>", true},
	{"compact", "Compact the context", "client", "", true},
	{"clear", "Clear the queued messages", "client", "", true},
	{"stop", "Stop the current run", "client", "", true},
	{"bash", "Run a shell command", "client", "<command>", true},
	{"skills", "List the available skills", "client", "", true},
	{"mcp", "List the MCP servers", "client", "", true},
	{"model", "List models, or switch to one", "client", "[id]", true},
	{"thinking", "Cycle or set the thinking level", "client", "[level]", true},
	{"export", "Export this session as HTML", "client", "", true},
	{"auto-compact", "Toggle automatic compaction", "client", "", true},
	{"auto-retry", "Toggle automatic retry", "client", "", true},
	{"abort-retry", "Abort an in-progress retry", "client", "", true},
	{"steering", "Set the steering mode", "client", "[all|one-at-a-time]", true},
	{"follow-up", "Set the follow-up mode", "client", "[all|one-at-a-time]", true},
	{"help", "Show these commands", "client", "", true},
}

// allCommands is the client's commands followed by Pi's discoverable ones.
func (s *session) allCommands() []cmd {
	out := append([]cmd(nil), clientCommands...)
	for _, c := range s.commands {
		src := c.Source
		if src == "" {
			src = "command"
		}
		out = append(out, cmd{name: c.Name, desc: c.Description, source: src})
	}
	return out
}

// slashQuery returns the command name being typed, when the draft is a slash
// command without arguments yet.
func slashQuery(draft string) (string, bool) {
	if !strings.HasPrefix(draft, "/") {
		return "", false
	}
	rest := draft[1:]
	if i := strings.IndexAny(rest, " \n"); i >= 0 {
		return "", false
	}
	return rest, true
}

// slashMatches returns the commands matching the name being typed.
func (s *session) slashMatches(query string) []cmd {
	q := strings.ToLower(query)
	var out []cmd
	for _, c := range s.allCommands() {
		if q == "" || strings.HasPrefix(strings.ToLower(c.name), q) {
			out = append(out, c)
		}
	}
	return out
}

// runCommand runs a client command. It reports whether the name is one.
func (s *session) runCommand(name, args string) bool {
	args = strings.TrimSpace(args)
	switch name {
	case "new":
		s.app.newSessionTab()
	case "resume":
		s.app.openSaved()
	case "settings":
		s.app.openSettings()
	case "rename":
		if args == "" {
			s.addError("usage: /rename <name>")
			return true
		}
		go s.setSessionName(args)
	case "compact":
		s.compact()
	case "clear":
		go s.clearQueue()
	case "stop":
		s.stop()
	case "bash":
		if args == "" {
			s.addError("usage: /bash <command>")
			return true
		}
		go s.runBash(args)
	case "skills":
		s.listSkills()
	case "mcp":
		go s.listMCP()
	case "model":
		if args == "" {
			s.listModels()
		} else {
			go s.setModelByName(args)
		}
	case "thinking":
		if args == "" {
			go s.cycleThinking()
		} else {
			go s.setThinking(args)
		}
	case "export":
		go s.exportHTML()
	case "auto-compact":
		go s.toggleAutoCompaction()
	case "auto-retry":
		go s.toggleAutoRetry()
	case "abort-retry":
		go s.abortRetry()
	case "steering":
		go s.setSteeringMode(args)
	case "follow-up":
		go s.setFollowUpMode(args)
	case "help":
		s.addSummary(helpText())
	default:
		return false
	}
	return true
}

// sendOrCommand runs a client slash command, or reports that the prompt
// should go to Pi (whose own commands expand there).
func (s *session) sendOrCommand() bool {
	text := strings.TrimSpace(s.draft)
	if strings.HasPrefix(text, "/") {
		fields := strings.SplitN(text[1:], " ", 2)
		args := ""
		if len(fields) == 2 {
			args = fields[1]
		}
		if s.runCommand(fields[0], args) {
			s.draft = ""
			s.slashIdx = 0
			return true
		}
	}
	return false
}

func helpText() string {
	var b strings.Builder
	b.WriteString("Slash commands:\n")
	for _, c := range clientCommands {
		b.WriteString("/" + c.name)
		if c.args != "" {
			b.WriteString(" " + c.args)
		}
		b.WriteString(" — " + c.desc + "\n")
	}
	b.WriteString("\nPi's own commands (prompt templates, skills, extensions) expand when sent.")
	return b.String()
}

// --- client command implementations ----------------------------------------

func (s *session) setSessionName(name string) {
	if c := s.getClient(); c != nil {
		_ = c.SetSessionName(context.Background(), name)
	}
	s.win().Update(func() {
		s.title = name
		s.state.SessionName = name
		s.app.saveState()
	})
}

func (s *session) clearQueue() {
	c := s.getClient()
	if c == nil {
		return
	}
	steering, followUp, err := c.ClearQueue(context.Background())
	if err != nil {
		s.win().Update(func() { s.addError("clear queue: " + err.Error()) })
		return
	}
	s.win().Update(func() {
		var parts []string
		parts = append(parts, steering...)
		parts = append(parts, followUp...)
		if len(parts) > 0 {
			if s.draft != "" {
				s.draft += "\n"
			}
			s.draft += strings.Join(parts, "\n")
		}
		s.focusComposer = true
	})
}

func (s *session) runBash(command string) {
	c := s.getClient()
	if c == nil {
		return
	}
	output, code, err := c.Bash(context.Background(), command, false)
	if err != nil {
		s.win().Update(func() { s.addError("bash: " + err.Error()) })
		return
	}
	s.win().Update(func() {
		ec := code
		s.addEntry(&entry{id: s.nextID(), kind: kindBash, command: command, output: output, exitCode: &ec})
	})
}

func (s *session) cycleThinking() {
	c := s.getClient()
	if c == nil {
		return
	}
	level, err := c.CycleThinkingLevel(context.Background())
	if err != nil {
		s.win().Update(func() { s.addError("thinking: " + err.Error()) })
		return
	}
	if level != "" {
		s.win().Update(func() { s.state.ThinkingLevel = level })
	}
}

func (s *session) setModelByName(name string) {
	c := s.getClient()
	if c == nil {
		return
	}
	name = strings.ToLower(name)
	var match *piModelRef
	for i := range s.models {
		m := s.models[i]
		if strings.EqualFold(m.ID, name) || strings.EqualFold(m.Name, name) {
			match = &piModelRef{m.Provider, m.ID}
			break
		}
	}
	if match == nil {
		for i := range s.models {
			m := s.models[i]
			if strings.Contains(strings.ToLower(m.ID), name) || strings.Contains(strings.ToLower(m.Name), name) {
				match = &piModelRef{m.Provider, m.ID}
				break
			}
		}
	}
	if match == nil {
		s.win().Update(func() { s.addError("no model matches " + name) })
		return
	}
	if _, err := c.SetModel(context.Background(), match.provider, match.id); err != nil {
		s.win().Update(func() { s.addError("set model: " + err.Error()) })
		return
	}
	state, _ := c.GetState(context.Background())
	levels, _ := c.GetAvailableThinkingLevels(context.Background())
	s.win().Update(func() {
		s.state = state
		s.levels = levels
		s.rememberModel(match.provider, match.id)
	})
}

type piModelRef struct{ provider, id string }

func (s *session) listModels() {
	var b strings.Builder
	if s.state.Model != nil {
		b.WriteString("Current: " + s.state.Model.Label() + "\n\n")
	}
	for _, m := range s.models {
		b.WriteString("· " + m.Label() + "  (" + m.Provider + "/" + m.ID + ")\n")
	}
	if len(s.models) == 0 {
		b.WriteString("No models configured.")
	}
	s.addSummary(b.String())
}

func (s *session) listSkills() {
	var b strings.Builder
	n := 0
	for _, c := range s.commands {
		if c.Source == "skill" {
			b.WriteString("· /" + c.Name + " — " + c.Description + "\n")
			n++
		}
	}
	if n == 0 {
		b.WriteString("No skills found.")
	}
	s.addSummary("Skills:\n" + b.String())
}

func (s *session) toggleAutoCompaction() {
	c := s.getClient()
	if c == nil {
		return
	}
	on := !s.state.AutoCompactionEnabled
	if err := c.SetAutoCompaction(context.Background(), on); err != nil {
		s.win().Update(func() { s.addError("auto compaction: " + err.Error()) })
		return
	}
	s.win().Update(func() {
		s.state.AutoCompactionEnabled = on
		s.addNotice("Automatic compaction " + onOff(on))
	})
}

func (s *session) toggleAutoRetry() {
	c := s.getClient()
	if c == nil {
		return
	}
	on := !s.autoRetry
	if err := c.SetAutoRetry(context.Background(), on); err != nil {
		s.win().Update(func() { s.addError("auto retry: " + err.Error()) })
		return
	}
	s.win().Update(func() {
		s.autoRetry = on
		s.addNotice("Automatic retry " + onOff(on))
	})
}

func (s *session) abortRetry() {
	if c := s.getClient(); c != nil {
		go func() { _ = c.AbortRetry(context.Background()) }()
	}
}

func (s *session) setSteeringMode(mode string) {
	c := s.getClient()
	if c == nil {
		return
	}
	if mode != "all" && mode != "one-at-a-time" {
		mode = otherMode(s.state.SteeringMode)
	}
	if err := c.SetSteeringMode(context.Background(), mode); err != nil {
		s.win().Update(func() { s.addError("steering mode: " + err.Error()) })
		return
	}
	s.win().Update(func() {
		s.state.SteeringMode = mode
		s.addNotice("Steering mode: " + mode)
	})
}

func (s *session) setFollowUpMode(mode string) {
	c := s.getClient()
	if c == nil {
		return
	}
	if mode != "all" && mode != "one-at-a-time" {
		mode = otherMode(s.state.FollowUpMode)
	}
	if err := c.SetFollowUpMode(context.Background(), mode); err != nil {
		s.win().Update(func() { s.addError("follow-up mode: " + err.Error()) })
		return
	}
	s.win().Update(func() {
		s.state.FollowUpMode = mode
		s.addNotice("Follow-up mode: " + mode)
	})
}

func otherMode(m string) string {
	if m == "all" {
		return "one-at-a-time"
	}
	return "all"
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// listMCP runs `pi mcp list --json` and shows the servers.
func (s *session) listMCP() {
	servers, errs, err := piMCPList(s.app.piPath)
	if err != nil {
		s.win().Update(func() { s.addError("mcp: " + err.Error()) })
		return
	}
	var b strings.Builder
	if len(servers) == 0 {
		b.WriteString("No MCP servers configured.")
	}
	for _, srv := range servers {
		b.WriteString("· " + srv.Name + " [" + srv.State + "] — " + itoa(len(srv.Tools)) + " tools\n")
	}
	for _, e := range errs {
		b.WriteString("! " + e + "\n")
	}
	s.win().Update(func() { s.addSummary("MCP:\n" + b.String()) })
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
