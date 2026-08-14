package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/term"

	"github.com/weve-ai/stamp/internal/agent"
	"github.com/weve-ai/stamp/internal/agentsetup"
	"github.com/weve-ai/stamp/internal/bundle"
	"github.com/weve-ai/stamp/internal/collab"
	"github.com/weve-ai/stamp/internal/doctor"
	stampdrive "github.com/weve-ai/stamp/internal/drive"
	"github.com/weve-ai/stamp/internal/notion"
	"github.com/weve-ai/stamp/internal/notioncollab"
	"github.com/weve-ai/stamp/internal/project"
	"github.com/weve-ai/stamp/internal/render"
	"github.com/weve-ai/stamp/internal/studio"
	"github.com/weve-ai/stamp/internal/themepreview"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "stamp:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}
	switch args[0] {
	case "login":
		message, err := stampdrive.Login(context.Background())
		if err == nil {
			fmt.Println(message)
		}
		return err
	case "logout":
		return stampdrive.Logout()
	case "google-oauth":
		return googleOAuthCommand(args[1:])
	case "notion":
		return notionCommand(args[1:])
	case "space":
		return spaceCommand(args[1:])
	case "version", "--version", "-v":
		fmt.Println(version)
		return nil
	case "project":
		return projectCommand(args[1:])
	case "template":
		return templateCommand(args[1:])
	case "preview":
		return previewCommand(args[1:])
	case "pull":
		return pullCommand(args[1:])
	case "push":
		return pushCommand(args[1:])
	case "studio":
		return studioCommand(args[1:])
	case "mcp":
		if len(args) != 2 || args[1] != "serve" {
			return errors.New("usage: stamp mcp serve")
		}
		return agent.Run(context.Background(), version)
	case "agent":
		return agentCommand(args[1:])
	case "doctor":
		return doctorCommand(args[1:])
	case "status":
		return statusCommand(args[1:])
	case "pack":
		return packCommand(args[1:])
	case "unpack":
		return unpackCommand(args[1:])
	case "help", "--help", "-h":
		usage()
		return nil
	default:
		return fmt.Errorf("unknown command %q (try stamp help)", args[0])
	}
}

func notionCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: stamp notion <login|logout|status|pages|project>")
	}
	ctx := context.Background()
	switch args[0] {
	case "login":
		if len(args) != 1 {
			return errors.New("usage: stamp notion login")
		}
		fmt.Fprint(os.Stderr, "Notion integration token: ")
		token, readErr := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if readErr != nil {
			return readErr
		}
		if err := notion.SaveToken(string(token)); err != nil {
			return err
		}
		client, err := notion.New()
		if err != nil {
			return err
		}
		me, err := client.Me(ctx)
		if err != nil {
			_ = notion.Logout()
			return err
		}
		fmt.Println("Connected Notion integration", defaultString(anyString(me["name"]), "unknown"))
		return nil
	case "logout":
		if len(args) != 1 {
			return errors.New("usage: stamp notion logout")
		}
		return notion.Logout()
	case "status":
		client, err := notion.New()
		if err != nil {
			return err
		}
		me, err := client.Me(ctx)
		if err != nil {
			return err
		}
		fmt.Println("Notion connected")
		fmt.Println("  integration:", defaultString(anyString(me["name"]), "unknown"))
		return nil
	case "pages":
		client, err := notion.New()
		if err != nil {
			return err
		}
		pages, err := client.SearchPages(ctx)
		if err != nil {
			return err
		}
		for _, page := range pages {
			fmt.Printf("%s\t%s\n", page.ID, page.URL)
		}
		return nil
	case "project":
		return notionProjectCommand(ctx, args[1:])
	default:
		return fmt.Errorf("unknown notion command %q", args[0])
	}
}

