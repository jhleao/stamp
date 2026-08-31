package collab

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	Trash(context.Context, string) error
	Retain(context.Context, stampdrive.Item) error
	ResolveFiles(context.Context, string, []stampdrive.FileRef) (map[string]stampdrive.Item, error)
}

type currentFolderAuthorizer interface {
	AuthorizeCurrentFolder(context.Context, stampdrive.Item) (stampdrive.Item, error)
}

const (
	PullSafe     PullMode = "safe"
	PullIncoming PullMode = "incoming"
	PullReplace  PullMode = "replace"
)

func Open(ctx context.Context, drive Drive, value, destination string) (Workspace, error) {
	diagnostic.Log("collab", "clone.start", "selection", stampdrive.ID(value), "destination", destination)
	canonical, folder, current, contents, err := inspectDriveProject(ctx, drive, value)
	if err != nil {
		return Workspace{}, err
	}
	if destination == "" {
		destination = safeName(strings.TrimSuffix(canonical.Name, ".stamp"))
	}
	if entries, err := os.ReadDir(destination); err == nil && len(entries) > 0 {
		return Workspace{}, fmt.Errorf("%s is not empty", destination)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Workspace{}, err
	}
	if err := unpackNewWorkspace(contents, destination); err != nil {
		return Workspace{}, err
	}
	if err := project.EnsureAgentCompatibility(destination); err != nil {
		return Workspace{}, err
	}
	hashes, err := project.FileHashes(destination)
	if err != nil {
		return Workspace{}, err
	}
	outputs, err := readOutputLedger(contents)
	if err != nil {
		return Workspace{}, err
	}
	state := project.RemoteState{FileID: canonical.ID, ProjectFolderID: folder.ID, CurrentFolderID: current.ID, BaseVersion: canonical.Version, BaseHash: hash(contents), WebURL: folder.WebURL, Files: hashes, Outputs: outputs}
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

// RemoteStateFor validates an existing Drive project and returns the complete
// state needed to connect root to it. It never changes the local workspace.
func RemoteStateFor(ctx context.Context, drive Drive, root, value string) (project.RemoteState, error) {
	canonical, folder, current, contents, err := inspectDriveProject(ctx, drive, value)
	if err != nil {
		return project.RemoteState{}, err
	}
	temporary, err := os.MkdirTemp("", "stamp-remote-")
	if err != nil {
		return project.RemoteState{}, err
	}
	defer os.RemoveAll(temporary)
	if err := bundle.UnpackReader(bytes.NewReader(contents), int64(len(contents)), temporary); err != nil {
		return project.RemoteState{}, fmt.Errorf("validate remote project: %w", err)
	}
	localManifest, err := project.Load(root)
	if err != nil {
		return project.RemoteState{}, err
	}
	remoteManifest, err := project.Load(temporary)
	if err != nil {
		return project.RemoteState{}, err
	}
	if localManifest.ID != remoteManifest.ID {
		return project.RemoteState{}, fmt.Errorf("selected Drive project %q is not this workspace (%q)", remoteManifest.Name, localManifest.Name)
	}
	hashes, err := project.FileHashes(temporary)
	if err != nil {
		return project.RemoteState{}, err
	}
	outputs, err := readOutputLedger(contents)
	if err != nil {
		return project.RemoteState{}, err
	}
	return project.RemoteState{
		FileID: canonical.ID, ProjectFolderID: folder.ID, CurrentFolderID: current.ID,
		BaseVersion: canonical.Version, BaseHash: hash(contents), WebURL: folder.WebURL,
		Files: hashes, Outputs: outputs,
	}, nil
}

func inspectDriveProject(ctx context.Context, drive Drive, value string) (stampdrive.Item, stampdrive.Item, stampdrive.Item, []byte, error) {
	item, err := drive.Get(ctx, stampdrive.ID(value))
	if err != nil {
		return stampdrive.Item{}, stampdrive.Item{}, stampdrive.Item{}, nil, err
	}
	var canonical, folder stampdrive.Item
	if item.Folder {
		diagnostic.Log("collab", "remote.selection", "type", "folder", "file_id", item.ID, "can_edit", item.CanEdit)
		folder = item
		if !item.CanEdit {
			return stampdrive.Item{}, stampdrive.Item{}, stampdrive.Item{}, nil, errors.New("the selected Drive folder is read-only; ask its owner for Editor access")
		}
		found, ok, err := drive.FindChildByProperty(ctx, item.ID, "stamp_kind", "canonical")
		if err != nil {
			return stampdrive.Item{}, stampdrive.Item{}, stampdrive.Item{}, nil, err
		}
		if !ok {
			return stampdrive.Item{}, stampdrive.Item{}, stampdrive.Item{}, nil, errors.New("Drive folder is not a Stamp project, or its archive is not authorized; select the .stamp archive inside this folder")
		}
		canonical = found
	} else {
		diagnostic.Log("collab", "remote.selection", "type", "archive", "file_id", item.ID, "name", item.Name, "can_edit", item.CanEdit)
		canonical = item
		if !item.CanEdit {
			return stampdrive.Item{}, stampdrive.Item{}, stampdrive.Item{}, nil, errors.New("the selected .stamp file is read-only; ask its owner for Editor access")
		}
		if len(item.Parents) == 0 {
			return stampdrive.Item{}, stampdrive.Item{}, stampdrive.Item{}, nil, errors.New("canonical project has no parent folder")
		}
		folder, err = drive.Get(ctx, item.Parents[0])
		if err != nil {
			return stampdrive.Item{}, stampdrive.Item{}, stampdrive.Item{}, nil, err
		}
	}
	current, ok, err := drive.FindChildByProperty(ctx, folder.ID, "stamp_kind", "current")
	if err != nil {
		return stampdrive.Item{}, stampdrive.Item{}, stampdrive.Item{}, nil, err
	}
	if !ok {
		authorizer, supported := drive.(currentFolderAuthorizer)
		if !supported {
			return stampdrive.Item{}, stampdrive.Item{}, stampdrive.Item{}, nil, errors.New("Drive project has no Current folder")
		}
		current, err = authorizer.AuthorizeCurrentFolder(ctx, folder)
		if err != nil {
			return stampdrive.Item{}, stampdrive.Item{}, stampdrive.Item{}, nil, fmt.Errorf("connect the project’s Current folder: %w", err)
		}
	}
	contents, err := drive.Download(ctx, canonical.ID)
	if err != nil {
		return stampdrive.Item{}, stampdrive.Item{}, stampdrive.Item{}, nil, err
	}
	return canonical, folder, current, contents, nil
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
	outputs, err := readOutputLedger(contents)
	if err != nil {
		return "", err
	}
	state.BaseVersion, state.BaseHash, state.Files, state.Outputs = remote.Version, hash(contents), hashes, outputs
	if err := project.WriteState(root, state); err != nil {
		return "", err
	}
	diagnostic.Log("collab", "pull.complete", "version", remote.Version, "files", len(hashes))
	return "Pulled Drive version " + remote.Version, nil
}

func Push(ctx context.Context, drive Drive, root, message, forceLease string) (project.RemoteState, error) {
	return push(ctx, drive, root, "root", message, forceLease, renderProject, nil)
}

type PushProgress struct {
	Stage     string `json:"stage"`
	Detail    string `json:"detail,omitempty"`
	Completed int    `json:"completed"`
	Total     int    `json:"total"`
	Percent   int    `json:"percent"`
}

type PushProgressFunc func(PushProgress)

func PushWithProgress(ctx context.Context, drive Drive, root, message, forceLease string, progress PushProgressFunc) (project.RemoteState, error) {
	renderWithProgress := func(root string) ([]render.Result, error) {
		return render.AllWithProgress(root, func(completed, total int, source string) {
			percent := 5
			if total > 0 {
				percent += completed * 45 / total
			}
			if progress != nil {
				progress(PushProgress{Stage: "Rendering documents", Detail: source, Completed: completed, Total: total, Percent: percent})
			}
		})
	}
	return push(ctx, drive, root, "root", message, forceLease, renderWithProgress, progress)
}

// Create publishes a new project's first remote version below parentID.
func Create(ctx context.Context, drive Drive, root, parentID string) (project.RemoteState, error) {
	return push(ctx, drive, root, parentID, "Create project", "", renderProject, nil)
}

func renderProject(root string) ([]render.Result, error) {
	return render.All(root)
}

func push(ctx context.Context, drive Drive, root, parentID, message, forceLease string, renderWorkspace func(string) ([]render.Result, error), progress PushProgressFunc) (project.RemoteState, error) {
	report := func(update PushProgress) {
		if progress != nil {
			progress(update)
		}
	}
	diagnostic.Log("collab", "push.start", "root", root, "parent_id", parentID, "force_lease", forceLease != "")
	report(PushProgress{Stage: "Preparing workspace", Percent: 2})
	manifest, err := project.Load(root)
	if err != nil {
		return project.RemoteState{}, err
	}
	renderDone := diagnostic.Start("render", "workspace", "root", root)
	results, err := renderWorkspace(root)
	if err != nil {
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
		report(PushProgress{Stage: "Checking Google Drive", Percent: 52})
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
	outputFiles, err := collectOutputs(root, results)
	if err != nil {
		return project.RemoteState{}, err
	}
	resolved := map[string]stampdrive.Item{}
	if !firstPush {
		refs := outputRefs(outputFiles, state.Outputs)
		if len(refs) > 0 {
			resolved, err = drive.ResolveFiles(ctx, state.CurrentFolderID, refs)
			if err != nil {
				return project.RemoteState{}, fmt.Errorf("connect published files before Push: %w", err)
			}
			for _, ref := range refs {
				if item, ok := resolved[ref.Key]; !ok || item.ID == "" {
					return project.RemoteState{}, fmt.Errorf("connect published files before Push: %q was not verified", ref.Name)
				}
			}
		}
	}
	state.Outputs, err = syncOutputs(ctx, drive, state.CurrentFolderID, outputFiles, state.Outputs, resolved, func(completed, total int, detail string) {
		percent := 55
		if total > 0 {
			percent += completed * 35 / total
		}
		report(PushProgress{Stage: "Uploading rendered files", Detail: detail, Completed: completed, Total: total, Percent: percent})
	})
	if err != nil {
		return project.RemoteState{}, err
	}
	version := VersionInfo{Message: message, CreatedAt: time.Now().UTC().Format(time.RFC3339), ParentVersion: remoteVersion}
	versionJSON, _ := json.MarshalIndent(version, "", "  ")
	outputsJSON, _ := json.MarshalIndent(struct {
		Files map[string]string `json:"files"`
	}{Files: state.Outputs}, "", "  ")
	var archive bytes.Buffer
	report(PushProgress{Stage: "Packaging project", Percent: 92})
	if err := bundle.PackWith(root, &archive, map[string][]byte{
		".stamp/version.json": append(versionJSON, '\n'),
		".stamp/outputs.json": append(outputsJSON, '\n'),
	}); err != nil {
		return project.RemoteState{}, err
	}
	diagnostic.Log("collab", "push.packed", "bytes", archive.Len(), "parent_version", remoteVersion)
	var updated stampdrive.Item
	report(PushProgress{Stage: "Publishing project version", Percent: 96})
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
	diagnostic.Log("collab", "push.complete", "version", state.BaseVersion, "files", len(hashes))
	report(PushProgress{Stage: "Push complete", Percent: 100})
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

type outputFile struct {
	Name string
	MIME string
	Data []byte
}

func collectOutputs(root string, results []render.Result) (map[string]outputFile, error) {
	files := make(map[string]outputFile, len(results))
	for _, result := range results {
		path := filepath.Join(root, filepath.FromSlash(result.Output))
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(filepath.Join(root, "outputs"), path)
		if err != nil {
			return nil, err
		}
		key := filepath.ToSlash(rel)
		files[key] = outputFile{Name: strings.ReplaceAll(key, "/", " - "), MIME: mimeFor(path), Data: data}
	}
	return files, nil
}

func outputRefs(current map[string]outputFile, ledger map[string]string) []stampdrive.FileRef {
	refs := make([]stampdrive.FileRef, 0, len(current)+len(ledger))
	seen := map[string]bool{}
	for key, file := range current {
		// An empty ledger means this project predates output identities. Every
		// existing mirror must be selected once; guessing would create duplicates.
		if len(ledger) == 0 || ledger[key] != "" {
			refs = append(refs, stampdrive.FileRef{Key: key, ID: ledger[key], Name: file.Name})
			seen[key] = true
		}
	}
	for key, id := range ledger {
		if !seen[key] {
			refs = append(refs, stampdrive.FileRef{Key: key, ID: id, Name: strings.ReplaceAll(key, "/", " - ")})
		}
	}
	return refs
}

func syncOutputs(ctx context.Context, drive Drive, folderID string, current map[string]outputFile, previous map[string]string, resolved map[string]stampdrive.Item, progress func(completed, total int, detail string)) (map[string]string, error) {
	diagnostic.Log("collab", "outputs.sync_start", "folder_id", folderID)
	if folderID == "" {
		return nil, errors.New("project has no Current folder")
	}
	next := make(map[string]string, len(current))
	total := len(current)
	for key := range previous {
		if _, ok := current[key]; !ok {
			total++
		}
	}
	completed := 0
	for key, file := range current {
		if progress != nil {
			progress(completed, total, key)
		}
		item, ok := resolved[key]
		if ok {
			diagnostic.Log("collab", "outputs.update", "path", key, "file_id", item.ID, "bytes", len(file.Data), "mime", file.MIME)
			updated, err := drive.UpdateFile(ctx, item.ID, file.MIME, bytes.NewReader(file.Data))
			if err != nil {
				return nil, err
			}
			if updated.ID == "" {
				updated.ID = item.ID
			}
			next[key] = updated.ID
		} else {
			diagnostic.Log("collab", "outputs.create", "path", key, "bytes", len(file.Data), "mime", file.MIME)
			created, err := drive.CreateFile(ctx, folderID, file.Name, file.MIME, bytes.NewReader(file.Data), map[string]string{"stamp_kind": "output", "stamp_path": key})
			if err != nil {
				return nil, err
			}
			next[key] = created.ID
		}
		completed++
	}
	for key := range previous {
		if _, ok := current[key]; ok {
			continue
		}
		item, ok := resolved[key]
		if !ok {
			return nil, fmt.Errorf("published file %q was not authorized for deletion", key)
		}
		diagnostic.Log("collab", "outputs.delete", "path", key, "file_id", item.ID)
		if err := drive.Trash(ctx, item.ID); err != nil {
			return nil, err
		}
		completed++
	}
	if progress != nil {
		progress(completed, total, "")
	}
	diagnostic.Log("collab", "outputs.sync_complete", "files", len(current))
	return next, nil
}

func readOutputLedger(contents []byte) (map[string]string, error) {
	reader, err := zip.NewReader(bytes.NewReader(contents), int64(len(contents)))
	if err != nil {
		return nil, fmt.Errorf("read output identities: %w", err)
	}
	for _, file := range reader.File {
		if file.Name != ".stamp/outputs.json" {
			continue
		}
		stream, err := file.Open()
		if err != nil {
			return nil, err
		}
		var ledger struct {
			Files map[string]string `json:"files"`
		}
		err = json.NewDecoder(io.LimitReader(stream, 1<<20)).Decode(&ledger)
		stream.Close()
		if err != nil {
			return nil, fmt.Errorf("read output identities: %w", err)
		}
		return ledger.Files, nil
	}
	return nil, nil
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
