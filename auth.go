package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// agentDir is pi's config directory: $PI_CODING_AGENT_DIR when set, else
// ~/.pi/agent. Pine reads and writes auth.json and models.json there.
func agentDir() string {
	if dir := os.Getenv("PI_CODING_AGENT_DIR"); dir != "" {
		return expandHome(dir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".pi", "agent")
}

func expandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, strings.TrimPrefix(path[1:], "/"))
}

// authPath and modelsPath are the two files Pine configures in agentDir.
func authPath() string   { return filepath.Join(agentDir(), "auth.json") }
func modelsPath() string { return filepath.Join(agentDir(), "models.json") }

// readJSONMap reads a JSON object, returning an empty one when the file is
// absent or empty. Values stay raw so fields Pine does not understand survive
// a write.
func readJSONMap(path string) (map[string]json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]json.RawMessage{}, nil
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return map[string]json.RawMessage{}, nil
	}
	m := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// writeJSONMap writes a JSON object with the permissions pi uses for its
// configuration (0600).
func writeJSONMap(path string, m map[string]json.RawMessage) error {
	dir := filepath.Dir(path)
	if dir == "" {
		return errors.New("no config directory")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// credential is one entry of auth.json, as far as Pine reads and writes it.
// Everything else in the entry, such as an OAuth token set, is kept as it is.
type credential struct {
	Type string `json:"type"`
	Key  string `json:"key,omitempty"`
}

func readCredentials() (map[string]json.RawMessage, error) { return readJSONMap(authPath()) }
func writeCredentials(m map[string]json.RawMessage) error  { return writeJSONMap(authPath(), m) }

// credentialKind names what auth.json holds for a provider: "api_key",
// "oauth", or "" when nothing usable is stored.
func credentialKind(raw json.RawMessage) string {
	var c credential
	if json.Unmarshal(raw, &c) != nil {
		return ""
	}
	if c.Type == "api_key" && c.Key == "" {
		return ""
	}
	return c.Type
}

// clearAuthRow removes a provider's stored credential.
func clearAuthRow(id string) error {
	stored, err := readCredentials()
	if err != nil {
		return err
	}
	delete(stored, id)
	return writeCredentials(stored)
}

// providerInfo is one built-in provider Pine offers an API-key field for.
type providerInfo struct {
	id, name, hint string
}

// apiKeyProviders are the built-in providers that take a single API key, in
// the order the settings window offers them.
var apiKeyProviders = []providerInfo{
	{"anthropic", "Anthropic", "sk-ant-…"},
	{"openai", "OpenAI", "sk-…"},
	{"google", "Google Gemini", "AIza…"},
	{"openrouter", "OpenRouter", "sk-or-…"},
	{"deepseek", "DeepSeek", "sk-…"},
	{"groq", "Groq", "gsk_…"},
	{"xai", "xAI", "xai-…"},
	{"mistral", "Mistral", ""},
	{"nvidia", "NVIDIA NIM", "nvapi-…"},
	{"cerebras", "Cerebras", "csk-…"},
	{"together", "Together AI", ""},
	{"fireworks", "Fireworks", "fw_…"},
	{"huggingface", "Hugging Face", "hf_…"},
	{"moonshotai", "Moonshot AI", "sk-…"},
	{"minimax", "MiniMax", ""},
	{"zai", "ZAI Coding Plan", ""},
	{"opencode-go", "OpenCode Zen and Go", ""},
	{"vercel-ai-gateway", "Vercel AI Gateway", ""},
	{"github-copilot", "GitHub Copilot", ""},
	{"azure", "Azure OpenAI", ""},
	{"amazon-bedrock", "Amazon Bedrock", ""},
	{"google-vertex", "Google Vertex AI", ""},
	{"radius", "Radius", ""},
}

// providerDisplayName names a provider id, falling back to the id itself.
func providerDisplayName(id string) string {
	for _, p := range apiKeyProviders {
		if p.id == id {
			return p.name
		}
	}
	return id
}

// reconnectAll restarts every session's pi so new credentials take effect.
func (a *app) reconnectAll() {
	for _, s := range a.sessions {
		s.reconnect()
	}
}

// modelOptions lists the providers and model ids the open sessions reported,
// for the default-model comboboxes.
func (a *app) modelOptions() (providers, models []string) {
	seenProvider, seenModel := map[string]bool{}, map[string]bool{}
	for _, s := range a.sessions {
		for _, m := range s.models {
			if !seenProvider[m.Provider] {
				seenProvider[m.Provider] = true
				providers = append(providers, m.Provider)
			}
			if !seenModel[m.Provider+"/"+m.ID] {
				seenModel[m.Provider+"/"+m.ID] = true
				models = append(models, m.ID)
			}
		}
	}
	sortStrings(providers)
	sortStrings(models)
	return providers, models
}

// sortStrings is a small insertion sort, so this file needs no sort import for
// the handful of values involved.
func sortStrings(items []string) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j] < items[j-1]; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}
