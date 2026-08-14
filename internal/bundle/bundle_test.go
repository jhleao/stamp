package bundle

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "documents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "stamp.yaml"), []byte("name: Test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "documents", "memo.page.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "test.stamp")
	if err := PackFile(root, archive); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := UnpackFile(archive, out); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, "documents", "memo.page.md"))
	if err != nil || string(data) != "hello" {
		t.Fatalf("round trip: %q, %v", data, err)
	}
}

func TestPackDereferencesClaudeCompatibilityLink(t *testing.T) {
	root := t.TempDir()
	guide := []byte("# Agent guide\n")
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), guide, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("AGENTS.md", filepath.Join(root, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "test.stamp")
	if err := PackFile(root, archive); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := UnpackFile(archive, out); err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(filepath.Join(out, "CLAUDE.md"))
	if err != nil || target != "AGENTS.md" {
		t.Fatalf("CLAUDE.md link = %q, %v", target, err)
	}
	data, err := os.ReadFile(filepath.Join(out, "CLAUDE.md"))
	if err != nil || string(data) != string(guide) {
		t.Fatalf("CLAUDE.md contents = %q, %v", data, err)
	}
}
