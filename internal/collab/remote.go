package collab

import (
	"context"
	"errors"
	"fmt"

	stampdrive "github.com/jhleao/stamp/internal/drive"
	"github.com/jhleao/stamp/internal/notion"
	"github.com/jhleao/stamp/internal/project"
)

// Remote exposes project operations; provider-specific object and permission
// models stay behind this boundary rather than imitating a Drive filesystem.
type Remote struct {
	drive  Drive
	notion *notion.Client
}

func Connect(ctx context.Context, provider string) (*Remote, error) {
	switch provider {
	case "", "drive":
		d, err := stampdrive.New(ctx)
		return &Remote{drive: d}, err
	case "notion":
		n, err := notion.New(ctx)
		return &Remote{notion: n}, err
	default:
		return nil, fmt.Errorf("unknown remote provider %q", provider)
	}
}
func ConnectWorkspace(ctx context.Context, root string) (*Remote, error) {
	s, err := project.ReadState(root)
	if err != nil {
		return nil, err
	}
	return Connect(ctx, s.Provider)
}
func (r *Remote) Open(ctx context.Context, id, destination string) (Workspace, error) {
	if r.notion != nil {
		return r.openNotion(ctx, id, destination)
	}
	return Open(ctx, r.drive, id, destination)
}
func (r *Remote) Create(ctx context.Context, root, parent string) (project.RemoteState, error) {
	if r.notion != nil {
		return r.createNotion(ctx, root, parent)
	}
	return Create(ctx, r.drive, root, parent)
}
func (r *Remote) Pull(ctx context.Context, root string, mode PullMode) (string, error) {
	if mode != PullSafe && mode != PullIncoming && mode != PullReplace {
		return "", errors.New("invalid pull mode")
	}
	if r.notion != nil {
		return r.pullNotion(ctx, root, mode)
	}
	return Pull(ctx, r.drive, root, mode)
}
func (r *Remote) Push(ctx context.Context, root, message, force string, progress PushProgressFunc) (project.RemoteState, error) {
	if r.notion != nil {
		return r.pushNotion(ctx, root, message, force, progress)
	}
	return PushWithProgress(ctx, r.drive, root, message, force, progress)
}
func (r *Remote) StateFor(ctx context.Context, root, id string) (project.RemoteState, error) {
	if r.notion != nil {
		return r.notionStateFor(ctx, root, id)
	}
	return RemoteStateFor(ctx, r.drive, root, id)
}
func (r *Remote) Version(ctx context.Context, id string) (string, error) {
	if r.notion != nil {
		s, err := r.notion.Inspect(ctx, id)
		if err == nil && len(s.Heads) > 1 {
			return s.Lease, fmt.Errorf("Notion has concurrent revisions; review Version history and resolve locally with stamp push --force-with-lease %s", s.Lease)
		}
		return s.Lease, err
	}
	item, err := r.drive.Get(ctx, id)
	return item.Version, err
}
func (r *Remote) Download(ctx context.Context, id string) ([]byte, error) {
	if r.notion != nil {
		s, err := r.notion.Inspect(ctx, id)
		if err != nil {
			return nil, err
		}
		return r.notion.Contents(ctx, s)
	}
	return r.drive.Download(ctx, id)
}
