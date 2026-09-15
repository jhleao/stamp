package collab

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/jhleao/stamp/internal/bundle"
	"github.com/jhleao/stamp/internal/notion"
	"github.com/jhleao/stamp/internal/project"
	"github.com/jhleao/stamp/internal/render"
)

func (r *Remote) openNotion(ctx context.Context, value, destination string) (Workspace, error) {
	id, err := notion.ID(value)
	if err != nil {
		return Workspace{}, err
	}
	snapshot, err := r.notion.Inspect(ctx, id)
	if err != nil {
		return Workspace{}, err
	}
	contents, err := r.notion.Contents(ctx, snapshot)
	if err != nil {
		return Workspace{}, err
	}
	if err = validateRemoteProject(contents); err != nil {
		return Workspace{}, err
	}
	if destination == "" {
		destination = safeName(snapshot.Name)
	}
	if entries, err := os.ReadDir(destination); err == nil && len(entries) > 0 {
		return Workspace{}, fmt.Errorf("%s is not empty", destination)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Workspace{}, err
	}
	if err = unpackNewWorkspace(contents, destination); err != nil {
		return Workspace{}, err
	}
	if err = project.EnsureAgentCompatibility(destination); err != nil {
		return Workspace{}, err
	}
	state, err := notionState(destination, snapshot, contents)
	if err != nil {
		return Workspace{}, err
	}
	if err = project.WriteState(destination, state); err != nil {
		return Workspace{}, err
	}
	root, err := filepath.Abs(destination)
	return Workspace{Root: root, State: state}, err
}
func notionState(root string, s notion.Snapshot, contents []byte) (project.RemoteState, error) {
	hashes, err := project.FileHashes(root)
	return project.RemoteState{Provider: "notion", FileID: s.PageID, ProjectFolderID: s.PageID, CurrentFolderID: s.CurrentID, WebURL: s.URL, BaseVersion: s.Lease, BaseHash: notion.Digest(contents), Files: hashes}, err
}
func (r *Remote) notionStateFor(ctx context.Context, root, value string) (project.RemoteState, error) {
	id, err := notion.ID(value)
	if err != nil {
		return project.RemoteState{}, err
	}
	s, err := r.notion.Inspect(ctx, id)
	if err != nil {
		return project.RemoteState{}, err
	}
	data, err := r.notion.Contents(ctx, s)
	if err != nil {
		return project.RemoteState{}, err
	}
	temp, err := unpackRemoteProject(data)
	if err != nil {
		return project.RemoteState{}, err
	}
	defer os.RemoveAll(temp)
	local, err := project.Load(root)
	if err != nil {
		return project.RemoteState{}, err
	}
	remote, err := project.Load(temp)
	if err != nil {
		return project.RemoteState{}, err
	}
	if local.ID != remote.ID {
		return project.RemoteState{}, errors.New("selected Notion project is not this workspace")
	}
	return notionState(temp, s, data)
}
func (r *Remote) createNotion(ctx context.Context, root, parent string) (project.RemoteState, error) {
	manifest, err := project.Load(root)
	if err != nil {
		return project.RemoteState{}, err
	}
	if parent != "" && parent != "root" {
		parent, err = notion.ID(parent)
		if err != nil {
			return project.RemoteState{}, err
		}
	}
	s, err := r.notion.Create(ctx, parent, manifest.Name)
	state := project.RemoteState{Provider: "notion", FileID: s.PageID, ProjectFolderID: s.PageID, CurrentFolderID: s.CurrentID, WebURL: s.URL}
	if err != nil {
		return state, err
	}
	// Persist the new subtree before rendering, so a failed first push can retry.
	if err = project.WriteState(root, state); err != nil {
		return state, err
	}
	return r.pushNotion(ctx, root, "Create project", "", nil)
}
func (r *Remote) pullNotion(ctx context.Context, root string, mode PullMode) (string, error) {
	state, err := project.ReadState(root)
	if err != nil {
		return "", err
	}
	s, err := r.notion.Inspect(ctx, state.FileID)
	if err != nil {
		return "", err
	}
	if s.Lease == state.BaseVersion {
		return "Already up to date.", nil
	}
	data, err := r.notion.Contents(ctx, s)
	if err != nil {
		return "", err
	}
	temporary, err := unpackRemoteProject(data)
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(temporary)
	localManifest, err := project.Load(root)
	if err != nil {
		return "", err
	}
	remoteManifest, err := project.Load(temporary)
	if err != nil {
		return "", err
	}
	if localManifest.ID != remoteManifest.ID {
		return "", errors.New("Notion archive belongs to a different project")
	}
	hashes, err := project.FileHashes(root)
	if err != nil {
		return "", err
	}
	dirty := !same(hashes, state.Files)
	if dirty && mode == PullSafe {
		return "", errors.New("Notion advanced while local files changed; use pull --incoming or pull --replace")
	}
	if dirty && mode == PullIncoming {
		destination := filepath.Join(root, ".stamp", "incoming", "version-"+s.Lease)
		if err = os.RemoveAll(destination); err != nil {
			return "", err
		}
		if err = bundle.UnpackReader(bytes.NewReader(data), int64(len(data)), destination); err != nil {
			return "", err
		}
		return "Remote version expanded at " + destination, nil
	}
	if dirty && mode == PullReplace {
		recovery := filepath.Join(root, ".stamp", "recovery", time.Now().UTC().Format("20060102T150405.000000000Z")+".stamp")
		if err = os.MkdirAll(filepath.Dir(recovery), 0755); err != nil {
			return "", err
		}
		if err = bundle.PackFile(root, recovery); err != nil {
			return "", err
		}
	}
	if err = replaceWorkspace(root, data); err != nil {
		return "", err
	}
	if err = project.EnsureAgentCompatibility(root); err != nil {
		return "", err
	}
	state, err = notionState(root, s, data)
	if err != nil {
		return "", err
	}
	if err = project.WriteState(root, state); err != nil {
		return "", err
	}
	return "Pulled Notion version " + s.Lease, nil
}
func (r *Remote) pushNotion(ctx context.Context, root, message, force string, progress PushProgressFunc) (project.RemoteState, error) {
	report := func(stage string, percent int) {
		if progress != nil {
			progress(PushProgress{Stage: stage, Percent: percent})
		}
	}
	state, err := project.ReadState(root)
	if err != nil {
		return state, err
	}
	s, err := r.notion.Inspect(ctx, state.FileID)
	if err != nil {
		return state, err
	}
	if (force != "" && force != s.Lease) || (state.BaseVersion != s.Lease && force != s.Lease) {
		return state, fmt.Errorf("push refused: Notion advanced to %s; pull first, or review and use --force-with-lease %s", s.Lease, s.Lease)
	}
	if len(s.Heads) > 1 && force != s.Lease {
		return state, fmt.Errorf("Notion has conflicting revisions; review and use --force-with-lease %s", s.Lease)
	}
	localManifest, err := project.Load(root)
	if err != nil {
		return state, err
	}
	for _, head := range s.Heads {
		data, err := r.notion.Download(ctx, head.BlockID)
		if err != nil {
			return state, err
		}
		if notion.Digest(data) != head.ID {
			return state, errors.New("Notion revision hash mismatch")
		}
		temporary, err := unpackRemoteProject(data)
		if err != nil {
			return state, err
		}
		remoteManifest, loadErr := project.Load(temporary)
		os.RemoveAll(temporary)
		if loadErr != nil {
			return state, loadErr
		}
		if remoteManifest.ID != localManifest.ID {
			return state, errors.New("Notion revision belongs to a different project")
		}
	}
	if err := r.notion.Preflight(ctx, s); err != nil {
		return state, err
	}
	report("Rendering documents", 5)
	results, err := render.All(root)
	if err != nil {
		return state, err
	}
	files, err := collectOutputs(root, results)
	if err != nil {
		return state, err
	}
	outputs := make([]notion.Output, 0, len(files))
	for key, file := range files {
		outputs = append(outputs, notion.Output{Path: key, MIME: file.MIME, Data: file.Data})
	}
	sort.Slice(outputs, func(i, j int) bool { return outputs[i].Path < outputs[j].Path })
	// Check every size before publishing the canonical archive.
	for _, file := range outputs {
		if int64(len(file.Data)) > r.notion.MaxUpload {
			return state, fmt.Errorf("%s exceeds Notion's file upload limit", file.Path)
		}
	}
	metadata, _ := json.Marshal(VersionInfo{Message: message, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), ParentVersion: s.Lease})
	var archive bytes.Buffer
	if err = bundle.PackWith(root, &archive, map[string][]byte{".stamp/version.json": metadata}); err != nil {
		return state, err
	}
	report("Retaining project revision", 55)
	revision, err := r.notion.Commit(ctx, s, archive.Bytes())
	if err != nil {
		return state, err
	}
	report("Publishing rendered files", 70)
	if err = r.notion.Publish(ctx, s, outputs); err != nil {
		return state, fmt.Errorf("archive revision %s was retained, but rendered pages need repair; review the remote and retry with --force-with-lease %s: %w", revision.ID, revision.ID, err)
	}
	latest, err := r.notion.Inspect(ctx, s.PageID)
	if err != nil {
		return state, err
	}
	if latest.Lease != revision.ID {
		return state, fmt.Errorf("Notion changed during rendering publication; archive retained, review revisions and lease %s", latest.Lease)
	}
	// Derive the saved baseline from the archive, so an edit made while upload
	// was running remains dirty rather than being marked as already published.
	temporary, err := unpackRemoteProject(archive.Bytes())
	if err != nil {
		return state, err
	}
	defer os.RemoveAll(temporary)
	state, err = notionState(temporary, latest, archive.Bytes())
	if err != nil {
		return state, err
	}
	if err = project.WriteState(root, state); err != nil {
		return state, err
	}
	report("Push complete", 100)
	return state, nil
}

// CreateNotionCopy publishes from staging before changing the workspace's
// connection. A failed migration leaves its original remote and files intact.
func (r *Remote) CreateNotionCopy(ctx context.Context, root, parent string) (project.RemoteState, error) {
	if r.notion == nil {
		return project.RemoteState{}, errors.New("remote create currently requires --backend notion")
	}
	var archive bytes.Buffer
	if err := bundle.PackSource(root, &archive); err != nil {
		return project.RemoteState{}, err
	}
	staging, err := unpackRemoteProject(archive.Bytes())
	if err != nil {
		return project.RemoteState{}, err
	}
	defer os.RemoveAll(staging)
	state, err := r.createNotion(ctx, staging, parent)
	if err != nil {
		if state.WebURL == "" {
			return state, fmt.Errorf("Notion copy did not complete; original connection preserved: %w", err)
		}
		return state, fmt.Errorf("Notion copy did not complete; original connection preserved (new page: %s): %w", state.WebURL, err)
	}
	if err := project.WriteState(root, state); err != nil {
		return state, fmt.Errorf("Notion copy created at %s, but the local connection could not be saved: %w", state.WebURL, err)
	}
	return state, nil
}
