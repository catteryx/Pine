package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// customMarker marks the "add a custom provider" choice.
const customMarker = "\x00custom"

// apiProtocols are the API protocols offered for a custom endpoint.
var apiProtocols = []string{
	"openai-completions",
	"openai-responses",
	"anthropic-messages",
	"google-generative-ai",
	"mistral-conversations",
}

// modelRow is one model of a custom endpoint: its id and the limits the user
// set, empty when pi's defaults should apply. Rows are held by pointer so an
// input keeps pointing at its model when the list is appended to or shortened.
type modelRow struct {
	id        string
	context   string // context window in tokens
	maxTokens string // max output tokens
}

// providerRow is one model service in the settings window: a built-in provider
// whose API key lives in auth.json, or a custom endpoint in models.json.
type providerRow struct {
	id       string
	name     string
	custom   bool
	oauth    bool
	fresh    bool // added this session, nothing stored yet
	expanded bool // show the fields, not only the name

	draftKey string // a key being typed for a built-in provider

	// Custom endpoints, from models.json.
	baseURL  string
	api      string
	key      string
	models   []*modelRow
	fetching bool
	fetchErr string
}

// customModel and customProvider mirror what Pine reads from and writes to
// models.json.
type customModel struct {
	ID        string
	Context   int
	MaxTokens int
}

type customProvider struct {
	ID      string
	Name    string
	API     string
	BaseURL string
	APIKey  string
	Models  []customModel
}

