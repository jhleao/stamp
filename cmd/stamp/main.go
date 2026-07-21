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

	"github.com/weve-ai/stamp/internal/agent"
	"github.com/weve-ai/stamp/internal/bundle"
	"github.com/weve-ai/stamp/internal/collab"
	"github.com/weve-ai/stamp/internal/doctor"
	stampdrive "github.com/weve-ai/stamp/internal/drive"
	"github.com/weve-ai/stamp/internal/project"
	"github.com/weve-ai/stamp/internal/render"
	"github.com/weve-ai/stamp/internal/studio"
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
		if len(args) != 2 {
			return errors.New("usage: stamp google-oauth <desktop-client.json>")
		}
		destination, err := stampdrive.InstallConfig(args[1])
		if err == nil {
			fmt.Println("Installed Google OAuth config at", destination)
		}
		return err
	case "space":
		return spaceCommand(args[1:])
	case "version", "--version", "-v":
		fmt.Println(version)
		return nil
	case "project":
		return projectCommand(args[1:])
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

func projectCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: stamp project <create|list|open>")
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
		pos, opts, _, err := parseArgs(args[1:], "dir")
		if err != nil || len(pos) != 1 {
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
	default:
		return fmt.Errorf("unknown project command %q", args[0])
	}
}

func createProject(args []string) error {
	pos, opts, _, err := parseArgs(args, "name")
	if err != nil || len(pos) != 1 {
		return errors.New("usage: stamp project create <directory> [--name <name>]")
	}
	dir, err := filepath.Abs(pos[0])
	if err != nil {
		return err
	}
	p, err := project.Create(dir, opts["name"])
	if err != nil {
		return err
	}
	fmt.Printf("Created %s at %s\n", p.Name, dir)
	return nil
}

func spaceCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: stamp space <list|create|init>")
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
		pos, opts, _, err := parseArgs(args[1:], "name")
		if err != nil || len(pos) != 1 {
			return errors.New("usage: stamp space init <drive-folder-url-or-id> [--name <name>]")
		}
		item, err := drive.InitSpace(context.Background(), pos[0], opts["name"])
		if err != nil {
			return err
		}
		fmt.Printf("Initialized %s\n%s\n", item.Name, item.WebURL)
		return nil
	default:
		return fmt.Errorf("unknown space command %q", args[0])
	}
}

func pullCommand(args []string) error {
	pos, opts, flags, err := parseArgs(args, "dir")
	if err != nil || len(pos) != 0 {
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
	pos, opts, _, err := parseArgs(args, "dir", "space", "message", "force-with-lease")
	if err != nil || len(pos) != 0 {
		return errors.New("usage: stamp push [--dir <directory>] [--space <id>] [--message <text>] [--force-with-lease <version>]")
	}
	root, err := project.FindRoot(defaultString(opts["dir"], "."))
	if err != nil {
		return err
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
	pos, opts, flags, err := parseArgs(args, "dir")
	if err != nil || len(pos) != 0 {
	}
	root, err := project.FindRoot(defaultString(opts["dir"], "."))
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return studio.Start(ctx, root, !flags["no-open"])
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
  stamp google-oauth <desktop-client.json>
  stamp space init <drive-folder-url-or-id> [--name <name>]
  stamp space create <name>
  stamp space list
  stamp project create <directory> [--name <name>]
  stamp project list
  stamp project open <drive-url-or-id> [--dir <directory>]
  stamp pull [--incoming|--replace]
  stamp push [--space <id>] [--message <text>] [--force-with-lease <version>]
  stamp studio [--dir <directory>] [--no-open]
  stamp mcp serve
  stamp doctor
  stamp preview [--dir <directory>]
  stamp status [--dir <directory>] [--json]
  stamp pack <project-directory> <archive.stamp>
  stamp unpack <archive.stamp> <directory>
  stamp version
`)
}

func parseArgs(args []string, valueOptions ...string) ([]string, map[string]string, map[string]bool, error) {
	wantsValue := map[string]bool{}
	for _, name := range valueOptions {
		wantsValue[name] = true
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
		} else {
			flags[name] = true
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
