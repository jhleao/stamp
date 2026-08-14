package agentsetup

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/weve-ai/stamp/internal/agent"
	"github.com/weve-ai/stamp/internal/project"
)

type Result struct {
	Root       string
	ClaudeFile string
	MCPFile    string
	Endpoint   string
}

// Setup makes a Stamp workspace self-describing to filesystem agents and
// connects Claude Code to the MCP endpoint hosted by Stamp Studio.
func Setup(start string) (Result, error) {
	root, err := project.FindRoot(start)
	if err != nil {
		return Result{}, err
	}
	if err := project.EnsureAgentCompatibility(root); err != nil {
		return Result{}, err
	}
	mcpPath := filepath.Join(root, ".mcp.json")
	config := map[string]any{}
	if data, readErr := os.ReadFile(mcpPath); readErr == nil {
		if err := json.Unmarshal(data, &config); err != nil {
			return Result{}, fmt.Errorf("read .mcp.json: %w", err)
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return Result{}, readErr
	}
	servers, ok := config["mcpServers"].(map[string]any)
	if !ok {
		servers = map[string]any{}
		config["mcpServers"] = servers
	}
	servers["stamp"] = map[string]any{"type": "http", "url": agent.StudioEndpoint}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return Result{}, err
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(root, ".stamp-mcp-*.json")
	if err != nil {
		return Result{}, err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return Result{}, err
	}
	if err := temporary.Close(); err != nil {
		return Result{}, err
	}
	if err := os.Rename(temporaryPath, mcpPath); err != nil {
		return Result{}, err
	}
	return Result{Root: root, ClaudeFile: filepath.Join(root, "CLAUDE.md"), MCPFile: mcpPath, Endpoint: agent.StudioEndpoint}, nil
}