// readCustomProviders returns the providers of models.json.
func readCustomProviders() (map[string]customProvider, error) {
	byID := map[string]customProvider{}
	top, err := readJSONMap(modelsPath())
	if err != nil {
		return byID, err
	}
	raw, ok := top["providers"]
	if !ok {
		return byID, nil
	}
	var provs map[string]struct {
		Name    string `json:"name"`
		API     string `json:"api"`
		BaseURL string `json:"baseUrl"`
		APIKey  string `json:"apiKey"`
		Models  []struct {
			ID            string `json:"id"`
			ContextWindow int    `json:"contextWindow"`
			MaxTokens     int    `json:"maxTokens"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &provs); err != nil {
		return byID, err
	}
	for id, p := range provs {
		cp := customProvider{ID: id, Name: p.Name, API: p.API, BaseURL: p.BaseURL, APIKey: p.APIKey}
		for _, m := range p.Models {
			cp.Models = append(cp.Models, customModel{ID: m.ID, Context: m.ContextWindow, MaxTokens: m.MaxTokens})
		}
		byID[id] = cp
	}
	return byID, nil
}

// writeCustomProvider adds or replaces one provider in models.json, keeping
// every field Pine does not manage, and every model entry's metadata (name,
// cost, …) for the model ids that are kept.
func writeCustomProvider(p customProvider) error {
	top, err := readJSONMap(modelsPath())
	if err != nil {
		return err
	}
	provs := map[string]json.RawMessage{}
	if raw, ok := top["providers"]; ok {
		_ = json.Unmarshal(raw, &provs)
	}
	entry := map[string]json.RawMessage{}
	if raw, ok := provs[p.ID]; ok {
		_ = json.Unmarshal(raw, &entry)
	}
	set := func(key string, value any) {
		b, _ := json.Marshal(value)
		entry[key] = b
	}
	if p.Name != "" {
		set("name", p.Name)
	}
	if p.API != "" {
		set("api", p.API)
	}
	set("baseUrl", p.BaseURL)
	if p.APIKey != "" {
		set("apiKey", p.APIKey)
	} else {
		delete(entry, "apiKey")
	}

	type modelEntry struct {
		ID string `json:"id"`
	}
	kept := map[string]json.RawMessage{}
	if raw, ok := entry["models"]; ok {
		var list []json.RawMessage
		if json.Unmarshal(raw, &list) == nil {
			for _, m := range list {
				var e modelEntry
				if json.Unmarshal(m, &e) == nil && e.ID != "" {
					kept[e.ID] = m
				}
			}
		}
	}
	models := make([]json.RawMessage, 0, len(p.Models))
	for _, m := range p.Models {
		fields := map[string]json.RawMessage{}
		if raw, ok := kept[m.ID]; ok {
			_ = json.Unmarshal(raw, &fields)
		}
		b, _ := json.Marshal(m.ID)
		fields["id"] = b
		setNumber := func(key string, n int) {
			if n > 0 {
				fields[key] = json.RawMessage(strconv.Itoa(n))
			} else {
				delete(fields, key)
			}
		}
		setNumber("contextWindow", m.Context)
		setNumber("maxTokens", m.MaxTokens)
		b, err := json.Marshal(fields)
		if err != nil {
			return err
		}
		models = append(models, b)
	}
	set("models", models)

	b, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	provs[p.ID] = b
	pb, err := json.Marshal(provs)
	if err != nil {
		return err
	}
	top["providers"] = pb
	return writeJSONMap(modelsPath(), top)
}

// removeCustomProvider deletes a provider from models.json.
func removeCustomProvider(id string) error {
	top, err := readJSONMap(modelsPath())
	if err != nil {
		return err
	}
	provs := map[string]json.RawMessage{}
	if raw, ok := top["providers"]; ok {
		_ = json.Unmarshal(raw, &provs)
	}
	delete(provs, id)
	pb, err := json.Marshal(provs)
	if err != nil {
		return err
	}
	top["providers"] = pb
	return writeJSONMap(modelsPath(), top)
}

// loadProviderRows rebuilds the list of configured model services from
// auth.json and models.json, keeping typed keys and expanded rows.
func (a *app) loadProviderRows() {
	drafts := map[string]string{}
	open := map[string]bool{}
	for _, r := range a.providerRows {
		if !r.fresh {
			drafts[r.id] = r.draftKey
			open[r.id] = r.expanded
		}
	}
	a.providerRows = nil
	a.providerLoaded = true
	a.providerErr = ""

	stored, err := readCredentials()
	if err != nil {
		a.providerErr = err.Error()
	}
	custom, err := readCustomProviders()
	if err != nil {
		a.providerErr = err.Error()
	}

	ids := make([]string, 0, len(custom))
	for id := range custom {
		ids = append(ids, id)
	}
	sortStrings(ids)
	for _, id := range ids {
		p := custom[id]
		api := p.API
		if api == "" {
			api = apiProtocols[0]
		}
		row := &providerRow{
			id:       id,
			name:     providerDisplayName(id),
			custom:   true,
			expanded: open[id],
			baseURL:  p.BaseURL,
			api:      api,
			key:      p.APIKey,
		}
		for _, m := range p.Models {
			row.models = append(row.models, &modelRow{
				id:        m.ID,
				context:   positiveInt(m.Context),
				maxTokens: positiveInt(m.MaxTokens),
			})
		}
		a.providerRows = append(a.providerRows, row)
	}

	ids = ids[:0]
	for id := range stored {
		if _, ok := custom[id]; !ok {
			ids = append(ids, id)
		}
	}
	sortStrings(ids)
	for _, id := range ids {
		row := &providerRow{id: id, name: providerDisplayName(id), draftKey: drafts[id], expanded: open[id]}
		if credentialKind(stored[id]) == "oauth" {
			row.oauth = true
		}
		a.providerRows = append(a.providerRows, row)
	}
}

func positiveInt(n int) string {
	if n <= 0 {
		return ""
	}
	return strconv.Itoa(n)
}

// addableProviders lists the built-in providers not configured yet, then the
// custom choice: the ids and the labels the add combobox shows.
func (a *app) addableProviders() (ids, labels []string) {
	have := map[string]bool{}
	for _, r := range a.providerRows {
		have[r.id] = true
	}
	for _, p := range apiKeyProviders {
		if !have[p.id] {
			ids = append(ids, p.id)
			labels = append(labels, p.name)
		}
	}
	return append(ids, customMarker), append(labels, "Custom provider…")
}

// addProvider starts a new row for the chosen id, or a blank custom row.
func (a *app) addProvider(id string) {
	if id == customMarker || id == "" {
		a.providerRows = append(a.providerRows, &providerRow{
			name:     "Custom",
			custom:   true,
			fresh:    true,
			expanded: true,
			api:      apiProtocols[0],
		})
		return
	}
	a.providerRows = append(a.providerRows, &providerRow{
		id:       id,
		name:     providerDisplayName(id),
		fresh:    true,
		expanded: true,
	})
}

// removeProviderRow drops a fresh row, or deletes a stored credential or
// custom endpoint from the file it lives in. Removing a row re-renders on the
// next frame; the row itself stays until then.
func (a *app) removeProviderRow(row *providerRow) {
	if row.fresh {
		out := a.providerRows[:0]
		for _, r := range a.providerRows {
			if r != row {
				out = append(out, r)
			}
		}
		a.providerRows = out
		return
	}
	var err error
	if row.custom {
		err = removeCustomProvider(row.id)
	} else {
		err = clearAuthRow(row.id)
	}
	if err != nil {
		a.providerErr = err.Error()
	} else {
		a.afterProviderChange()
	}
}

// saveProviders writes typed keys to auth.json and custom endpoints to
// models.json.
func (a *app) saveProviders() error {
	stored, err := readCredentials()
	if err != nil {
		return err
	}
	for _, r := range a.providerRows {
		if r.custom || r.oauth {
			continue
		}
		key := strings.TrimSpace(r.draftKey)
		if key == "" {
			continue
		}
		raw, err := json.Marshal(credential{Type: "api_key", Key: key})
		if err != nil {
			return err
		}
		stored[r.id] = raw
	}
	if err := writeCredentials(stored); err != nil {
		return err
	}

	for _, r := range a.providerRows {
		if !r.custom {
			continue
		}
		id := strings.TrimSpace(r.id)
		baseURL := strings.TrimSpace(r.baseURL)
		var models []customModel
		for _, m := range r.models {
			mid := strings.TrimSpace(m.id)
			if mid == "" {
				continue
			}
			models = append(models, customModel{
				ID:        mid,
				Context:   atoi(m.context),
				MaxTokens: atoi(m.maxTokens),
			})
		}
		if id == "" && baseURL == "" && len(models) == 0 {
			continue // an empty custom row the user did not fill in
		}
		if id == "" {
			return fmt.Errorf("custom provider needs an id")
		}
		if baseURL == "" {
			return fmt.Errorf("provider %s needs a base URL", id)
		}
		if len(models) == 0 {
			return fmt.Errorf("provider %s needs at least one model", id)
		}
		if err := writeCustomProvider(customProvider{
			ID:      id,
			Name:    strings.TrimSpace(r.name),
			API:     r.api,
			BaseURL: baseURL,
			APIKey:  strings.TrimSpace(r.key),
			Models:  models,
		}); err != nil {
			return err
		}
	}

	for _, r := range a.providerRows {
		r.draftKey = ""
	}
	return nil
}

// fetchProviderModels asks the endpoint for its model list and merges it into
// the row, in the background. Limits the endpoint reports fill in the context
// window and max output tokens.
func (a *app) fetchProviderModels(r *providerRow) {
	if r.fetching {
		return
	}
	r.fetching = true
	r.fetchErr = ""
	baseURL, api, key := r.baseURL, r.api, r.key
	go func() {
		models, err := fetchModels(baseURL, api, key)
		mygo.RunOnMain(func() {
			r.fetching = false
			if err != nil {
				r.fetchErr = err.Error()
			} else {
				r.mergeModels(models)
			}
			if a.settingsWin != nil {
				a.settingsWin.Invalidate()
			}
		})
	}()
}

// mergeModels adds the fetched models the row does not have, and fills in
// limits the endpoint reported for models it does.
func (r *providerRow) mergeModels(fetched []fetchedModel) {
	index := map[string]*modelRow{}
	for _, m := range r.models {
		index[strings.TrimSpace(m.id)] = m
	}
	for _, f := range fetched {
		if m, ok := index[f.id]; ok {
			if m.context == "" && f.context > 0 {
				m.context = positiveInt(f.context)
			}
			if m.maxTokens == "" && f.maxTokens > 0 {
				m.maxTokens = positiveInt(f.maxTokens)
			}
			continue
		}
		row := &modelRow{id: f.id, context: positiveInt(f.context), maxTokens: positiveInt(f.maxTokens)}
		r.models = append(r.models, row)
		index[f.id] = row
	}
}

// fetchedModel is one model an endpoint listed, with whatever limits it
// reported.
type fetchedModel struct {
	id        string
	context   int
	maxTokens int
}

// fetchModels lists the models an endpoint serves.
func fetchModels(baseURL, api, apiKey string) ([]fetchedModel, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("base URL is empty")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var req *http.Request
	var parse func([]byte) ([]fetchedModel, error)
	switch api {
	case "anthropic-messages":
		req, _ = http.NewRequestWithContext(ctx, http.MethodGet, base+"/models?limit=1000", nil)
		if apiKey != "" {
			req.Header.Set("x-api-key", apiKey)
		}
		req.Header.Set("anthropic-version", "2023-06-01")
		parse = func(b []byte) ([]fetchedModel, error) {
			var d struct {
				Data []struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			if err := json.Unmarshal(b, &d); err != nil {
				return nil, err
			}
			var out []fetchedModel
			for _, m := range d.Data {
				if m.ID != "" {
					out = append(out, fetchedModel{id: m.ID})
				}
			}
			return out, nil
		}
	case "google-generative-ai":
		u := base + "/models"
		if apiKey != "" {
			u += "?key=" + url.QueryEscape(apiKey)
		}
		req, _ = http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		parse = func(b []byte) ([]fetchedModel, error) {
			var d struct {
				Models []struct {
					Name             string `json:"name"`
					InputTokenLimit  int    `json:"inputTokenLimit"`
					OutputTokenLimit int    `json:"outputTokenLimit"`
				} `json:"models"`
			}
			if err := json.Unmarshal(b, &d); err != nil {
				return nil, err
			}
			var out []fetchedModel
			for _, m := range d.Models {
				if m.Name == "" {
					continue
				}
				out = append(out, fetchedModel{
					id:        strings.TrimPrefix(m.Name, "models/"),
					context:   m.InputTokenLimit,
					maxTokens: m.OutputTokenLimit,
				})
			}
			return out, nil
		}
	default: // openai-completions, openai-responses, mistral-conversations
		req, _ = http.NewRequestWithContext(ctx, http.MethodGet, base+"/models", nil)
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
		parse = func(b []byte) ([]fetchedModel, error) {
			var d struct {
				Data []struct {
					ID             string `json:"id"`
					ContextWindow  int    `json:"context_window"`
					ContextLength  int    `json:"context_length"`
					MaxOutputToken int    `json:"max_output_tokens"`
				} `json:"data"`
			}
			if err := json.Unmarshal(b, &d); err != nil {
				return nil, err
			}
			var out []fetchedModel
			for _, m := range d.Data {
				if m.ID == "" {
					continue
				}
				window := m.ContextWindow
				if window == 0 {
					window = m.ContextLength
				}
				out = append(out, fetchedModel{id: m.ID, context: window, maxTokens: m.MaxOutputToken})
			}
			return out, nil
		}
	}
	if req == nil {
		return nil, fmt.Errorf("protocol %q cannot list models", api)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode/100 != 2 {
		msg := strings.TrimSpace(string(body))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return nil, fmt.Errorf("%s: %s", resp.Status, msg)
	}
	models, err := parse(body)
	if err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("no models returned")
	}
	return models, nil
}

// afterProviderChange reloads the rows and restarts the sessions, since pi
// reads auth.json and models.json when it starts.
func (a *app) afterProviderChange() {
	a.loadProviderRows()
	a.reconnectAll()
	if a.win != nil {
		a.win.Invalidate()
	}
	if a.settingsWin != nil {
		a.settingsWin.Invalidate()
	}
}

// providersSection lists the configured model services, each as a card whose
// fields appear only while it is expanded, so the tab opens small.
func (a *app) providersSection(c *ui.Context, t *ui.Theme) {
	if !a.providerLoaded {
		a.loadProviderRows()
	}
	ui.Text(c, "Model services").FontWeight(600).FontSize(t.FontSize + 1)
	ui.Text(c, "API keys are stored in pi's auth.json, custom endpoints in models.json.").
		FontSize(t.FontSize - 1).TextColor(t.TextMuted)

	if len(a.providerRows) == 0 {
		ui.Text(c, "No model services yet. Add one below.").FontSize(t.FontSize - 1).TextColor(t.TextMuted)
	}
	for _, row := range a.providerRows {
		a.providerCard(c, t, row)
	}

	ids, labels := a.addableProviders()
	ui.Row(c).AlignItems(ui.Center).Gap(t.Space(2)).Children(func() {
		ui.Combobox(c, &a.addChoice, labels).Placeholder("Add a provider…").Grow(1)
		if ui.Button(c, "Add").Clicked() {
			for i, label := range labels {
				if label == a.addChoice {
					a.addProvider(ids[i])
					break
				}
			}
			a.addChoice = ""
		}
	})

	ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
		if a.providerErr != "" {
			ui.Text(c, a.providerErr).FontSize(t.FontSize - 1).TextColor(t.Danger).Shrink(1)
		} else {
			ui.Text(c, "")
		}
		if ui.Button(c, "Save").Clicked() {
			if err := a.saveProviders(); err != nil {
				a.providerErr = err.Error()
			} else {
				a.afterProviderChange()
			}
		}
	})
}

// providerCard is one model service: a header that expands to its fields.
func (a *app) providerCard(c *ui.Context, t *ui.Theme, r *providerRow) {
	face := t.Surface
	if !t.Dark {
		face = t.Background
	}
	ui.Column(c).Shrink(0).Gap(t.Space(2)).Radius(t.Radius+4).
		Background(face).Border(1, t.Border).Clip().
		Padding(t.Space(3), t.Space(4)).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Column(c).Grow(1).Shrink(1).Gap(t.Space(0.5)).Children(func() {
				ui.Text(c, r.title()).FontWeight(500).SingleLine()
				ui.Text(c, providerSummary(r)).FontSize(t.FontSize - 1).TextColor(t.TextMuted).SingleLine()
			})
			label := "Edit"
			if r.expanded {
				label = "Done"
			}
			if ui.Button(c, label).Clicked() {
				r.expanded = !r.expanded
			}
		})
		if !r.expanded {
			return
		}
		if r.custom {
			a.customProviderFields(c, t, r)
		} else {
			a.builtinProviderFields(c, t, r)
		}
	})
}

// fieldRow is a label and its control on one line, inside a card.
func fieldRow(c *ui.Context, t *ui.Theme, label string, control func()) {
	ui.Row(c).AlignItems(ui.Center).Gap(t.Space(3)).Children(func() {
		ui.Text(c, label).FontSize(t.FontSize - 1).TextColor(t.TextMuted).Width(84).Shrink(0)
		ui.Row(c).Grow(1).AlignItems(ui.Center).Children(control)
	})
}

// builtinProviderFields edits one built-in provider.
func (a *app) builtinProviderFields(c *ui.Context, t *ui.Theme, r *providerRow) {
	if r.oauth {
		ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Text(c, "OAuth credential stored by pi.").FontSize(t.FontSize - 1).TextColor(t.TextMuted).Shrink(1)
			if ui.Button(c, "Remove").Clicked() {
				a.removeProviderRow(r)
			}
		})
		return
	}
	fieldRow(c, t, "API key", func() {
		ui.TextInput(c, &r.draftKey).Password().Placeholder("API key").Grow(1)
	})
	ui.Row(c).Justify(ui.End).Children(func() {
		if ui.Button(c, "Remove").Clicked() {
			a.removeProviderRow(r)
		}
	})
}

// customProviderFields edits one custom endpoint and its models.
func (a *app) customProviderFields(c *ui.Context, t *ui.Theme, r *providerRow) {
	if r.fresh {
		fieldRow(c, t, "Provider ID", func() {
			ui.TextInput(c, &r.id).Placeholder("ollama").Grow(1)
		})
	}
	fieldRow(c, t, "Base URL", func() {
		ui.TextInput(c, &r.baseURL).Placeholder("http://localhost:11434/v1").Grow(1)
	})
	fieldRow(c, t, "API", func() {
		ui.Combobox(c, &r.api, apiProtocols).Width(220)
	})
	fieldRow(c, t, "API key", func() {
		ui.TextInput(c, &r.key).Password().Placeholder("optional").Grow(1)
	})

	// Both model actions on the header line, not on rows of their own.
	ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
		ui.Text(c, fmt.Sprintf("Models · %d", len(r.models))).FontSize(t.FontSize - 1).TextColor(t.TextMuted)
		ui.Row(c).Shrink(0).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			if ui.Button(c, "Fetch models").Clicked() {
				a.fetchProviderModels(r)
			}
			if ui.Button(c, "Add model").Clicked() {
				r.models = append(r.models, &modelRow{})
			}
		})
	})
	if r.fetching {
		ui.Text(c, "Fetching models…").FontSize(t.FontSize - 1).TextColor(t.TextMuted)
	}
	if r.fetchErr != "" {
		ui.Text(c, "Fetch failed: "+r.fetchErr).FontSize(t.FontSize - 1).TextColor(t.Danger)
	}

	// Removing is deferred to after the loop: mutating the slice while it is
	// being walked is what crashed the app before.
	removeAt := -1
	for i := range r.models {
		m := r.models[i]
		ui.Row(c).AlignItems(ui.Center).Gap(t.Space(1.5)).Children(func() {
			ui.TextInput(c, &m.id).Grow(1).Placeholder("model id")
			ui.TextInput(c, &m.context).Width(76).Placeholder("context")
			ui.TextInput(c, &m.maxTokens).Width(76).Placeholder("max out")
			if ui.Button(c, "✕").Clicked() {
				removeAt = i
			}
		})
	}
	if removeAt >= 0 {
		r.models = append(r.models[:removeAt], r.models[removeAt+1:]...)
	}

	ui.Row(c).Justify(ui.End).Children(func() {
		if ui.Button(c, "Remove provider").Clicked() {
			a.removeProviderRow(r)
		}
	})
}

// title names a provider row.
func (r *providerRow) title() string {
	if r.custom && r.id == "" {
		return "New custom provider"
	}
	return r.name
}

// providerSummary is the one line shown while a row is collapsed.
func providerSummary(r *providerRow) string {
	if r.custom {
		if strings.TrimSpace(r.baseURL) == "" {
			return "Custom endpoint · not set"
		}
		return fmt.Sprintf("%s · %d models", strings.TrimSpace(r.baseURL), len(r.models))
	}
	switch {
	case r.oauth:
		return "OAuth credential"
	case strings.TrimSpace(r.draftKey) != "":
		return "Unsaved key"
	case r.fresh:
		return "Not set"
	}
	return "API key saved"
}

// atoi reads a token count, treating anything unparsable as unset.
func atoi(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 {
		return 0
	}
	return n
}
