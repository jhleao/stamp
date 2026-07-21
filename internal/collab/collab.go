package collab

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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

const stampMIME = "application/vnd.weve.stamp+zip"

type VersionInfo struct {
	Message       string `json:"message,omitempty"`
	CreatedAt     string `json:"createdAt"`
	ParentVersion string `json:"parentVersion,omitempty"`
}

type PullMode string

const (
	PullSafe     PullMode = "safe"
	PullIncoming PullMode = "incoming"
	PullReplace  PullMode = "replace"
)

func Open(ctx context.Context, drive *stampdrive.Client, value, destination string) (project.RemoteState, error) {
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
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return project.RemoteState{}, err
	}
	if err := bundle.UnpackReader(bytes.NewReader(contents), int64(len(contents)), destination); err != nil {
		return project.RemoteState{}, err
	}
	current, _, err := drive.FindChildByProperty(ctx, folder.ID, "stamp_kind", "current")
	if err != nil {
		return project.RemoteState{}, err
	}
	hashes, err := project.FileHashes(destination)
	if err != nil {
		return project.RemoteState{}, err
	}
	state := project.RemoteState{FileID: canonical.ID, ProjectFolderID: folder.ID, CurrentFolderID: current.ID, BaseVersion: canonical.Version, BaseHash: hash(contents), WebURL: folder.WebURL, Files: hashes}
	return state, project.WriteState(destination, state)
}

func Pull(ctx context.Context, drive *stampdrive.Client, root string, mode PullMode) (string, error) {
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

func Push(ctx context.Context, drive *stampdrive.Client, root, spaceID, message, forceLease string) (project.RemoteState, error) {
	manifest, err := project.Load(root)
	if err != nil {
		return project.RemoteState{}, err
	}
	if _, err := render.All(root); err != nil {
		return project.RemoteState{}, err
	}
	state, err := project.ReadState(root)
	if err != nil {
		return project.RemoteState{}, err
	}
	if state.FileID == "" {
		if spaceID == "" {
			return project.RemoteState{}, errors.New("first push needs --space <space-id-or-url>")
		}
		if err := createRemote(ctx, drive, root, manifest, stampdrive.ID(spaceID), &state); err != nil {
			return project.RemoteState{}, err
		}
	}
	remote, err := drive.Get(ctx, state.FileID)
	if err != nil {
		return project.RemoteState{}, err
	}
	if state.BaseVersion != "" && remote.Version != state.BaseVersion && forceLease != remote.Version {
		return project.RemoteState{}, fmt.Errorf("push refused: workspace lease is %s but Drive is %s; pull first, or use --force-with-lease %s after reviewing it", state.BaseVersion, remote.Version, remote.Version)
	}
	if forceLease != "" && forceLease != remote.Version {
		return project.RemoteState{}, fmt.Errorf("force lease %s is stale; Drive is %s", forceLease, remote.Version)
	}
	version := VersionInfo{Message: message, CreatedAt: time.Now().UTC().Format(time.RFC3339), ParentVersion: remote.Version}
	versionJSON, _ := json.MarshalIndent(version, "", "  ")
	var archive bytes.Buffer
	if err := bundle.PackWith(root, &archive, map[string][]byte{".stamp/version.json": append(versionJSON, '\n')}); err != nil {
		return project.RemoteState{}, err
	}
	updated, err := drive.UpdateFile(ctx, state.FileID, stampMIME, bytes.NewReader(archive.Bytes()))
	if err != nil {
		return project.RemoteState{}, err
	}
	if err := syncOutputs(ctx, drive, root, state.CurrentFolderID); err != nil {
		return project.RemoteState{}, fmt.Errorf("canonical version %s was pushed, but output mirrors need retrying: %w", updated.Version, err)
	}
	hashes, err := project.FileHashes(root)
	if err != nil {
		return project.RemoteState{}, err
	}
	state.BaseVersion, state.BaseHash, state.Files = updated.Version, hash(archive.Bytes()), hashes
	return state, project.WriteState(root, state)
}

func createRemote(ctx context.Context, drive *stampdrive.Client, root string, manifest project.Manifest, spaceID string, state *project.RemoteState) error {
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
	var archive bytes.Buffer
	if err := bundle.Pack(root, &archive); err != nil {
		return err
	}
	canonical, err := drive.CreateFile(ctx, folder.ID, manifest.Name+".stamp", stampMIME, bytes.NewReader(archive.Bytes()), map[string]string{"stamp_kind": "canonical", "stamp_id": manifest.ID})
	if err != nil {
		return err
	}
	state.FileID, state.ProjectFolderID, state.CurrentFolderID = canonical.ID, folder.ID, current.ID
	state.BaseVersion, state.BaseHash, state.WebURL = canonical.Version, hash(archive.Bytes()), folder.WebURL
	return nil
}

func syncOutputs(ctx context.Context, drive *stampdrive.Client, root, folderID string) error {
	if folderID == "" {
		return errors.New("project has no Current folder")
	}
	return filepath.WalkDir(filepath.Join(root, "outputs"), func(path string, entry fs.DirEntry, err error) error {
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
	})
}

func replaceWorkspace(root string, contents []byte) error {
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
	return bundle.UnpackReader(bytes.NewReader(contents), int64(len(contents)), root)
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