func notionProjectCommand(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: stamp notion project <create|open|pull|push|status>")
	}
	client, err := notion.New()
	if err != nil {
		return err
	}
	switch args[0] {
	case "create":
		pos, opts, _, err := parseArgs(args[1:], []string{"parent", "dir", "name"}, nil)
		if err != nil {
			return err
		}
		if len(pos) != 0 {
			return errors.New("usage: stamp notion project create --parent <page-url-or-id> [--dir <directory>] [--name <name>]")
		}
		root, err := project.FindRoot(defaultString(opts["dir"], "."))
		if err != nil {
			return err
		}
		state, err := notioncollab.Create(ctx, client, opts["parent"], root, opts["name"])
		if err != nil {
			return err
		}
		fmt.Printf("Created Notion project revision %d\n%s\n", state.Revision, state.URL)
		return nil
	case "open":
		pos, opts, _, err := parseArgs(args[1:], []string{"dir"}, nil)
		if err != nil {
			return err
		}
		if len(pos) != 1 {
			return errors.New("usage: stamp notion project open <page-url-or-id> [--dir <directory>]")
		}
		state, err := notioncollab.Open(ctx, client, pos[0], opts["dir"])
		if err != nil {
			return err
		}
		fmt.Printf("Opened Notion revision %d\n%s\n", state.Revision, state.URL)
		return nil
	case "pull":
		pos, opts, flags, err := parseArgs(args[1:], []string{"dir"}, []string{"replace"})
		if err != nil {
			return err
		}
		if len(pos) != 0 {
			return errors.New("usage: stamp notion project pull [--dir <directory>] [--replace]")
		}
		root, err := project.FindRoot(defaultString(opts["dir"], "."))
		if err != nil {
			return err
		}
		state, err := notioncollab.Pull(ctx, client, root, flags["replace"])
		if err != nil {
			return err
		}
		fmt.Printf("Pulled Notion revision %d\n", state.Revision)
		return nil
	case "push":
		pos, opts, _, err := parseArgs(args[1:], []string{"dir", "message", "force-with-lease"}, nil)
		if err != nil {
			return err
		}
		if len(pos) != 0 {
			return errors.New("usage: stamp notion project push [--dir <directory>] [--message <text>] [--force-with-lease <revision>]")
		}
		root, err := project.FindRoot(defaultString(opts["dir"], "."))
		if err != nil {
			return err
		}
		state, err := notioncollab.Push(ctx, client, root, opts["message"], opts["force-with-lease"])
		if err != nil {
			return err
		}
		fmt.Printf("Pushed Notion revision %d\n%s\n", state.Revision, state.URL)
		return nil
	case "status":
		pos, opts, _, err := parseArgs(args[1:], []string{"dir"}, nil)
		if err != nil {
			return err
		}
		if len(pos) != 0 {
			return errors.New("usage: stamp notion project status [--dir <directory>]")
		}
		root, err := project.FindRoot(defaultString(opts["dir"], "."))
		if err != nil {
			return err
		}
		status, err := notioncollab.StatusOf(ctx, client, root)
		if err != nil {
			return err
		}
		fmt.Printf("Notion revision %d\n  local changes: %v\n  remote changes: %v\n%s\n", status.State.Revision, status.LocalChanged, status.RemoteChanged, status.State.URL)
		return nil
	default:
		return fmt.Errorf("unknown notion project command %q", args[0])
	}
}

func anyString(value any) string { result, _ := value.(string); return result }

func googleOAuthCommand(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: stamp google-oauth <desktop-client.json>|status|reset")
	}
	switch args[0] {
	case "status":
		info, err := stampdrive.Credentials()
		if err != nil {
			return err
		}
		fmt.Println("Google OAuth")
		fmt.Println("  source:", info.Source)
		fmt.Println("  location:", info.Path)
		fmt.Println("  client:", info.ClientID)
		fmt.Println("  scope:", info.Scope)
		return nil
	case "reset":
		path, err := stampdrive.ResetConfig()
		if err != nil {
			return err
		}
		fmt.Println("Removed organization OAuth override at", path)
		if os.Getenv("STAMP_GOOGLE_OAUTH_CONFIG") != "" {
			fmt.Println("The STAMP_GOOGLE_OAUTH_CONFIG environment override is still active.")
		} else {
			fmt.Println("Stamp will use its bundled Google OAuth client. Run stamp login to connect it.")
		}
		return nil
	default:
		destination, err := stampdrive.InstallConfig(args[0])
		if err == nil {
			fmt.Println("Installed Google OAuth override at", destination)
			fmt.Println("Run stamp login to connect this OAuth client.")
		}
		return err
	}
}

