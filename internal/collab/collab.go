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

	"github.com/weve-ai/stamp/internal/bundle"
	stampdrive "github.com/weve-ai/stamp/internal/drive"
	"github.com/weve-ai/stamp/internal/project"
	"github.com/weve-ai/stamp/internal/render"
)

const stampMIME = "application/vnd.stamp+zip"

type VersionInfo struct {
	Message       string `json:"message,omitempty"`
	CreatedAt     string `json:"createdAt"`
	ParentVersion string `json:"parentVersion,omitempty"`
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

func Open(ctx context.Context, drive Drive, value, destination string) (project.RemoteState, error) {
	item, err := drive.Get(ctx, stampdrive.ID(value))
	if err != nil {
		return project.RemoteState{}, err
	}
	var canonical, folder stampdrive.Item
	if item.Folder {
		folder = item
		found, ok, err := drive.FindChildByProperty(ctx, item.ID, "stamp_kind", "canonical")
		if err != nil {
			return project.RemoteState{}, err
		}
		if !ok {
			return project.RemoteState{}, errors.New("Drive folder is not a Stamp project")
		}
		canonical = found
	} else {
		canonical = item
		if len(item.Parents) == 0 {
			return project.RemoteState{}, errors.New("canonical project has no parent folder")
		}
		folder, err = drive.Get(ctx, item.Parents[0])
		if err != nil {
			return project.RemoteState{}, err
		}
	}
	if destination == "" {
		destination = safeName(strings.TrimSuffix(canonical.Name, ".stamp"))
	}
	if entries, err := os.ReadDir(destination); err == nil && len(entries) > 0 {
		return project.RemoteState{}, fmt.Errorf("%s is not empty", destination)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return project.RemoteState{}, err
	}
	contents, err := drive.Download(ctx, canonical.ID)
	if err != nil {
		return project.RemoteState{}, err
	}
	if err := unpackNewWorkspace(contents, destination); err != nil {
		return project.RemoteState{}, err
	}
	if err := project.EnsureAgentCompatibility(destination); err != nil {
		return project.RemoteState{}, err
	}
	current, ok, err := drive.FindChildByProperty(ctx, folder.ID, "stamp_kind", "current")
	if err != nil {
		return project.RemoteState{}, err
	}
	if !ok {
		return project.RemoteState{}, errors.New("Drive project has no Current folder")
	}
	hashes, err := project.FileHashes(destination)
	if err != nil {
		return project.RemoteState{}, err
	}
	state := project.RemoteState{FileID: canonical.ID, ProjectFolderID: folder.ID, CurrentFolderID: current.ID, BaseVersion: canonical.Version, BaseHash: hash(contents), WebURL: folder.WebURL, Files: hashes}
	return state, project.WriteState(destination, state)
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
	state, err := project.ReadState(root)
	if err != nil {
		return "", err
	}
	if state.FileID == "" {
		return "", errors.New("project has no Drive remote; push it to a space first")
	}
	remote, err := drive.Get(ctx, state.FileID)
	if err != nil {
		return "", err
	}
	if remote.Version == state.BaseVersion {
		return "Already up to date.", nil
	}
	hashes, err := project.FileHashes(root)
	if err != nil {
		return "", err
	}
	dirty := !same(hashes, state.Files)
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
	return "Pulled Drive version " + remote.Version, nil
}

func Push(ctx context.Context, drive Drive, root, spaceID, message, forceLease string) (project.RemoteState, error) {
	return push(ctx, drive, root, spaceID, message, forceLease, renderProject)
}

func renderProject(root string) error {
	_, err := render.All(root)
	return err
}

func push(ctx context.Context, drive Drive, root, spaceID, message, forceLease string, renderWorkspace func(string) error) (project.RemoteState, error) {
	manifest, err := project.Load(root)
	if err != nil {
		return project.RemoteState{}, err
	}
	if err := renderWorkspace(root); err != nil {
		return project.RemoteState{}, err
	}
	state, err := project.ReadState(root)
	if err != nil {
		return project.RemoteState{}, err
	}
	firstPush := state.FileID == ""
	if firstPush {
		if spaceID == "" {
			return project.RemoteState{}, errors.New("first push needs --space <space-id-or-url>")
		}
		if err := createRemote(ctx, drive, manifest, stampdrive.ID(spaceID), &state); err != nil {
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
	return state, nil
}

// Reconnect preserves the previous remote state as local recovery metadata and
// publishes the workspace as a new Drive project. The old Drive project is not
// modified. This is primarily useful when changing OAuth applications because
// drive.file grants do not transfer between client IDs.
func Reconnect(ctx context.Context, drive Drive, root, spaceID, message string) (project.RemoteState, string, error) {
	return reconnect(ctx, drive, root, spaceID, message, renderProject)
}

func reconnect(ctx context.Context, drive Drive, root, spaceID, message string, renderWorkspace func(string) error) (project.RemoteState, string, error) {
	if strings.TrimSpace(spaceID) == "" {
		return project.RemoteState{}, "", errors.New("reconnect needs --space <space-id-or-url>")
	}
	previous, err := project.ReadState(root)
	if err != nil {
		return project.RemoteState{}, "", err
	}
	if previous.FileID == "" {
		return project.RemoteState{}, "", errors.New("project is local-only; use stamp push --space instead")
	}
	recoveryDir := filepath.Join(root, ".stamp", "recovery")
	if err := os.MkdirAll(recoveryDir, 0o700); err != nil {
		return project.RemoteState{}, "", err
	}
	recovery := filepath.Join(recoveryDir, "drive-link-"+time.Now().UTC().Format("20060102T150405Z")+".json")
	data, err := json.MarshalIndent(previous, "", "  ")
	if err != nil {
		return project.RemoteState{}, "", err
	}
	if err := os.WriteFile(recovery, append(data, '\n'), 0o600); err != nil {
		return project.RemoteState{}, "", err
	}
	if err := project.WriteState(root, project.RemoteState{}); err != nil {
		return project.RemoteState{}, recovery, err
	}
	state, err := push(ctx, drive, root, spaceID, message, "", renderWorkspace)
	if err != nil {
		if restoreErr := project.WriteState(root, previous); restoreErr != nil {
			return project.RemoteState{}, recovery, fmt.Errorf("reconnect failed: %v; restore previous Drive link: %w", err, restoreErr)
		}
		return project.RemoteState{}, recovery, err
	}
	return state, recovery, nil
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

func createRemote(ctx context.Context, drive Drive, manifest project.Manifest, spaceID string, state *project.RemoteState) error {
	projects, err := drive.EnsureFolder(ctx, spaceID, "Projects", map[string]string{"stamp_kind": "projects"})
	if err != nil {
		return err
	}
	folder, err := drive.EnsureFolder(ctx, projects.ID, manifest.Name, map[string]string{"stamp_kind": "project", "stamp_id": manifest.ID})
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
			_, err = drive.UpdateFile(ctx, item.ID, mime, bytes.NewReader(data))
		} else {
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
			if err := drive.Trash(ctx, item.ID); err != nil {
				return err
			}
		}
	}
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
