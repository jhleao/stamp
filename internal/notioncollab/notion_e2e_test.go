package notioncollab

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhleao/stamp/internal/notion"
	"github.com/jhleao/stamp/internal/render"
)

// This live test is intentionally opt-in. It exercises the private test page
// selected by STAMP_NOTION_E2E_ROOT and uses the token already held in Keychain.
func TestNotionEndToEnd(t *testing.T) {
	root := os.Getenv("STAMP_NOTION_E2E_ROOT")
	if root == "" {
		t.Skip("set STAMP_NOTION_E2E_ROOT to a disposable connected project")
	}
	ctx := context.Background()
	client, err := notion.New()
	if err != nil {
		t.Fatal(err)
	}
	state, err := ReadState(root)
	if err != nil {
		t.Fatal(err)
	}

	path := "documents/start-here.page.md"
	pageID, content := findDocument(t, ctx, client, state.PageID, path)
	marker := "Notion collaboration round-trip verified."
	if !strings.Contains(content, marker) {
		if _, err := client.ReplaceMarkdown(ctx, pageID, encodeDocument(path, strings.TrimSpace(content)+"\n\n"+marker+"\n")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Pull(ctx, client, root, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), marker) {
		t.Fatalf("Notion edit did not reach %s", path)
	}

	themePath := filepath.Join(root, "theme", "tailwind.css")
	theme, err := os.ReadFile(themePath)
	if err != nil {
		t.Fatal(err)
	}
	themeMarker := "/* notion-source-round-trip */"
	if !strings.Contains(string(theme), themeMarker) {
		if err := os.WriteFile(themePath, append(theme, []byte("\n"+themeMarker+"\n")...), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pushed, err := Push(ctx, client, root, "Verify native content and source archive round trip", "")
	if err != nil {
		t.Fatal(err)
	}

	checkout := filepath.Join(t.TempDir(), "checkout")
	opened, err := Open(ctx, client, pushed.URL, checkout)
	if err != nil {
		t.Fatal(err)
	}
	if opened.Revision != pushed.Revision {
		t.Fatalf("opened revision %d, want %d", opened.Revision, pushed.Revision)
	}
	recoveredTheme, err := os.ReadFile(filepath.Join(checkout, "theme", "tailwind.css"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(recoveredTheme), themeMarker) {
		t.Fatal("theme source was not recovered from Notion")
	}
	recoveredDocument, err := os.ReadFile(filepath.Join(checkout, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(recoveredDocument), marker) {
		t.Fatal("native Notion document was not recovered")
	}
	if _, err := render.All(checkout); err != nil {
		t.Fatal("recovered project did not render: ", err)
	}
	if _, err := os.Stat(filepath.Join(checkout, "outputs", "documents", "start-here.pdf")); err != nil {
		t.Fatal("rendered output missing after checkout render: ", err)
	}

	staleCheckout := filepath.Join(t.TempDir(), "stale-checkout")
	if _, err := Open(ctx, client, pushed.URL, staleCheckout); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(checkout, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, "assets", "lease-a.txt"), []byte("first writer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	advanced, err := Push(ctx, client, checkout, "Advance source lease for conflict test", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(staleCheckout, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staleCheckout, "assets", "lease-b.txt"), []byte("stale writer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Push(ctx, client, staleCheckout, "This stale push must fail", ""); err == nil || !strings.Contains(err.Error(), "push refused") {
		t.Fatalf("stale push error = %v, want lease refusal", err)
	}
	refreshed, err := Pull(ctx, client, staleCheckout, true)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Revision != advanced.Revision {
		t.Fatalf("pulled revision %d, want %d", refreshed.Revision, advanced.Revision)
	}
	if _, err := os.Stat(filepath.Join(staleCheckout, "assets", "lease-a.txt")); err != nil {
		t.Fatal("replacement pull did not recover winning source: ", err)
	}

	_ = pageID // useful in failures while keeping the helper contract explicit
}

func findDocument(t *testing.T, ctx context.Context, client *notion.Client, projectID, path string) (string, string) {
	t.Helper()
	remote, _, err := remoteArchive(ctx, client, projectID)
	if err != nil {
		t.Fatal(err)
	}
	pageID, err := findDocumentPage(ctx, client, remote, path)
	if err != nil {
		t.Fatal(err)
	}
	if pageID == "" {
		t.Fatalf("Notion document %s not found", path)
	}
	markdown, err := client.Markdown(ctx, pageID)
	if err != nil {
		t.Fatal(err)
	}
	return pageID, decodeDocument(path, markdown.Markdown)
}