func agentCommand(args []string) error {
	pos, opts, _, err := parseArgs(args, []string{"dir"}, nil)
	if err != nil {
		return err
	}
	if len(pos) != 1 || pos[0] != "setup" {
		return errors.New("usage: stamp agent setup [--dir <directory>]")
	}
	result, err := agentsetup.Setup(defaultString(opts["dir"], "."))
	if err != nil {
		return err
	}
	fmt.Println("Agent setup ready")
	fmt.Println("  instructions:", result.ClaudeFile, "-> AGENTS.md")
	fmt.Println("  Claude MCP:", result.MCPFile)
	fmt.Println("  endpoint:", result.Endpoint)
	fmt.Println("Run stamp studio in this workspace before opening Claude Code.")
	return nil
}

func projectCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: stamp project <create|list|open|rename|reconnect>")
	}
	switch args[0] {
	case "list":
		drive, err := stampdrive.New(context.Background())
		if err != nil {
			return err
		}
		items, err := drive.Projects(context.Background())
		if err != nil {
			return err
		}
		for _, item := range items {
			fmt.Printf("%s\t%s\t%s\n", item.Name, item.ID, item.WebURL)
		}
		return nil
	case "open":
		pos, opts, _, err := parseArgs(args[1:], []string{"dir"}, nil)
		if err != nil {
			return err
		}
		if len(pos) != 1 {
			return errors.New("usage: stamp project open <drive-url-or-id> [--dir <directory>]")
		}
		drive, err := stampdrive.New(context.Background())
		if err != nil {
			return err
		}
		state, err := collab.Open(context.Background(), drive, pos[0], opts["dir"])
		if err != nil {
			return err
		}
		fmt.Printf("Opened Drive version %s\n", state.BaseVersion)
		return nil
	case "create":
		return createProject(args[1:])
	case "rename":
		pos, opts, _, err := parseArgs(args[1:], []string{"dir"}, nil)
		if err != nil {
			return err
		}
		if len(pos) != 1 {
			return errors.New("usage: stamp project rename <name> [--dir <directory>]")
		}
		root, err := project.FindRoot(defaultString(opts["dir"], "."))
		if err != nil {
			return err
		}
		state, err := project.ReadState(root)
		if err != nil {
			return err
		}
		if state.ProjectFolderID == "" || state.FileID == "" {
			return errors.New("project has not been pushed")
		}
		drive, err := stampdrive.New(context.Background())
		if err != nil {
			return err
		}
		if _, err := drive.Rename(context.Background(), state.ProjectFolderID, pos[0]); err != nil {
			return err
		}
		if err := project.Rename(root, pos[0]); err != nil {
			return err
		}
		state, err = collab.Push(context.Background(), drive, root, "", "rename project to "+pos[0], "")
		if err != nil {
			return err
		}
		fmt.Printf("Renamed project to %s at Drive version %s\n", pos[0], state.BaseVersion)
		return nil
	case "reconnect":
		pos, opts, _, err := parseArgs(args[1:], []string{"dir", "space", "message"}, nil)
		if err != nil {
			return err
		}
		if len(pos) != 0 || opts["space"] == "" {
			return errors.New("usage: stamp project reconnect --space <id-or-url> [--dir <directory>] [--message <text>]")
		}
		root, err := project.FindRoot(defaultString(opts["dir"], "."))
		if err != nil {
			return err
		}
		drive, err := stampdrive.New(context.Background())
		if err != nil {
			return err
		}
		message := defaultString(opts["message"], "Reconnect project to Stamp Drive access")
		state, recovery, err := collab.Reconnect(context.Background(), drive, root, opts["space"], message)
		if err != nil {
			return err
		}
		fmt.Println("Previous Drive project left untouched; old link saved at", recovery)
		fmt.Printf("Connected Drive version %s\n%s\n", state.BaseVersion, state.WebURL)
		return nil
	default:
		return fmt.Errorf("unknown project command %q", args[0])
	}
}

