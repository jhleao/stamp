package collab

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhleao/stamp/internal/bundle"
	stampdrive "github.com/jhleao/stamp/internal/drive"
	"github.com/jhleao/stamp/internal/project"
	"github.com/jhleao/stamp/internal/render"
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
func (f *fakeDrive) ResolveFiles(_ context.Context, _ string, refs []stampdrive.FileRef) (map[string]stampdrive.Item, error) {
	items := make(map[string]stampdrive.Item, len(refs))
	for _, ref := range refs {
		id := ref.ID
		if id == "" {
			id = "selected-" + ref.Key
		}
		items[ref.Key] = stampdrive.Item{ID: id, Name: ref.Name, CanEdit: true}
	}
	return items, nil
}

type recordingDrive struct {
	fakeDrive
	uploaded []byte
}

type blockedOutputDrive struct {
	fakeDrive
	writes int
}

type projectDrive struct {
	items             map[string]stampdrive.Item
	data              []byte
	hideCurrent       bool
	currentAuthorized bool
}

func (f *projectDrive) Get(_ context.Context, id string) (stampdrive.Item, error) {
	item, ok := f.items[id]
	if !ok {
		return stampdrive.Item{}, errors.New("missing Drive item")
	}
	return item, nil
}
func (f *projectDrive) Download(context.Context, string) ([]byte, error) { return f.data, nil }
func (f *projectDrive) FindChildByProperty(_ context.Context, parent, key, value string) (stampdrive.Item, bool, error) {
	if !f.hideCurrent && parent == "project" && key == "stamp_kind" && value == "current" {
		return f.items["current"], true, nil
	}
	return stampdrive.Item{}, false, nil
}
func (f *projectDrive) AuthorizeCurrentFolder(context.Context, stampdrive.Item) (stampdrive.Item, error) {
	f.currentAuthorized = true
	return f.items["current"], nil
}
func (f *projectDrive) EnsureFolder(context.Context, string, string, map[string]string) (stampdrive.Item, error) {
	return stampdrive.Item{}, errors.New("unexpected write")
}
func (f *projectDrive) CreateFile(context.Context, string, string, string, io.Reader, map[string]string) (stampdrive.Item, error) {
	return stampdrive.Item{}, errors.New("unexpected write")
}
func (f *projectDrive) UpdateFile(context.Context, string, string, io.Reader) (stampdrive.Item, error) {
	return stampdrive.Item{}, errors.New("unexpected write")
}
func (f *projectDrive) UpdateNamedFile(context.Context, string, string, string, io.Reader) (stampdrive.Item, error) {
	return stampdrive.Item{}, errors.New("unexpected write")
}
func (f *projectDrive) Trash(context.Context, string) error { return errors.New("unexpected write") }
func (f *projectDrive) Retain(context.Context, stampdrive.Item) error {
	return errors.New("unexpected write")
}
func (f *projectDrive) ResolveFiles(context.Context, string, []stampdrive.FileRef) (map[string]stampdrive.Item, error) {
	return nil, errors.New("unexpected write")
}

func (f *blockedOutputDrive) ResolveFiles(context.Context, string, []stampdrive.FileRef) (map[string]stampdrive.Item, error) {
	return nil, errors.New("published files were not authorized")
}

func (f *blockedOutputDrive) UpdateFile(context.Context, string, string, io.Reader) (stampdrive.Item, error) {
	f.writes++
	return stampdrive.Item{}, nil
}

func (f *blockedOutputDrive) UpdateNamedFile(context.Context, string, string, string, io.Reader) (stampdrive.Item, error) {
	f.writes++
	return stampdrive.Item{}, nil
}

func (f *blockedOutputDrive) CreateFile(context.Context, string, string, string, io.Reader, map[string]string) (stampdrive.Item, error) {
	f.writes++
	return stampdrive.Item{}, nil
}

func (f *recordingDrive) UpdateNamedFile(_ context.Context, _ string, _ string, _ string, contents io.Reader) (stampdrive.Item, error) {
	data, err := io.ReadAll(contents)
	if err != nil {
		return stampdrive.Item{}, err
	}
	f.uploaded = data
	return stampdrive.Item{ID: "canonical", Version: "3"}, nil
}

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

