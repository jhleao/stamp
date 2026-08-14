package agentsetup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/weve-ai/stamp/internal/agent"
	"github.com/weve-ai/stamp/internal/project"
)

func TestSetupCreatesClaudeLinkAndMergesMCPConfig(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if _, err := project.Create(root, "Test"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(`{"mcpServers":{"other":{"type":"http","url":"http://example.test/mcp"}},"custom":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Setup(root)
	if err != nil {
		t.Fatal(err)
	}
	if result.Endpoint != agent.StudioEndpoint {
		t.Fatalf("endpoint = %q", result.Endpoint)
	}
	target, err := os.Readlink(filepath.Join(root, "CLAUDE.md"))
	if err != nil || target != "AGENTS.md" {
		t.Fatalf("CLAUDE.md = %q, %v", target, err)
	}
	var config struct {
		MCPServers map[string]struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"mcpServers"`
		Custom bool `json:"custom"`
	}
	data, err := os.ReadFile(filepath.Join(root, ".mcp.json"))
	if err != nil || json.Unmarshal(data, &config) != nil {
		t.Fatal(err)
	}
	if !config.Custom || config.MCPServers["other"].URL == "" || config.MCPServers["stamp"].URL != agent.StudioEndpoint {
		t.Fatalf("config = %#v", config)
	}
}
