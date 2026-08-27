package collab

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jhleao/stamp/internal/bundle"
	"github.com/jhleao/stamp/internal/diagnostic"
	stampdrive "github.com/jhleao/stamp/internal/drive"
	"github.com/jhleao/stamp/internal/project"
	"github.com/jhleao/stamp/internal/render"
)

const stampMIME = "application/vnd.stamp+zip"

type VersionInfo struct {
	Message       string `json:"message,omitempty"`
	CreatedAt     string `json:"createdAt"`
	ParentVersion string `json:"parentVersion,omitempty"`
}

type Workspace struct {
	Root  string
	State project.RemoteState
}

type PullMode string

// Drive is the small storage surface collaboration needs. Keeping it here
// makes the conflict and recovery rules testable without a Google account.
type Drive interface {
	Get(context.Context, string) (stampdrive.Item, error)
	Download(context.Context, string) ([]byte, error)
	FindChildByProperty(context.Context, string, string, string) (stampdrive.Item, bool, error)
	EnsureFolder(context.Context, string, string, map[string]string) (stampdrive.Item, error)
	CreateFile(context.Context, string, string, string, io.Reader, map[string]string) (stampdrive.Item, error)
	UpdateFile(context.Context, string, string, io.Reader) (stampdrive.Item, error)
	UpdateNamedFile(context.Context, string, string, string, io.Reader) (stampdrive.Item, error)
	Children(context.Context, string) ([]stampdrive.Item, error)
	Trash(context.Context, string) error
	Retain(context.Context, stampdrive.Item) error
}

const (
	PullSafe     PullMode = "safe"
	PullIncoming PullMode = "incoming"
	PullReplace  PullMode = "replace"
)

func Open(ctx context.Context, drive Drive, value, destination string) (Workspace, error) {
	diagnostic.Log("collab", "clone.start", "selection", stampdrive.ID(value), "destination", destination)
	item, err := drive.Get(ctx, stampdrive.ID(value))
	if err != nil {
		return Workspace{}, err
	}
	var canonical, folder stampdrive.Item
	if item.Folder {
		diagnostic.Log("collab", "clone.selection", "type", "folder", "file_id", item.ID, "can_edit", item.CanEdit)
		folder = item
		if !item.CanEdit {
			return Workspace{}, errors.New("the selected Drive folder is read-only; ask its owner for Editor access")
		}
		found, ok, err := drive.FindChildByProperty(ctx, item.ID, "stamp_kind", "canonical")
		if err != nil {
			return Workspace{}, err
		}
		if !ok {
			return Workspace{}, errors.New("Drive folder is not a Stamp project, or its archive is not authorized; run stamp clone again and select the .stamp archive inside this folder")
		}
		canonical = found
	} else {
		diagnostic.Log("collab", "clone.selection", "type", "archive", "file_id", item.ID, "name", item.Name, "can_edit", item.CanEdit)
		canonical = item
		if len(item.Parents) == 0 {
			return Workspace{}, errors.New("canonical project has no parent folder")
		}
		folder, err = drive.Get(ctx, item.Parents[0])
		if err != nil {
			return Workspace{}, err
		}
	}
	if destination == "" {
		destination = safeName(strings.TrimSuffix(canonical.Name, ".stamp"))
	}
	if entries, err := os.ReadDir(destination); err == nil && len(entries) > 0 {
		return Workspace{}, fmt.Errorf("%s is not empty", destination)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Workspace{}, err
	}
	contents, err := drive.Download(ctx, canonical.ID)
	if err != nil {
		return Workspace{}, err
	}
	if err := unpackNewWorkspace(contents, destination); err != nil {
		return Workspace{}, err
	}
	if err := project.EnsureAgentCompatibility(destination); err != nil {
		return Workspace{}, err
	}
	current, ok, err := drive.FindChildByProperty(ctx, folder.ID, "stamp_kind", "current")
	if err != nil {
		return Workspace{}, err
	}
	if !ok {
		return Workspace{}, errors.New("Drive project has no Current folder")
	}
	hashes, err := project.FileHashes(destination)
	if err != nil {
		return Workspace{}, err
	}
	state := project.RemoteState{FileID: canonical.ID, ProjectFolderID: folder.ID, CurrentFolderID: current.ID, BaseVersion: canonical.Version, BaseHash: hash(contents), WebURL: folder.WebURL, Files: hashes}
	if err := project.WriteState(destination, state); err != nil {
		return Workspace{}, err
	}
	root, err := filepath.Abs(destination)
	if err != nil {
		return Workspace{}, err
	}
	diagnostic.Log("collab", "clone.complete", "root", root, "version", state.BaseVersion, "files", len(hashes))
	return Workspace{Root: root, State: state}, nil
}

