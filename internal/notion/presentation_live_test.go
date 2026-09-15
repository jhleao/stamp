package notion

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestStyleLivePage(t *testing.T) {
	id := os.Getenv("STAMP_NOTION_STYLE_PAGE")
	if id == "" {
		t.Skip("explicit page required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	c, err := New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	before, err := c.Inspect(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := c.readCatalog(ctx, before)
	if err != nil {
		t.Fatal(err)
	}
	after, err := c.Style(ctx, before)
	if err != nil {
		t.Fatal(err)
	}
	if after.ArchiveID != before.ArchiveID || after.Lease != before.Lease || len(after.Revisions) != len(before.Revisions) {
		t.Fatal("styling changed the archive identity or source version")
	}
	oldArchives := map[string]string{}
	for _, r := range before.Revisions {
		oldArchives[r.ID] = r.BlockID
	}
	for _, r := range after.Revisions {
		if oldArchives[r.ID] != r.BlockID {
			t.Fatal("styling changed a historical archive identity")
		}
		block, err := c.Block(ctx, r.BlockID)
		if err != nil {
			t.Fatal(err)
		}
		if caption(block) != "" {
			t.Fatal("revision metadata remains visible on the archive")
		}
	}
	next, err := c.verifyCatalog(ctx, after)
	if err != nil {
		t.Fatal(err)
	}
	for key, file := range inventory.Files {
		if next.Files[key] != file {
			t.Fatal("styling changed a PDF or page identity")
		}
	}
	if _, err = c.Contents(ctx, after); err != nil {
		t.Fatal(err)
	}
	t.Log("Styled existing page; source archive and all output identities preserved")
}
