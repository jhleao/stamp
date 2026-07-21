package agent

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/weve-ai/stamp/internal/collab"
	"github.com/weve-ai/stamp/internal/doctor"
	stampdrive "github.com/weve-ai/stamp/internal/drive"
	"github.com/weve-ai/stamp/internal/project"
	"github.com/weve-ai/stamp/internal/render"
)

type noInput struct{}
type dirInput struct {
	Dir string `json:"dir,omitempty" jsonschema:"project directory; defaults to the current directory"`
}
type createInput struct {
	Dir  string `json:"dir" jsonschema:"new project directory"`
	Name string `json:"name,omitempty" jsonschema:"project name"`
}
type openInput struct {
	Project string `json:"project" jsonschema:"Google Drive project URL or file ID"`
	Dir     string `json:"dir,omitempty" jsonschema:"destination directory"`
}
type pullInput struct {
	Dir  string `json:"dir,omitempty"`
	Mode string `json:"mode,omitempty" jsonschema:"safe, incoming, or replace"`
}
type pushInput struct {
	Dir            string `json:"dir,omitempty"`
	Space          string `json:"space,omitempty" jsonschema:"Drive Space URL or ID; required only for the first push"`
	Message        string `json:"message,omitempty"`
	ForceWithLease string `json:"forceWithLease,omitempty" jsonschema:"exact remote version shown by a refused push"`
}

func New(version string) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "stamp", Version: version}, nil)
	mcp.AddTool(s, &mcp.Tool{Name: "spaces_list", Description: "List Stamp Spaces in Google Drive."}, spacesList)
	mcp.AddTool(s, &mcp.Tool{Name: "projects_list", Description: "List Stamp projects in Google Drive."}, projectsList)
	mcp.AddTool(s, &mcp.Tool{Name: "project_create", Description: "Create a small local Stamp project."}, projectCreate)
	mcp.AddTool(s, &mcp.Tool{Name: "project_open", Description: "Open a Stamp project from Google Drive."}, projectOpen)
	mcp.AddTool(s, &mcp.Tool{Name: "project_status", Description: "Show local files, changes, and the Drive lease."}, projectStatus)
	mcp.AddTool(s, &mcp.Tool{Name: "project_preview", Description: "Render all documents, decks, and spreadsheets."}, projectPreview)
	mcp.AddTool(s, &mcp.Tool{Name: "project_pull", Description: "Pull from Drive with conflict detection."}, projectPull)
	mcp.AddTool(s, &mcp.Tool{Name: "project_push", Description: "Render and push one immutable Drive version."}, projectPush)
	mcp.AddTool(s, &mcp.Tool{Name: "project_drive_link", Description: "Return the project's Google Drive link."}, projectDriveLink)
	mcp.AddTool(s, &mcp.Tool{Name: "doctor", Description: "Check Stamp's local rendering dependencies."}, doctorTool)
	return s
}

func Run(ctx context.Context, version string) error {
	return New(version).Run(ctx, &mcp.StdioTransport{})
}

func spacesList(ctx context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, any, error) {
	d, err := stampdrive.New(ctx)
	if err != nil {
		return nil, nil, err
	}
	items, err := d.Spaces(ctx)
	return nil, items, err
}

func projectsList(ctx context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, any, error) {
	d, err := stampdrive.New(ctx)
	if err != nil {
		return nil, nil, err
	}
	items, err := d.Projects(ctx)
	return nil, items, err
}

func projectCreate(_ context.Context, _ *mcp.CallToolRequest, in createInput) (*mcp.CallToolResult, any, error) {
	if in.Dir == "" {
		return nil, nil, fmt.Errorf("dir is required")
	}
	dir, err := filepath.Abs(in.Dir)
	if err != nil {
		return nil, nil, err
	}
	manifest, err := project.Create(dir, in.Name)
	return nil, map[string]any{"project": manifest, "dir": dir}, err
}

func projectOpen(ctx context.Context, _ *mcp.CallToolRequest, in openInput) (*mcp.CallToolResult, any, error) {
	if in.Project == "" {
		return nil, nil, fmt.Errorf("project is required")
	}
	d, err := stampdrive.New(ctx)
	if err != nil {
		return nil, nil, err
	}
	state, err := collab.Open(ctx, d, in.Project, in.Dir)
	return nil, state, err
}

func projectStatus(_ context.Context, _ *mcp.CallToolRequest, in dirInput) (*mcp.CallToolResult, any, error) {
	root, err := root(in.Dir)
	if err != nil {
		return nil, nil, err
	}
	status, err := project.Status(root)
	return nil, status, err
}

func projectPreview(_ context.Context, _ *mcp.CallToolRequest, in dirInput) (*mcp.CallToolResult, any, error) {
	root, err := root(in.Dir)
	if err != nil {
		return nil, nil, err
	}
	results, err := render.All(root)
	return nil, map[string]any{"outputs": results}, err
}

func projectPull(ctx context.Context, _ *mcp.CallToolRequest, in pullInput) (*mcp.CallToolResult, any, error) {
	root, err := root(in.Dir)
	if err != nil {
		return nil, nil, err
	}
	mode := collab.PullMode(in.Mode)
	if mode == "" {
		mode = collab.PullSafe
	}
	if mode != collab.PullSafe && mode != collab.PullIncoming && mode != collab.PullReplace {
		return nil, nil, fmt.Errorf("mode must be safe, incoming, or replace")
	}
	d, err := stampdrive.New(ctx)
	if err != nil {
		return nil, nil, err
	}
	message, err := collab.Pull(ctx, d, root, mode)
	return nil, map[string]string{"message": message}, err
}

func projectPush(ctx context.Context, _ *mcp.CallToolRequest, in pushInput) (*mcp.CallToolResult, any, error) {
	root, err := root(in.Dir)
	if err != nil {
		return nil, nil, err
	}
	d, err := stampdrive.New(ctx)
	if err != nil {
		return nil, nil, err
	}
	state, err := collab.Push(ctx, d, root, in.Space, in.Message, in.ForceWithLease)
	return nil, state, err
}

func projectDriveLink(_ context.Context, _ *mcp.CallToolRequest, in dirInput) (*mcp.CallToolResult, any, error) {
	root, err := root(in.Dir)
	if err != nil {
		return nil, nil, err
	}
	state, err := project.ReadState(root)
	if err != nil {
		return nil, nil, err
	}
	if state.WebURL == "" {
		return nil, nil, fmt.Errorf("project has not been pushed")
	}
	return nil, map[string]string{"url": state.WebURL, "version": state.BaseVersion}, nil
}

func doctorTool(_ context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, any, error) {
	return nil, map[string]any{"checks": doctor.Run()}, nil
}

func root(dir string) (string, error) {
	if dir == "" {
		dir = "."
	}
	return project.FindRoot(dir)
}