func unpackNewWorkspace(contents []byte, destination string) error {
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(parent, ".stamp-open-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := bundle.UnpackReader(bytes.NewReader(contents), int64(len(contents)), staging); err != nil {
		return fmt.Errorf("validate remote project: %w", err)
	}
	if err := os.Remove(destination); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(staging, destination)
}

func Pull(ctx context.Context, drive Drive, root string, mode PullMode) (string, error) {
	diagnostic.Log("collab", "pull.start", "root", root, "mode", mode)
	state, err := project.ReadState(root)
	if err != nil {
		return "", err
	}
	if state.FileID == "" {
		return "", errors.New("project has no Drive remote; create it with stamp new")
	}
	remote, err := drive.Get(ctx, state.FileID)
	if err != nil {
		return "", err
	}
	if remote.Version == state.BaseVersion {
		diagnostic.Log("collab", "pull.current", "version", remote.Version)
		return "Already up to date.", nil
	}
	hashes, err := project.FileHashes(root)
	if err != nil {
		return "", err
	}
	dirty := !same(hashes, state.Files)
	diagnostic.Log("collab", "pull.compare", "local_version", state.BaseVersion, "remote_version", remote.Version, "local_changed", dirty)
	contents, err := drive.Download(ctx, state.FileID)
	if err != nil {
		return "", err
	}
	if dirty && mode == PullSafe {
		return "", fmt.Errorf("Drive advanced from version %s to %s while local files changed; use pull --incoming or pull --replace", state.BaseVersion, remote.Version)
	}
	if dirty && mode == PullIncoming {
		destination := filepath.Join(root, ".stamp", "incoming", "version-"+remote.Version)
		if err := os.RemoveAll(destination); err != nil {
			return "", err
		}
		if err := os.MkdirAll(destination, 0o755); err != nil {
			return "", err
		}
		if err := bundle.UnpackReader(bytes.NewReader(contents), int64(len(contents)), destination); err != nil {
			return "", err
		}
		diagnostic.Log("collab", "pull.incoming", "destination", destination, "version", remote.Version)
		return "Remote version expanded at " + destination, nil
	}
	if dirty && mode == PullReplace {
		recovery := filepath.Join(root, ".stamp", "recovery", time.Now().UTC().Format("20060102T150405Z")+".stamp")
		if err := os.MkdirAll(filepath.Dir(recovery), 0o755); err != nil {
			return "", err
		}
		if err := bundle.PackFile(root, recovery); err != nil {
			return "", err
		}
		diagnostic.Log("collab", "pull.recovery", "archive", recovery)
	}
	if err := replaceWorkspace(root, contents); err != nil {
		return "", err
	}
	if err := project.EnsureAgentCompatibility(root); err != nil {
		return "", err
	}
	hashes, err = project.FileHashes(root)
	if err != nil {
		return "", err
	}
	state.BaseVersion, state.BaseHash, state.Files = remote.Version, hash(contents), hashes
	if err := project.WriteState(root, state); err != nil {
		return "", err
	}
	diagnostic.Log("collab", "pull.complete", "version", remote.Version, "files", len(hashes))
	return "Pulled Drive version " + remote.Version, nil
}