func createProject(args []string) error {
	pos, opts, _, err := parseArgs(args, []string{"name", "template", "space"}, nil)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("usage: stamp project create <directory> [--name <name>] [--template <theme-directory>] [--space <id-or-url>]")
	}
	dir, err := filepath.Abs(pos[0])
	if err != nil {
		return err
	}
	p, err := project.CreateWithTheme(dir, opts["name"], opts["template"])
	if err != nil {
		return err
	}
	fmt.Printf("Created %s at %s\n", p.Name, dir)
	if _, err := agentsetup.Setup(dir); err != nil {
		return fmt.Errorf("prepare agent integration: %w", err)
	}
	if opts["space"] != "" {
		drive, err := stampdrive.New(context.Background())
		if err != nil {
			return fmt.Errorf("project was created locally but could not connect to Drive: %w", err)
		}
		state, err := collab.Push(context.Background(), drive, dir, opts["space"], "Create project", "")
		if err != nil {
			return fmt.Errorf("project was created locally but could not connect to Drive: %w", err)
		}
		fmt.Printf("Connected Drive version %s\n%s\n", state.BaseVersion, state.WebURL)
		fmt.Println("Ready for stamp studio --dir", dir)
	} else {
		fmt.Println("Local project only. Connect it with stamp push --dir", dir, "--space <space-id-or-url> before opening Studio.")
	}
	return nil
}

func templateCommand(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: stamp template <create|preview> <directory>")
	}
	if args[0] == "preview" {
		results, err := themepreview.All(args[1])
		for _, result := range results {
			fmt.Printf("%s -> %s\n", result.Source, result.Output)
		}
		return err
	}
	if args[0] != "create" {
		return fmt.Errorf("unknown template command %q", args[0])
	}
	dir, err := filepath.Abs(args[1])
	if err != nil {
		return err
	}
	if entries, readErr := os.ReadDir(dir); readErr == nil && len(entries) > 0 {
		return fmt.Errorf("%s is not empty", dir)
	} else if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	if err := project.WriteStarterTheme(dir); err != nil {
		return err
	}
	fmt.Printf("Created theme at %s\n", dir)
	fmt.Println("Edit examples, components, and tailwind.css; verify with stamp template preview", dir)
	return nil
}

func spaceCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: stamp space <list|create|pick|init|rename>")
	}
	drive, err := stampdrive.New(context.Background())
	if err != nil {
		return err
	}
	switch args[0] {
	case "create":
		if len(args) != 2 {
			return errors.New("usage: stamp space create <name>")
		}
		item, err := drive.CreateSpace(context.Background(), args[1])
		if err != nil {
			return err
		}
		fmt.Printf("Created %s\n%s\n", item.Name, item.WebURL)
		return nil
	case "rename":
		if len(args) != 3 {
			return errors.New("usage: stamp space rename <drive-url-or-id> <name>")
		}
		item, err := drive.Rename(context.Background(), stampdrive.ID(args[1]), args[2])
		if err != nil {
			return err
		}
		fmt.Printf("Renamed Space to %s\n%s\n", item.Name, item.WebURL)
		return nil
	case "list":
		items, err := drive.Spaces(context.Background())
		if err != nil {
			return err
		}
		for _, item := range items {
			fmt.Printf("%s\t%s\t%s\n", item.Name, item.ID, item.WebURL)
		}
		return nil
	case "init":
		pos, opts, _, err := parseArgs(args[1:], []string{"name"}, nil)
		if err != nil {
			return err
		}
		if len(pos) != 1 {
			return errors.New("usage: stamp space init <drive-folder-url-or-id> [--name <name>]")
		}
		item, err := drive.InitSpace(context.Background(), pos[0], opts["name"])
		if err != nil {
			return err
		}
		fmt.Printf("Initialized %s\n%s\n", item.Name, item.WebURL)
		return nil
	case "pick":
		pos, opts, _, err := parseArgs(args[1:], []string{"name"}, nil)
		if err != nil {
			return err
		}
		if len(pos) != 0 {
			return errors.New("usage: stamp space pick [--name <name>]")
		}
		id, err := stampdrive.PickFolder(context.Background())
		if err != nil {
			return err
		}
		item, err := drive.InitSpace(context.Background(), id, opts["name"])
		if err != nil {
			return fmt.Errorf("connect selected Drive folder: %w", err)
		}
		fmt.Printf("Connected %s\n%s\n", item.Name, item.WebURL)
		return nil
	default:
		return fmt.Errorf("unknown space command %q", args[0])
	}
}