func TestRemoteStateForRequiresSameProjectAndDoesNotChangeLocalFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "local")
	if _, err := project.Create(root, "Shared"); err != nil {
		t.Fatal(err)
	}
	remote := filepath.Join(t.TempDir(), "remote")
	if err := copyTree(root, remote); err != nil {
		t.Fatal(err)
	}
	remoteFile := filepath.Join(remote, "documents", "remote.page.md")
	if err := os.WriteFile(remoteFile, []byte("# Remote\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := bundle.Pack(remote, &archive); err != nil {
		t.Fatal(err)
	}
	drive := &projectDrive{
		items: map[string]stampdrive.Item{
			"canonical": {ID: "canonical", Name: "Shared.stamp", CanEdit: true, Version: "7", Parents: []string{"project"}},
			"project":   {ID: "project", Folder: true, CanEdit: true, WebURL: "https://drive.google.com/folder/project"},
			"current":   {ID: "current", Folder: true, CanEdit: true},
		},
		data:        archive.Bytes(),
		hideCurrent: true,
	}
	state, err := RemoteStateFor(context.Background(), drive, root, "canonical")
	if err != nil {
		t.Fatal(err)
	}
	if state.FileID != "canonical" || state.CurrentFolderID != "current" || state.BaseVersion != "7" {
		t.Fatalf("incomplete remote state: %+v", state)
	}
	if !drive.currentAuthorized {
		t.Fatal("remote did not request authorization for an undiscoverable Current folder")
	}
	if _, err := os.Stat(filepath.Join(root, "documents", "remote.page.md")); !os.IsNotExist(err) {
		t.Fatalf("remote inspection changed local files: %v", err)
	}

	other := filepath.Join(t.TempDir(), "other")
	if _, err := project.Create(other, "Other"); err != nil {
		t.Fatal(err)
	}
	archive.Reset()
	if err := bundle.Pack(other, &archive); err != nil {
		t.Fatal(err)
	}
	drive.data = archive.Bytes()
	if _, err := RemoteStateFor(context.Background(), drive, root, "canonical"); err == nil || !strings.Contains(err.Error(), "is not this workspace") {
		t.Fatalf("expected project identity refusal, got %v", err)
	}
}

func copyTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, contents, 0o644)
	})
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

func TestPushArchiveOmitsDeletedWorkspaceFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "local")
	if _, err := project.Create(root, "Shared"); err != nil {
		t.Fatal(err)
	}
	removed := filepath.Join(root, "documents", "remove-me.page.md")
	if err := os.WriteFile(removed, []byte("# Remove me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hashes, err := project.FileHashes(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := project.WriteState(root, project.RemoteState{
		FileID: "canonical", ProjectFolderID: "project", CurrentFolderID: "current",
		BaseVersion: "2", Files: hashes,
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(removed); err != nil {
		t.Fatal(err)
	}
	drive := &recordingDrive{fakeDrive: fakeDrive{item: stampdrive.Item{ID: "canonical", Version: "2"}}}
	if _, err := push(context.Background(), drive, root, "root", "cleanup", "", func(string) ([]render.Result, error) { return nil, nil }, nil); err != nil {
		t.Fatal(err)
	}
	unpacked := t.TempDir()
	if err := bundle.UnpackReader(bytes.NewReader(drive.uploaded), int64(len(drive.uploaded)), unpacked); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(unpacked, "documents", "remove-me.page.md")); !os.IsNotExist(err) {
		t.Fatalf("deleted file remains in pushed archive: %v", err)
	}
}

func TestPushDoesNotWriteWhenCanonicalOutputsAreNotAuthorized(t *testing.T) {
	root := filepath.Join(t.TempDir(), "local")
	if _, err := project.Create(root, "Shared"); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "outputs", "brief.pdf")
	if err := os.WriteFile(output, []byte("pdf"), 0o644); err != nil {
		t.Fatal(err)
	}
	hashes, err := project.FileHashes(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := project.WriteState(root, project.RemoteState{
		FileID: "canonical", ProjectFolderID: "project", CurrentFolderID: "current",
		BaseVersion: "2", Files: hashes, Outputs: map[string]string{"brief.pdf": "published-pdf"},
	}); err != nil {
		t.Fatal(err)
	}
	drive := &blockedOutputDrive{fakeDrive: fakeDrive{item: stampdrive.Item{ID: "canonical", Version: "2"}}}
	_, err = push(context.Background(), drive, root, "root", "update", "", func(string) ([]render.Result, error) {
		return []render.Result{{Source: "documents/brief.page.md", Output: "outputs/brief.pdf"}}, nil
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "connect published files before Push") {
		t.Fatalf("expected authorization refusal, got %v", err)
	}
	if drive.writes != 0 {
		t.Fatalf("push made %d Drive writes before output authorization", drive.writes)
	}
}