func Push(ctx context.Context, drive Drive, root, message, forceLease string) (project.RemoteState, error) {
	return push(ctx, drive, root, "root", message, forceLease, renderProject)
}

// Create publishes a new project's first remote version below parentID.
func Create(ctx context.Context, drive Drive, root, parentID string) (project.RemoteState, error) {
	return push(ctx, drive, root, parentID, "Create project", "", renderProject)
}

func renderProject(root string) error {
	_, err := render.All(root)
	return err
}

func push(ctx context.Context, drive Drive, root, parentID, message, forceLease string, renderWorkspace func(string) error) (project.RemoteState, error) {
	diagnostic.Log("collab", "push.start", "root", root, "parent_id", parentID, "force_lease", forceLease != "")
	manifest, err := project.Load(root)
	if err != nil {
		return project.RemoteState{}, err
	}
	renderDone := diagnostic.Start("render", "workspace", "root", root)
	if err := renderWorkspace(root); err != nil {
		renderDone(err)
		return project.RemoteState{}, err
	}
	renderDone(nil)
	state, err := project.ReadState(root)
	if err != nil {
		return project.RemoteState{}, err
	}
	firstPush := state.FileID == ""
	diagnostic.Log("collab", "push.state", "first_push", firstPush, "local_version", state.BaseVersion)
	if firstPush {
		if err := createRemote(ctx, drive, manifest, stampdrive.ID(parentID), &state); err != nil {
			return project.RemoteState{}, err
		}
	}
	remoteVersion := ""
	if !firstPush {
		remote, err := drive.Get(ctx, state.FileID)
		if err != nil {
			return project.RemoteState{}, err
		}
		remoteVersion = remote.Version
		diagnostic.Log("collab", "push.lease", "local_version", state.BaseVersion, "remote_version", remoteVersion, "forced", forceLease != "")
		if err := checkLease(state.BaseVersion, remoteVersion, forceLease); err != nil {
			return project.RemoteState{}, err
		}
	}
	version := VersionInfo{Message: message, CreatedAt: time.Now().UTC().Format(time.RFC3339), ParentVersion: remoteVersion}
	versionJSON, _ := json.MarshalIndent(version, "", "  ")
	var archive bytes.Buffer
	if err := bundle.PackWith(root, &archive, map[string][]byte{".stamp/version.json": append(versionJSON, '\n')}); err != nil {
		return project.RemoteState{}, err
	}
	diagnostic.Log("collab", "push.packed", "bytes", archive.Len(), "parent_version", remoteVersion)
	var updated stampdrive.Item
	if firstPush {
		updated, err = drive.CreateFile(ctx, state.ProjectFolderID, manifest.Name+".stamp", stampMIME, bytes.NewReader(archive.Bytes()), map[string]string{"stamp_kind": "canonical", "stamp_id": manifest.ID})
		state.FileID = updated.ID
	} else {
		updated, err = drive.UpdateNamedFile(ctx, state.FileID, manifest.Name+".stamp", stampMIME, bytes.NewReader(archive.Bytes()))
	}
	if err != nil {
		return project.RemoteState{}, err
	}
	hashes, err := project.FileHashes(root)
	if err != nil {
		return project.RemoteState{}, err
	}
	state.BaseVersion, state.BaseHash, state.Files = updated.Version, hash(archive.Bytes()), hashes
	if err := project.WriteState(root, state); err != nil {
		return state, err
	}
	if err := drive.Retain(ctx, updated); err != nil {
		return state, fmt.Errorf("canonical version %s was pushed, but Drive did not preserve its revision: %w", updated.Version, err)
	}
	if err := syncOutputs(ctx, drive, root, state.CurrentFolderID); err != nil {
		return state, fmt.Errorf("canonical version %s was pushed, but output mirrors need retrying: %w", state.BaseVersion, err)
	}
	diagnostic.Log("collab", "push.complete", "version", state.BaseVersion, "files", len(hashes))
	return state, nil
}

