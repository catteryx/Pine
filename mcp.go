package main

import (
	"encoding/json"
	"os/exec"
)

// mcpServer is one server from `pi mcp list --json`.
type mcpServer struct {
	Name  string   `json:"name"`
	State string   `json:"state"`
	Tools []string `json:"tools"`
}

func piBin(piPath string) string {
	if piPath == "" {
		return "pi"
	}
	return piPath
}

// piMCPList runs `pi mcp list --json` and returns the servers and errors.
func piMCPList(piPath string) ([]mcpServer, []string, error) {
	out, err := exec.Command(piBin(piPath), "mcp", "list", "--json").Output()
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
