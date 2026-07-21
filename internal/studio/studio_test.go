package studio

import (
	"path/filepath"
	"testing"
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
