package agent

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jhleao/stamp/internal/collab"
	"github.com/jhleao/stamp/internal/doctor"
	stampdrive "github.com/jhleao/stamp/internal/drive"
	"github.com/jhleao/stamp/internal/notion"
	"github.com/jhleao/stamp/internal/notioncollab"
	"github.com/jhleao/stamp/internal/project"
	"github.com/jhleao/stamp/internal/render"
	"github.com/jhleao/stamp/internal/themepreview"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type noInput struct{}
type dirInput struct {
	Dir string `json:"dir,omitempty" jsonschema:"project directory; defaults to the current directory"`
}
type createInput struct {
	Dir      string `json:"dir" jsonschema:"new project directory"`
	Name     string `json:"name,omitempty" jsonschema:"project name"`
	Template string `json:"template,omitempty" jsonschema:"optional local theme directory to snapshot"`
}
type templateCreateInput struct {
	Dir string `json:"dir" jsonschema:"new theme directory"`
}
type openInput struct {
	Project string `json:"project" jsonschema:"Google Drive or Notion project URL or ID"`
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

const (
	StudioAddress  = "127.0.0.1:57183"
	StudioEndpoint = "http://" + StudioAddress + "/mcp"
)

func New(version string) *mcp.Server {
	return newServer(version, "")
}

func NewForProject(version, root string) *mcp.Server {
	return newServer(version, root)
}

func HTTPHandler(version, root string) http.Handler {
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return NewForProject(version, root)
	}, nil)
}

func newServer(version, defaultDir string) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "stamp", Version: version}, nil)
	mcp.AddTool(s, &mcp.Tool{Name: "spaces_list", Description: "List Stamp Spaces in Google Drive."}, spacesList)
	mcp.AddTool(s, &mcp.Tool{Name: "projects_list", Description: "List Stamp projects in Google Drive."}, projectsList)
	mcp.AddTool(s, &mcp.Tool{Name: "template_create", Description: "Create a local starter theme with examples and reusable components."}, templateCreate)
	mcp.AddTool(s, &mcp.Tool{Name: "template_preview", Description: "Render every visual example in a standalone theme."}, templatePreview)
	mcp.AddTool(s, &mcp.Tool{Name: "project_create", Description: "Create a small local Stamp project."}, projectCreate)
	mcp.AddTool(s, &mcp.Tool{Name: "project_open", Description: "Open a Stamp project from Google Drive or Notion."}, projectOpen)
	mcp.AddTool(s, &mcp.Tool{Name: "project_status", Description: "Show local files, changes, and the Drive lease."}, func(ctx context.Context, req *mcp.CallToolRequest, in dirInput) (*mcp.CallToolResult, any, error) {
		return projectStatus(ctx, req, in, defaultDir)
	})
	mcp.AddTool(s, &mcp.Tool{Name: "project_preview", Description: "Render all documents, decks, and spreadsheets."}, func(ctx context.Context, req *mcp.CallToolRequest, in dirInput) (*mcp.CallToolResult, any, error) {
		return projectPreview(ctx, req, in, defaultDir)
	})
	mcp.AddTool(s, &mcp.Tool{Name: "project_pull", Description: "Pull from the connected backend with conflict detection."}, func(ctx context.Context, req *mcp.CallToolRequest, in pullInput) (*mcp.CallToolResult, any, error) {
		return projectPull(ctx, req, in, defaultDir)
	})
	mcp.AddTool(s, &mcp.Tool{Name: "project_push", Description: "Render and push one leased backend revision."}, func(ctx context.Context, req *mcp.CallToolRequest, in pushInput) (*mcp.CallToolResult, any, error) {
		return projectPush(ctx, req, in, defaultDir)
	})
	mcp.AddTool(s, &mcp.Tool{Name: "project_drive_link", Description: "Return the connected project's backend link."}, func(ctx context.Context, req *mcp.CallToolRequest, in dirInput) (*mcp.CallToolResult, any, error) {
		return projectDriveLink(ctx, req, in, defaultDir)
	})
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
	manifest, err := project.CreateWithTheme(dir, in.Name, in.Template)
	return nil, map[string]any{"project": manifest, "dir": dir}, err
}

