package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreate(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	created, err := Create(root, "Board Pack")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded != created || loaded.Name != "Board Pack" {
		t.Fatalf("loaded %#v, created %#v", loaded, created)
	}
	for _, name := range []string{"theme/page.html.tmpl", "theme/examples/welcome-deck.deck.md"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
}
