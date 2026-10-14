package main

import (
	"encoding/json"

	"github.com/rain/pi-client/internal/pi"
)

// mcpServer is one server from `pi mcp list --json`.
type mcpServer struct {
	Name  string   `json:"name"`
	State string   `json:"state"`
	Tools []string `json:"tools"`
}

// piMCPList runs `pi mcp list --json` and returns the servers and errors.
func piMCPList(piPath string) ([]mcpServer, []string, error) {
	cmd, err := pi.NewCommand(piPath, "mcp", "list", "--json")
	if err != nil {
		return nil, nil, err
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, nil, err
	}
	var res struct {
		Servers []mcpServer `json:"servers"`
		Errors  []string    `json:"errors"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, nil, err
	}
	return res.Servers, res.Errors, nil
}

// loadMCP fetches the MCP servers for the sidebar, once.
func (a *app) loadMCP() {
	if a.mcpLoading || a.mcpLoaded {
		return
	}
	a.mcpLoading = true
	go func() {
		servers, errs, err := piMCPList(a.piPath)
		a.win.Update(func() {
			a.mcpLoading = false
			a.mcpLoaded = true
			if err != nil {
				a.mcpErr = err.Error()
				return
			}
			a.mcpServers = servers
			a.mcpErrors = errs
		})
	}()
}
