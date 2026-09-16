package notion

import (
	"context"
	"os"
	"testing"
	"time"
)

// Opt-in migration check: only rewrites presentation on the explicitly supplied
// project, verifying every hosted file and the immutable archive history.
func TestMigratePublishedTreeLive(t *testing.T) {
	id := os.Getenv("STAMP_NOTION_TREE_PAGE")
	if id == "" {
		t.Skip("explicit page required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	c, err := New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	before, err := c.Inspect(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	old, err := c.verifyCatalog(ctx, before)
	if err != nil {
		t.Fatal(err)
	}
	hashes := map[string]string{}
	for key, file := range old.Files {
		data, err := c.Download(ctx, file.BlockID)
		if err != nil {
			t.Fatal(err)
		}
		hashes[key] = Digest(data)
	}
	t.Logf("Migrating %s: %d files, %d folders", before.Name, len(old.Files), len(old.Folders))
	if err = c.MigratePublishedTree(ctx, before); err != nil {
		t.Fatal(err)
	}
	after, err := c.Inspect(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if after.ArchiveID != before.ArchiveID || after.Lease != before.Lease || len(after.Revisions) != len(before.Revisions) {
		t.Fatal("migration changed source archive or revisions")
	}
	oldRevisions := map[string]string{}
	for _, r := range before.Revisions {
		oldRevisions[r.ID] = r.BlockID
	}
	for _, r := range after.Revisions {
		if oldRevisions[r.ID] != r.BlockID {
			t.Fatal("historical archive changed")
		}
	}
	next, err := c.verifyCatalog(ctx, after)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Files) != len(old.Files) || len(next.Folders) != len(old.Folders) {
		t.Fatal("migration lost catalog entries")
	}
	for key, file := range next.Files {
		data, err := c.Download(ctx, file.BlockID)
		if err != nil {
			t.Fatal(err)
		}
		if Digest(data) != hashes[key] {
			t.Fatalf("output changed: %s", key)
		}
		b, err := c.Block(ctx, file.PageID)
		if err != nil {
			t.Fatal(err)
		}
		if String(b, "type") != "toggle" {
			t.Fatal("file is not a toggle")
		}
	}
	if err = c.MigratePublishedTree(ctx, after); err != nil {
		t.Fatal(err)
	}
	again, err := c.Inspect(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if again.CurrentID != after.CurrentID || again.CatalogID != after.CatalogID {
		t.Fatal("repeated migration replaced the tree")
	}
	t.Logf("Verified %d files byte-for-byte; archive and history unchanged", len(hashes))
}