func pullCommand(args []string) error {
	pos, opts, flags, err := parseArgs(args, []string{"dir"}, []string{"incoming", "replace"})
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return errors.New("usage: stamp pull [--dir <directory>] [--incoming|--replace]")
	}
	mode := collab.PullSafe
	if flags["incoming"] {
		mode = collab.PullIncoming
	}
	if flags["replace"] {
		if mode != collab.PullSafe {
			return errors.New("choose --incoming or --replace, not both")
		}
		mode = collab.PullReplace
	}
	root, err := project.FindRoot(defaultString(opts["dir"], "."))
	if err != nil {
		return err
	}
	if state, _ := notioncollab.ReadState(root); state.PageID != "" {
		if mode == collab.PullIncoming {
			return errors.New("Notion pull supports safe pull and --replace; --incoming is not available")
		}
		client, err := notion.New()
		if err != nil {
			return err
		}
		state, err := notioncollab.Pull(context.Background(), client, root, mode == collab.PullReplace)
		if err == nil {
			fmt.Printf("Pulled Notion revision %d\n", state.Revision)
		}
		return err
	}
	drive, err := stampdrive.New(context.Background())
	if err != nil {
		return err
	}
	message, err := collab.Pull(context.Background(), drive, root, mode)
	if err == nil {
		fmt.Println(message)
	}
	return err
}

func pushCommand(args []string) error {
	pos, opts, _, err := parseArgs(args, []string{"dir", "space", "message", "force-with-lease"}, nil)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return errors.New("usage: stamp push [--dir <directory>] [--space <id>] [--message <text>] [--force-with-lease <version>]")
	}
	root, err := project.FindRoot(defaultString(opts["dir"], "."))
	if err != nil {
		return err
	}
	if state, _ := notioncollab.ReadState(root); state.PageID != "" {
		client, err := notion.New()
		if err != nil {
			return err
		}
		state, err := notioncollab.Push(context.Background(), client, root, opts["message"], opts["force-with-lease"])
		if err != nil {
			return err
		}
		fmt.Printf("Pushed Notion revision %d\n%s\n", state.Revision, state.URL)
		return nil
	}
	drive, err := stampdrive.New(context.Background())
	if err != nil {
		return err
	}
	state, err := collab.Push(context.Background(), drive, root, opts["space"], opts["message"], opts["force-with-lease"])
	if err != nil {
		return err
	}
	fmt.Printf("Pushed Drive version %s\n%s\n", state.BaseVersion, state.WebURL)
	return nil
}

func studioCommand(args []string) error {
	pos, opts, flags, err := parseArgs(args, []string{"dir"}, []string{"no-open"})
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return errors.New("usage: stamp studio [--dir <directory>] [--no-open]")
	}
	root, err := project.FindRoot(defaultString(opts["dir"], "."))
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return studio.Start(ctx, root, !flags["no-open"], version)
}

func doctorCommand(args []string) error {
	if len(args) != 0 {
		return errors.New("usage: stamp doctor")
	}
	failed := false
	for _, check := range doctor.Run() {
		mark := "ok"
		if !check.OK {
			mark, failed = "missing", true
		}
		fmt.Printf("%-8s %-20s %s\n", mark, check.Name, check.Detail)
	}
	if failed {
		return errors.New("some dependencies are missing")
	}
	return nil
}

