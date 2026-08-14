package collab

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weve-ai/stamp/internal/bundle"
	stampdrive "github.com/weve-ai/stamp/internal/drive"
	"github.com/weve-ai/stamp/internal/project"
)

type fakeDrive struct {
	item stampdrive.Item
	data []byte
}

func (f *fakeDrive) Get(context.Context, string) (stampdrive.Item, error) { return f.item, nil }
func (f *fakeDrive) Download(context.Context, string) ([]byte, error)     { return f.data, nil }
func (f *fakeDrive) FindChildByProperty(context.Context, string, string, string) (stampdrive.Item, bool, error) {
	return stampdrive.Item{}, false, nil
}
func (f *fakeDrive) EnsureFolder(_ context.Context, _ string, name string, _ map[string]string) (stampdrive.Item, error) {
	return stampdrive.Item{ID: strings.ToLower(name), WebURL: "https://drive.google.com/folder/" + strings.ToLower(name)}, nil
}
func (f *fakeDrive) CreateFile(context.Context, string, string, string, io.Reader, map[string]string) (stampdrive.Item, error) {
	return stampdrive.Item{ID: "new-canonical", Version: "new-version"}, nil
}
func (f *fakeDrive) UpdateFile(context.Context, string, string, io.Reader) (stampdrive.Item, error) {
	return stampdrive.Item{}, nil
}
func (f *fakeDrive) UpdateNamedFile(context.Context, string, string, string, io.Reader) (stampdrive.Item, error) {
	return stampdrive.Item{}, nil
}
func (f *fakeDrive) Children(context.Context, string) ([]stampdrive.Item, error) { return nil, nil }
func (f *fakeDrive) Trash(context.Context, string) error                         { return nil }
func (f *fakeDrive) Retain(context.Context, stampdrive.Item) error               { return nil }

func pullFixture(t *testing.T) (string, *fakeDrive) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "local")
	if _, err := project.Create(root, "Shared"); err != nil {
		t.Fatal(err)
	}
	hashes, err := project.FileHashes(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := project.WriteState(root, project.RemoteState{FileID: "canonical", BaseVersion: "1", Files: hashes}); err != nil {
		t.Fatal(err)
	}
	remote := filepath.Join(t.TempDir(), "remote")
	if _, err := project.Create(remote, "Shared"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remote, "documents", "remote.page.md"), []byte("# From teammate\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := bundle.Pack(remote, &archive); err != nil {
		t.Fatal(err)
	}
	return root, &fakeDrive{item: stampdrive.Item{ID: "canonical", Version: "2"}, data: archive.Bytes()}
}

func TestPullSafeRefusesDivergedWorkspace(t *testing.T) {
	root, drive := pullFixture(t)
	if err := os.WriteFile(filepath.Join(root, "documents", "local.page.md"), []byte("# Local work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Pull(context.Background(), drive, root, PullSafe)
	if err == nil || !strings.Contains(err.Error(), "while local files changed") {
		t.Fatalf("expected divergence refusal, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "documents", "local.page.md")); err != nil {
		t.Fatal("safe pull removed local work")
	}
}

func TestPullIncomingPreservesLocalAndExpandsRemote(t *testing.T) {
	root, drive := pullFixture(t)
	local := filepath.Join(root, "documents", "local.page.md")
	if err := os.WriteFile(local, []byte("# Local work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	message, err := Pull(context.Background(), drive, root, PullIncoming)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(message, "version-2") {
		t.Fatalf("message = %q", message)
	}
	if _, err := os.Stat(local); err != nil {
		t.Fatal("incoming pull removed local work")
	}
	if _, err := os.Stat(filepath.Join(root, ".stamp", "incoming", "version-2", "documents", "remote.page.md")); err != nil {
		t.Fatal("incoming version was not expanded")
	}
}

func TestPullReplaceCreatesRecoveryBeforeReplacing(t *testing.T) {
	root, drive := pullFixture(t)
	if err := os.WriteFile(filepath.Join(root, "documents", "local.page.md"), []byte("# Local work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	message, err := Pull(context.Background(), drive, root, PullReplace)
	if err != nil {
		t.Fatal(err)
	}
	if message != "Pulled Drive version 2" {
		t.Fatalf("message = %q", message)
	}
	if _, err := os.Stat(filepath.Join(root, "documents", "remote.page.md")); err != nil {
		t.Fatal("remote workspace was not installed")
	}
	if _, err := os.Stat(filepath.Join(root, "documents", "local.page.md")); !os.IsNotExist(err) {
		t.Fatal("local workspace was not replaced")
	}
	recoveries, err := filepath.Glob(filepath.Join(root, ".stamp", "recovery", "*.stamp"))
	if err != nil || len(recoveries) != 1 {
		t.Fatalf("recovery archives = %v, %v", recoveries, err)
	}
	data, err := os.ReadFile(recoveries[0])
	if err != nil || !bytes.Contains(data, []byte("local.page.md")) {
		t.Fatalf("recovery does not contain local work: %v", err)
	}
	state, err := project.ReadState(root)
	if err != nil || state.BaseVersion != "2" {
		t.Fatalf("state = %#v, %v", state, err)
	}
}

func TestPullValidatesRemoteBeforeReplacingCleanWorkspace(t *testing.T) {
	root, drive := pullFixture(t)
	drive.data = []byte("not a stamp archive")
	_, err := Pull(context.Background(), drive, root, PullSafe)
	if err == nil || !strings.Contains(err.Error(), "validate remote project") {
		t.Fatalf("expected validation error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "documents", "start-here.page.md")); err != nil {
		t.Fatal("invalid remote removed the local workspace")
	}
}

func TestPushLeaseRequiresTheObservedRemoteVersion(t *testing.T) {
	tests := []struct {
		name, local, remote, forced string
		wantError                   bool
	}{
		{name: "same lease", local: "2", remote: "2"},
		{name: "remote advanced", local: "2", remote: "3", wantError: true},
		{name: "exact reviewed override", local: "2", remote: "3", forced: "3"},
		{name: "stale override", local: "2", remote: "4", forced: "3", wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := checkLease(test.local, test.remote, test.forced)
			if (err != nil) != test.wantError {
				t.Fatalf("checkLease() error = %v, wantError %v", err, test.wantError)
			}
		})
	}
}

func TestReconnectPreservesOldLinkAndPublishesNewProject(t *testing.T) {
	root, drive := pullFixture(t)
	compiled := time.Now().Add(time.Second)
	for _, name := range []string{"page.css", "deck.css"} {
		path := filepath.Join(root, "theme", name)
		if err := os.Chtimes(path, compiled, compiled); err != nil {
			t.Fatal(err)
		}
	}
	state, recovery, err := Reconnect(context.Background(), drive, root, "space", "Migrate Drive client")
	if err != nil {
		t.Fatal(err)
	}
	if state.FileID != "new-canonical" || state.BaseVersion != "new-version" {
		t.Fatalf("new state = %#v", state)
	}
	data, err := os.ReadFile(recovery)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"fileId": "canonical"`)) || !bytes.Contains(data, []byte(`"baseVersion": "1"`)) {
		t.Fatalf("recovery did not preserve previous link: %s", data)
	}
}
