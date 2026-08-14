package studio

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jhleao/stamp/internal/project"
	stamptheme "github.com/jhleao/stamp/internal/theme"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRoutesServeProjectBoundMCP(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if _, err := project.Create(root, "MCP project"); err != nil {
		t.Fatal(err)
	}
	server := &Server{root: root, token: "test", clients: map[chan string]struct{}{}, version: "test"}
	httpServer := httptest.NewServer(server.routes())
	defer httpServer.Close()
	server.host = strings.TrimPrefix(httpServer.URL, "http://")
	server.origin = httpServer.URL
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: httpServer.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "project_status", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatalf("project_status = %#v, %v", result, err)
	}
}

func TestResolveStaysInProject(t *testing.T) {
	server := &Server{root: t.TempDir()}
	if _, err := server.resolve("../outside"); err == nil {
		t.Fatal("expected traversal to be refused")
	}
	want := filepath.Join(server.root, "documents", "memo.page.md")
	got, err := server.resolve("documents/memo.page.md")
	if err != nil || got != want {
		t.Fatalf("resolve = %q, %v; want %q", got, err, want)
	}
}

func TestCompileTailwind(t *testing.T) {
	root := t.TempDir()
	theme := filepath.Join(root, "theme")
	if err := os.MkdirAll(filepath.Join(theme, "components"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(theme, "tailwind.css"), []byte("@import \"tailwindcss\" source(\"./\");\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(theme, "components", "card.tsx"), []byte(`export default function Card({ children }) { return <div className="grid gap-7">{children}</div>; }`), 0o644); err != nil {
		t.Fatal(err)
	}
	binary, err := filepath.Abs(filepath.Join("..", "..", "node_modules", ".bin", "tailwindcss"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("STAMP_TAILWIND_BIN", binary)
	if err := stamptheme.CompileIfNeeded(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(theme, "page.css"))
	if err != nil || !strings.Contains(string(data), ".grid") || !strings.Contains(string(data), ".gap-7") {
		t.Fatalf("compiled CSS did not contain component utilities: %v", err)
	}
}

func TestCreateComponent(t *testing.T) {
	root := t.TempDir()
	server := &Server{root: root, clients: map[chan string]struct{}{}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "/api/components", strings.NewReader(`{"name":"metric-card"}`))
	server.createComponent(recorder, request)
	if recorder.Code != 200 {
		t.Fatalf("createComponent status = %d: %s", recorder.Code, recorder.Body.String())
	}
	path := filepath.Join(root, "theme", "components", "metric-card.tsx")
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), `className="metric-card my-6`) || !strings.Contains(string(data), `props.label`) {
		t.Fatalf("component = %q, %v", data, err)
	}
}

func TestClassifySync(t *testing.T) {
	tests := []struct {
		name   string
		state  project.RemoteState
		status project.ProjectStatus
		remote string
		want   string
		first  bool
	}{
		{name: "local only", want: "local-only", first: true},
		{name: "up to date", state: project.RemoteState{FileID: "file", BaseVersion: "2", WebURL: "https://drive.google.com/example"}, status: project.ProjectStatus{Name: "Quarterly update"}, remote: "2", want: "up-to-date"},
		{name: "local ahead", state: project.RemoteState{FileID: "file", BaseVersion: "2"}, status: project.ProjectStatus{Dirty: true}, remote: "2", want: "local-ahead"},
		{name: "remote ahead", state: project.RemoteState{FileID: "file", BaseVersion: "2"}, remote: "3", want: "remote-ahead"},
		{name: "diverged", state: project.RemoteState{FileID: "file", BaseVersion: "2"}, status: project.ProjectStatus{Dirty: true}, remote: "3", want: "diverged"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := classifySync(test.state, test.status, test.remote)
			if got.State != test.want || got.FirstPush != test.first {
				t.Fatalf("classifySync() = %#v; want state %q, firstPush %v", got, test.want, test.first)
			}
			if test.name == "up to date" && (got.DriveName != "Quarterly update" || got.DriveURL != "https://drive.google.com/example" || got.BaseVersion != "2") {
				t.Fatalf("classifySync() omitted Drive identity: %#v", got)
			}
		})
	}
}

func TestStartRejectsLocalOnlyProject(t *testing.T) {
	root := t.TempDir()
	if _, err := project.Create(root, "Local only"); err != nil {
		t.Fatal(err)
	}
	err := Start(context.Background(), root, false, "test")
	if err == nil || !strings.Contains(err.Error(), "only connected projects") {
		t.Fatalf("Start error = %v", err)
	}
}

func TestDiffHashesReportsFileLevelChanges(t *testing.T) {
	got := diffHashes(
		map[string]string{"removed.md": "old", "same.md": "same", "changed.md": "old"},
		map[string]string{"added.md": "new", "same.md": "same", "changed.md": "new"},
	)
	want := []fileChange{
		{Path: "added.md", Kind: "added"},
		{Path: "changed.md", Kind: "modified"},
		{Path: "removed.md", Kind: "removed"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("diffHashes() = %#v; want %#v", got, want)
	}
}

func TestDecorateFilesSeparatesContentAndTemplates(t *testing.T) {
	files := []fileItem{
		{Path: "documents/report.page.md", Previewable: true},
		{Path: "theme/examples/example.page.md", Previewable: true},
		{Path: "theme/page.css", Editable: true},
		{Path: "theme/page.html.tmpl", Editable: true},
		{Path: "theme/components/callout.tsx", Editable: true},
	}
	decorateFiles(files)
	if files[0].Section != "content" || files[0].Template != "Page template" {
		t.Fatalf("content metadata = %#v", files[0])
	}
	for _, index := range []int{2, 3} {
		if files[index].Section != "templates" || files[index].PreviewPath != "theme/examples/example.page.md" {
			t.Fatalf("template metadata = %#v", files[index])
		}
	}
	if files[4].PreviewPath != "" || files[4].Component != "callout" {
		t.Fatalf("component should have an isolated preview: %#v", files[4])
	}
	if files[2].Label != "Styles" || files[3].Label != "Structure" || files[4].Group != "Components" {
		t.Fatalf("template labels were not assigned: %#v", files)
	}
}

func TestDecorateFilesHidesGeneratedCSSForTailwindTheme(t *testing.T) {
	files := []fileItem{
		{Path: "theme/tailwind.css"},
		{Path: "theme/page.css"},
		{Path: "theme/deck.css"},
	}
	decorateFiles(files)
	if files[0].Group != "Design system" || files[0].Label != "Tailwind theme" {
		t.Fatalf("Tailwind source metadata = %#v", files[0])
	}
	if !files[1].Hidden || !files[2].Hidden {
		t.Fatalf("generated CSS should be hidden: %#v", files)
	}
}

func TestTailwindClassesIncludeProjectUtilitiesAndTokens(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "theme", "components"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "theme", "components", "card.tsx"), []byte(`export default function Card({ children }) { return <article className="grid gap-7 bg-brand">{children}</article>; }`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "theme", "tailwind.css"), []byte(`@theme { --color-brand: oklch(.5 .1 20); --font-editorial: Georgia; }`), 0o644); err != nil {
		t.Fatal(err)
	}
	classes := (&Server{root: root}).tailwindClasses()
	joined := " " + strings.Join(classes, " ") + " "
	for _, want := range []string{" grid ", " gap-7 ", " bg-brand ", " text-brand ", " font-editorial "} {
		if !strings.Contains(joined, want) {
			t.Fatalf("classes missing %q: %v", want, classes)
		}
	}
}
