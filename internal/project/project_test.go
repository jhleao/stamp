package project

import (
	"os"
	"path/filepath"
	"strings"
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
	for _, name := range []string{"documents/start-here.page.md", "decks/start-here.deck.md", "theme/page.html.tmpl", "theme/tailwind.css", "theme/examples/welcome-deck.deck.md"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	target, err := os.Readlink(filepath.Join(root, "CLAUDE.md"))
	if err != nil || target != "AGENTS.md" {
		t.Fatalf("CLAUDE.md compatibility link = %q, %v", target, err)
	}
	status, err := Status(root)
	if err != nil || !status.Dirty || status.Lease != "local only" {
		t.Fatalf("new project status = %#v, %v", status, err)
	}
}

func TestStarterIsBrandNeutral(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if _, err := Create(root, "Example Project"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"documents/start-here.page.md",
		"decks/start-here.deck.md",
		"theme/README.md",
		"theme/page.html.tmpl",
		"theme/deck.html.tmpl",
		"theme/tailwind.css",
		"theme/components/Callout.tsx",
		"theme/examples/welcome-page.page.md",
		"theme/examples/welcome-deck.deck.md",
	} {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		lower := strings.ToLower(string(data))
		if strings.Contains(lower, "weve") || strings.Contains(lower, "weave") {
			t.Fatalf("starter file %s contains product-specific branding", name)
		}
	}
}

func TestEnsureAgentCompatibilityMigratesOlderProject(t *testing.T) {
	root := t.TempDir()
	if err := EnsureAgentCompatibility(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(filepath.Join(root, "CLAUDE.md"))
	if err != nil || target != "AGENTS.md" {
		t.Fatalf("CLAUDE.md = %q, %v", target, err)
	}
}

func TestConnectedRequiresCompleteRemoteState(t *testing.T) {
	root := t.TempDir()
	for _, test := range []struct {
		state RemoteState
		want  bool
	}{
		{state: RemoteState{}, want: false},
		{state: RemoteState{FileID: "file", ProjectFolderID: "project", CurrentFolderID: "current"}, want: false},
		{state: RemoteState{FileID: "file", ProjectFolderID: "project", CurrentFolderID: "current", BaseVersion: "version"}, want: true},
	} {
		if err := WriteState(root, test.state); err != nil {
			t.Fatal(err)
		}
		got, err := Connected(root)
		if err != nil || got != test.want {
			t.Fatalf("Connected(%+v) = %v, %v; want %v", test.state, got, err, test.want)
		}
	}
}
