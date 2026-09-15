package collab

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jhleao/stamp/internal/bundle"
	"github.com/jhleao/stamp/internal/notion"
	"github.com/jhleao/stamp/internal/project"
)

// Opt in only with a dedicated test parent and a saved or environment token. The test
// archives only the child project it creates, never the supplied parent.
func TestLiveNotionRoundTrip(t *testing.T) {
	parent := os.Getenv("STAMP_NOTION_TEST_PARENT")
	if parent == "" {
		t.Skip("set STAMP_NOTION_TEST_PARENT and authenticate with stamp login notion or STAMP_NOTION_TOKEN for live verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	remote, err := Connect(ctx, "notion")
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "author")
	if _, err = project.Create(root, "Stamp live integration test"); err != nil {
		t.Fatal(err)
	}
	state, err := remote.Create(ctx, root, parent)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := remote.notion.TrashPage(cleanupCtx, state.FileID); err != nil {
			t.Logf("test page cleanup failed: %s: %v", state.WebURL, err)
		}
	})
	snapshot, err := remote.notion.Inspect(ctx, state.FileID)
	if err != nil {
		t.Fatal(err)
	}
	canonicalID := snapshot.ArchiveID
	clone := filepath.Join(t.TempDir(), "clone")
	if _, err = remote.Open(ctx, state.FileID, clone); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "documents", "start-here.page.md")
	original, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(source, append(original, []byte("\nLive update.\n")...), 0644); err != nil {
		t.Fatal(err)
	}
	next, err := remote.Push(ctx, root, "Live update", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	after, err := remote.notion.Inspect(ctx, state.FileID)
	if err != nil {
		t.Fatal(err)
	}
	if after.ArchiveID != canonicalID {
		t.Fatal("canonical attachment identity changed")
	}
	if _, err = remote.Push(ctx, clone, "Stale update", "", nil); err == nil {
		t.Fatal("stale writer was accepted")
	}
	if _, err = remote.Pull(ctx, clone, PullSafe); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(clone, "documents", "start-here.page.md"))
	want, _ := os.ReadFile(source)
	if !bytes.Equal(got, want) {
		t.Fatal("archive round trip changed source bytes")
	}

	// Publish two immutable archive children of the same head, reproducing the
	// state left by a real race between append requests without timing guesses.
	for _, message := range []string{"Concurrent Alice", "Concurrent Bob"} {
		var archive bytes.Buffer
		metadata, _ := json.Marshal(VersionInfo{Message: message, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), ParentVersion: next.BaseVersion})
		if err = bundle.PackWith(root, &archive, map[string][]byte{".stamp/version.json": metadata}); err != nil {
			t.Fatal(err)
		}
		upload, err := remote.notion.Upload(ctx, "revision.stamp.zip", "application/zip", archive.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		revision, _ := json.Marshal(notion.Revision{ID: notion.Digest(archive.Bytes()), Parents: []string{next.BaseVersion}})
		if _, err = remote.notion.Append(ctx, after.HistoryID, notion.FileBlock("file", upload, "revision.stamp", "stamp-revision:"+string(revision))); err != nil {
			t.Fatal(err)
		}
	}
	conflicted, err := remote.notion.Inspect(ctx, state.FileID)
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicted.Heads) != 2 {
		t.Fatalf("expected two retained heads, got %d", len(conflicted.Heads))
	}
	if _, err = remote.Pull(ctx, clone, PullSafe); err == nil {
		t.Fatal("pull chose a winner in a concurrent conflict")
	}
	resolved, err := remote.Push(ctx, root, "Resolve reviewed conflict", conflicted.Lease, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(resolved.BaseVersion, "conflict-") {
		t.Fatal("conflict remained unresolved")
	}
	if _, err = remote.Pull(ctx, clone, PullSafe); err != nil {
		t.Fatal(err)
	}
	t.Logf("verified Notion round trip, stable archive ID, stale rejection, retained conflict and force resolution: %s", state.WebURL)
}
