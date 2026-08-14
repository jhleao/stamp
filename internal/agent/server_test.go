package agent

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/jhleao/stamp/internal/project"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestServerHasSmallToolSurface(t *testing.T) {
	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := New("test").Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()
	tools, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 12 {
		t.Fatalf("got %d tools, want 12", len(tools.Tools))
	}
}

func TestHTTPServerDefaultsToolsToStudioProject(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if _, err := project.Create(root, "Studio project"); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(HTTPHandler("test", root))
	defer httpServer.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: httpServer.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "project_status", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("project_status returned an error: %#v", result.Content)
	}
}