func templateCreate(_ context.Context, _ *mcp.CallToolRequest, in templateCreateInput) (*mcp.CallToolResult, any, error) {
	if in.Dir == "" {
		return nil, nil, fmt.Errorf("dir is required")
	}
	dir, err := filepath.Abs(in.Dir)
	if err != nil {
		return nil, nil, err
	}
	if entries, readErr := os.ReadDir(dir); readErr == nil && len(entries) > 0 {
		return nil, nil, fmt.Errorf("%s is not empty", dir)
	} else if readErr != nil && !os.IsNotExist(readErr) {
		return nil, nil, readErr
	}
	if err := project.WriteStarterTheme(dir); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"dir": dir, "next": "edit examples and components, call template_preview, then create a project using this theme"}, nil
}

func templatePreview(_ context.Context, _ *mcp.CallToolRequest, in templateCreateInput) (*mcp.CallToolResult, any, error) {
	if in.Dir == "" {
		return nil, nil, fmt.Errorf("dir is required")
	}
	results, err := themepreview.All(in.Dir)
	return nil, map[string]any{"outputs": results}, err
}

func projectOpen(ctx context.Context, _ *mcp.CallToolRequest, in openInput) (*mcp.CallToolResult, any, error) {
	if in.Project == "" {
		return nil, nil, fmt.Errorf("project is required")
	}
	if strings.Contains(in.Project, "notion") {
		client, err := notion.New()
		if err != nil {
			return nil, nil, err
		}
		state, err := notioncollab.Open(ctx, client, in.Project, in.Dir)
		return nil, state, err
	}
	d, err := stampdrive.New(ctx)
	if err != nil {
		return nil, nil, err
	}
	state, err := collab.Open(ctx, d, in.Project, in.Dir)
	return nil, state, err
}

func projectStatus(ctx context.Context, _ *mcp.CallToolRequest, in dirInput, defaultDir string) (*mcp.CallToolResult, any, error) {
	root, err := root(in.Dir, defaultDir)
	if err != nil {
		return nil, nil, err
	}
	if state, _ := notioncollab.ReadState(root); state.PageID != "" {
		client, err := notion.New()
		if err != nil {
			return nil, nil, err
		}
		status, err := notioncollab.StatusOf(ctx, client, root)
		return nil, status, err
	}
	status, err := project.Status(root)
	return nil, status, err
}

func projectPreview(_ context.Context, _ *mcp.CallToolRequest, in dirInput, defaultDir string) (*mcp.CallToolResult, any, error) {
	root, err := root(in.Dir, defaultDir)
	if err != nil {
		return nil, nil, err
	}
	results, err := render.All(root)
	return nil, map[string]any{"outputs": results}, err
}

func projectPull(ctx context.Context, _ *mcp.CallToolRequest, in pullInput, defaultDir string) (*mcp.CallToolResult, any, error) {
	root, err := root(in.Dir, defaultDir)
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
	if notionState, _ := notioncollab.ReadState(root); notionState.PageID != "" {
		client, err := notion.New()
		if err != nil {
			return nil, nil, err
		}
		state, err := notioncollab.Pull(ctx, client, root, mode == collab.PullReplace)
		return nil, map[string]any{"message": fmt.Sprintf("Pulled Notion revision %d", state.Revision), "state": state}, err
	}
	d, err := stampdrive.New(ctx)
	if err != nil {
		return nil, nil, err
	}
	message, err := collab.Pull(ctx, d, root, mode)
	return nil, map[string]string{"message": message}, err
}

func projectPush(ctx context.Context, _ *mcp.CallToolRequest, in pushInput, defaultDir string) (*mcp.CallToolResult, any, error) {
	root, err := root(in.Dir, defaultDir)
	if err != nil {
		return nil, nil, err
	}
	if notionState, _ := notioncollab.ReadState(root); notionState.PageID != "" {
		client, err := notion.New()
		if err != nil {
			return nil, nil, err
		}
		state, err := notioncollab.Push(ctx, client, root, in.Message, in.ForceWithLease)
		return nil, state, err
	}
	d, err := stampdrive.New(ctx)
	if err != nil {
		return nil, nil, err
	}
	state, err := collab.Push(ctx, d, root, in.Space, in.Message, in.ForceWithLease)
	return nil, state, err
}

func projectDriveLink(_ context.Context, _ *mcp.CallToolRequest, in dirInput, defaultDir string) (*mcp.CallToolResult, any, error) {
	root, err := root(in.Dir, defaultDir)
	if err != nil {
		return nil, nil, err
	}
	if state, _ := notioncollab.ReadState(root); state.PageID != "" {
		return nil, map[string]any{"url": state.URL, "version": state.Revision, "provider": "notion"}, nil
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

func root(dir, defaultDir string) (string, error) {
	if dir == "" {
		dir = defaultDir
		if dir == "" {
			dir = "."
		}
	}
	return project.FindRoot(dir)
}
