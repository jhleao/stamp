package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKnownProjectsKeepsRecentConnectedWorkspacesAndPrunesStalePaths(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	first := connectedProject(t, "First")
	second := connectedProject(t, "Second")

	if err := Remember(first); err != nil {
		t.Fatal(err)
	}
	if err := Remember(second); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(first, ManifestName)); err != nil {
		t.Fatal(err)
	}
	second, err := canonicalPath(second)
	if err != nil {
		t.Fatal(err)
	}

	known, err := KnownProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(known) != 1 || known[0].Name != "Second" || known[0].Path != second {
		t.Fatalf("known projects = %#v", known)
	}

	registry, err := readRecentProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Paths) != 1 || registry.Paths[0] != second {
		t.Fatalf("saved paths = %#v", registry.Paths)
	}
}

func connectedProject(t *testing.T, name string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), name)
	if _, err := Create(root, name); err != nil {
		t.Fatal(err)
	}
	if err := WriteState(root, RemoteState{
		FileID:          "archive",
		ProjectFolderID: "project",
		CurrentFolderID: "current",
		BaseVersion:     "v1",
	}); err != nil {
		t.Fatal(err)
	}
	return root
}