func previewCommand(args []string) error {
	fs := flag.NewFlagSet("preview", flag.ContinueOnError)
	dir := fs.String("dir", ".", "project directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, err := project.FindRoot(*dir)
	if err != nil {
		return err
	}
	results, err := render.All(root)
	for _, result := range results {
		fmt.Printf("%s -> %s\n", result.Source, result.Output)
	}
	return err
}

func statusCommand(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	dir := fs.String("dir", ".", "project directory")
	jsonOutput := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, err := project.FindRoot(*dir)
	if err != nil {
		return err
	}
	if state, _ := notioncollab.ReadState(root); state.PageID != "" {
		client, err := notion.New()
		if err != nil {
			return err
		}
		status, err := notioncollab.StatusOf(context.Background(), client, root)
		if err != nil {
			return err
		}
		if *jsonOutput {
			return json.NewEncoder(os.Stdout).Encode(status)
		}
		fmt.Printf("Notion revision %d\n", status.State.Revision)
		fmt.Printf("  local changes: %v\n", status.LocalChanged)
		fmt.Printf("  remote changes: %v\n", status.RemoteChanged)
		fmt.Printf("  link: %s\n", status.State.URL)
		return nil
	}
	status, err := project.Status(root)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(status)
	}
	fmt.Printf("%s\n", status.Name)
	fmt.Printf("  files: %d\n", status.Files)
	fmt.Printf("  lease: %s\n", status.Lease)
	fmt.Printf("  local changes: %v\n", status.Dirty)
	return nil
}

func packCommand(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: stamp pack <project-directory> <archive.stamp>")
	}
	return bundle.PackFile(args[0], args[1])
}

func unpackCommand(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: stamp unpack <archive.stamp> <directory>")
	}
	return bundle.UnpackFile(args[0], args[1])
}

func usage() {
	fmt.Print(`Stamp makes document projects with people and agents.

Usage:
  stamp login | logout
  stamp google-oauth <desktop-client.json>|status|reset
  stamp notion login | logout | status | pages
  stamp notion project create --parent <page-url-or-id> [--dir <directory>] [--name <name>]
  stamp notion project open <page-url-or-id> [--dir <directory>]
  stamp notion project pull [--dir <directory>] [--replace]
  stamp notion project push [--dir <directory>] [--message <text>] [--force-with-lease <revision>]
  stamp notion project status [--dir <directory>]
  stamp space init <drive-folder-url-or-id> [--name <name>]
  stamp space pick [--name <name>]
  stamp space create <name>
  stamp space rename <drive-url-or-id> <name>
  stamp space list
	stamp template create <directory>
	stamp template preview <directory>
  stamp project create <directory> [--name <name>] [--template <theme-directory>] [--space <id-or-url>]
  stamp project list
  stamp project open <drive-url-or-id> [--dir <directory>]
  stamp project rename <name> [--dir <directory>]
  stamp project reconnect --space <id-or-url> [--dir <directory>] [--message <text>]
  stamp pull [--incoming|--replace]
  stamp push [--space <id>] [--message <text>] [--force-with-lease <version>]
  stamp studio [--dir <directory>] [--no-open]
  stamp agent setup [--dir <directory>]
  stamp mcp serve
  stamp doctor
  stamp preview [--dir <directory>]
  stamp status [--dir <directory>] [--json]
  stamp pack <project-directory> <archive.stamp>
  stamp unpack <archive.stamp> <directory>
  stamp version
`)
}

func parseArgs(args, valueOptions, booleanOptions []string) ([]string, map[string]string, map[string]bool, error) {
	wantsValue := map[string]bool{}
	for _, name := range valueOptions {
		wantsValue[name] = true
	}
	wantsBoolean := map[string]bool{}
	for _, name := range booleanOptions {
		wantsBoolean[name] = true
	}
	var positional []string
	values := map[string]string{}
	flags := map[string]bool{}
	for i := 0; i < len(args); i++ {
		if !strings.HasPrefix(args[i], "--") {
			positional = append(positional, args[i])
			continue
		}
		name := strings.TrimPrefix(args[i], "--")
		if wantsValue[name] {
			if i+1 >= len(args) {
				return nil, nil, nil, fmt.Errorf("--%s needs a value", name)
			}
			values[name] = args[i+1]
			i++
		} else if wantsBoolean[name] {
			flags[name] = true
		} else {
			return nil, nil, nil, fmt.Errorf("unknown option --%s", name)
		}
	}
	return positional, values, flags, nil
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
