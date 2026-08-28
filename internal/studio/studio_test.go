package studio

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jhleao/stamp/internal/project"
	stamptheme "github.com/jhleao/stamp/internal/theme"
)

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
	if err := os.WriteFile(filepath.Join(theme, "components", "Card.tsx"), []byte(`export default function Card({ children }) { return <div className="grid gap-7">{children}</div>; }`), 0o644); err != nil {
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
	server := &Server{root: root, clients: map[chan serverEvent]struct{}{}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "/api/components", strings.NewReader(`{"name":"MetricCard"}`))
	server.createComponent(recorder, request)
	if recorder.Code != 200 {
		t.Fatalf("createComponent status = %d: %s", recorder.Code, recorder.Body.String())
	}
	path := filepath.Join(root, "theme", "components", "MetricCard.tsx")
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), `className="my-6`) || !strings.Contains(string(data), `props.label`) {
		t.Fatalf("component = %q, %v", data, err)
	}
}

func TestFileTreeOperations(t *testing.T) {
	root := t.TempDir()
	documents := filepath.Join(root, "documents")
	if err := os.MkdirAll(documents, 0o755); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(documents, "brief.page.md")
	if err := os.WriteFile(original, []byte("# Brief\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := &Server{root: root, clients: map[chan serverEvent]struct{}{}}

	duplicateRecorder := httptest.NewRecorder()
	duplicateRequest := httptest.NewRequest("POST", "/api/file/duplicate", strings.NewReader(`{"path":"documents/brief.page.md"}`))
	server.duplicateFile(duplicateRecorder, duplicateRequest)
	if duplicateRecorder.Code != 200 {
		t.Fatalf("duplicate status = %d: %s", duplicateRecorder.Code, duplicateRecorder.Body.String())
	}
	copyPath := filepath.Join(documents, "brief-copy.page.md")
	if data, err := os.ReadFile(copyPath); err != nil || string(data) != "# Brief\n" {
		t.Fatalf("duplicate = %q, %v", data, err)
	}

	renameRecorder := httptest.NewRecorder()
	renameRequest := httptest.NewRequest("PATCH", "/api/file", strings.NewReader(`{"path":"documents/brief-copy.page.md","name":"review.page.md"}`))
	server.renameFile(renameRecorder, renameRequest)
	if renameRecorder.Code != 200 {
		t.Fatalf("rename status = %d: %s", renameRecorder.Code, renameRecorder.Body.String())
	}
	renamed := filepath.Join(documents, "review.page.md")
	if _, err := os.Stat(renamed); err != nil {
		t.Fatalf("renamed file: %v", err)
	}

	deleteRecorder := httptest.NewRecorder()
	deleteRequest := httptest.NewRequest("DELETE", "/api/file?path=documents/review.page.md", nil)
	server.deleteFile(deleteRecorder, deleteRequest)
	if deleteRecorder.Code != 200 {
		t.Fatalf("delete status = %d: %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
	if _, err := os.Stat(renamed); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted file still exists: %v", err)
	}
}

func TestFolderTreeOperations(t *testing.T) {
	root := t.TempDir()
	client := filepath.Join(root, "documents", "Client")
	if err := os.MkdirAll(client, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(client, "brief.page.md"), []byte("# Brief\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := &Server{root: root, clients: map[chan serverEvent]struct{}{}}

	renameRecorder := httptest.NewRecorder()
	renameRequest := httptest.NewRequest("PATCH", "/api/folder", strings.NewReader(`{"path":"documents/Client","name":"Customer"}`))
	server.renameFolder(renameRecorder, renameRequest)
	if renameRecorder.Code != 200 {
		t.Fatalf("rename folder status = %d: %s", renameRecorder.Code, renameRecorder.Body.String())
	}
	renamed := filepath.Join(root, "documents", "Customer")
	if _, err := os.Stat(filepath.Join(renamed, "brief.page.md")); err != nil {
		t.Fatalf("renamed folder content: %v", err)
	}

	deleteRecorder := httptest.NewRecorder()
	deleteRequest := httptest.NewRequest("DELETE", "/api/folder?path=documents/Customer", nil)
	server.deleteFolder(deleteRecorder, deleteRequest)
	if deleteRecorder.Code != 200 {
		t.Fatalf("delete folder status = %d: %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
	if _, err := os.Stat(renamed); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted folder still exists: %v", err)
	}
}

func TestMoveFilesMovesSelectionAndRejectsCollisionsBeforeChangingAnything(t *testing.T) {
	root := t.TempDir()
	for name, contents := range map[string]string{
		"documents/One/alpha.page.md":    "# Alpha\n",
		"documents/Two/beta.page.md":     "# Beta\n",
		"documents/Target/index.page.md": "# Target\n",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	server := &Server{root: root, clients: map[chan serverEvent]struct{}{}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "/api/files/move", strings.NewReader(`{"paths":["documents/One/alpha.page.md","documents/Two/beta.page.md"],"destination":"documents/Target"}`))
	server.moveFiles(recorder, request)
	if recorder.Code != 200 {
		t.Fatalf("move files status = %d: %s", recorder.Code, recorder.Body.String())
	}
	for _, name := range []string{"alpha.page.md", "beta.page.md"} {
		if _, err := os.Stat(filepath.Join(root, "documents", "Target", name)); err != nil {
			t.Fatalf("moved %s: %v", name, err)
		}
	}

	if err := os.WriteFile(filepath.Join(root, "documents", "One", "keep.page.md"), []byte("# Keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "documents", "Two", "index.page.md"), []byte("# Collision\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	collisionRecorder := httptest.NewRecorder()
	collisionRequest := httptest.NewRequest("POST", "/api/files/move", strings.NewReader(`{"paths":["documents/One/keep.page.md","documents/Two/index.page.md"],"destination":"documents/Target"}`))
	server.moveFiles(collisionRecorder, collisionRequest)
	if collisionRecorder.Code != 409 {
		t.Fatalf("collision status = %d: %s", collisionRecorder.Code, collisionRecorder.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "documents", "One", "keep.page.md")); err != nil {
		t.Fatalf("validation moved a file before reporting collision: %v", err)
	}
}

func TestSplitFileNameKeepsStampSuffixTogether(t *testing.T) {
	stem, suffix := splitFileName("quarterly.page.md")
	if stem != "quarterly" || suffix != ".page.md" {
		t.Fatalf("splitFileName = %q, %q", stem, suffix)
	}
}

func TestValidFileName(t *testing.T) {
	if !validFileName("x0-review.page.md") {
		t.Fatal("ordinary file name was rejected")
	}
	for _, name := range []string{"", "..", "nested/review.page.md", `nested\review.page.md`} {
		if validFileName(name) {
			t.Fatalf("unsafe file name %q was accepted", name)
		}
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
		{Path: "theme/components/Callout.tsx", Editable: true},
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
	if files[4].PreviewPath != "" || files[4].Component != "Callout" {
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
	if err := os.WriteFile(filepath.Join(root, "theme", "components", "Card.tsx"), []byte(`export default function Card({ children }) { return <article className="grid gap-7 bg-brand">{children}</article>; }`), 0o644); err != nil {
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