func checkLease(local, remote, forced string) error {
	if local != "" && remote != local && forced != remote {
		return fmt.Errorf("push refused: workspace lease is %s but Drive is %s; pull first, or use --force-with-lease %s after reviewing it", local, remote, remote)
	}
	if forced != "" && forced != remote {
		return fmt.Errorf("force lease %s is stale; Drive is %s", forced, remote)
	}
	return nil
}

func createRemote(ctx context.Context, drive Drive, manifest project.Manifest, parentID string, state *project.RemoteState) error {
	diagnostic.Log("collab", "remote.create", "parent_id", parentID, "name", manifest.Name)
	folder, err := drive.EnsureFolder(ctx, parentID, manifest.Name, map[string]string{"stamp_kind": "project", "stamp_id": manifest.ID})
	if err != nil {
		return err
	}
	current, err := drive.EnsureFolder(ctx, folder.ID, "Current", map[string]string{"stamp_kind": "current"})
	if err != nil {
		return err
	}
	state.ProjectFolderID, state.CurrentFolderID, state.WebURL = folder.ID, current.ID, folder.WebURL
	return nil
}

func syncOutputs(ctx context.Context, drive Drive, root, folderID string) error {
	diagnostic.Log("collab", "outputs.sync_start", "folder_id", folderID)
	if folderID == "" {
		return errors.New("project has no Current folder")
	}
	current := map[string]bool{}
	if err := filepath.WalkDir(filepath.Join(root, "outputs"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(filepath.Join(root, "outputs"), path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)
		current[key] = true
		item, ok, err := drive.FindChildByProperty(ctx, folderID, "stamp_path", key)
		if err != nil {
			return err
		}
		mime := mimeFor(path)
		if ok {
			diagnostic.Log("collab", "outputs.update", "path", key, "file_id", item.ID, "bytes", len(data), "mime", mime)
			_, err = drive.UpdateFile(ctx, item.ID, mime, bytes.NewReader(data))
		} else {
			diagnostic.Log("collab", "outputs.create", "path", key, "bytes", len(data), "mime", mime)
			_, err = drive.CreateFile(ctx, folderID, strings.ReplaceAll(key, "/", " - "), mime, bytes.NewReader(data), map[string]string{"stamp_kind": "output", "stamp_path": key})
		}
		return err
	}); err != nil {
		return err
	}
	items, err := drive.Children(ctx, folderID)
	if err != nil {
		return err
	}
	for _, item := range items {
		if path := item.Props["stamp_path"]; path != "" && !current[path] {
			diagnostic.Log("collab", "outputs.delete", "path", path, "file_id", item.ID)
			if err := drive.Trash(ctx, item.ID); err != nil {
				return err
			}
		}
	}
	diagnostic.Log("collab", "outputs.sync_complete", "files", len(current), "remote_items", len(items))
	return nil
}

func replaceWorkspace(root string, contents []byte) error {
	stagingRoot := filepath.Join(root, ".stamp", "staging")
	if err := os.MkdirAll(stagingRoot, 0o755); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(stagingRoot, "pull-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := bundle.UnpackReader(bytes.NewReader(contents), int64(len(contents)), staging); err != nil {
		return fmt.Errorf("validate remote project: %w", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == ".stamp" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
			return err
		}
	}
	staged, err := os.ReadDir(staging)
	if err != nil {
		return err
	}
	for _, entry := range staged {
		if entry.Name() == ".stamp" {
			continue
		}
		if err := os.Rename(filepath.Join(staging, entry.Name()), filepath.Join(root, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func same(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for name, value := range a {
		if b[name] != value {
			return false
		}
	}
	return true
}

func hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func mimeFor(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdf":
		return "application/pdf"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	default:
		return "application/octet-stream"
	}
}

func safeName(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, string(filepath.Separator), "-")
	if value == "" || value == "." || value == ".." {
		return "stamp-project"
	}
	return value
}
