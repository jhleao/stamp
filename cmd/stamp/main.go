package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/weve-ai/stamp/internal/bundle"
	"github.com/weve-ai/stamp/internal/project"
	"github.com/weve-ai/stamp/internal/render"
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
	case "version", "--version", "-v":
		fmt.Println(version)
		return nil
	case "project":
		return projectCommand(args[1:])
	case "preview":
		return previewCommand(args[1:])
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
	if len(args) == 0 || args[0] != "create" {
		return errors.New("usage: stamp project create <directory> [--name <name>]")
	}
	var dirArg, name string
	for i := 1; i < len(args); i++ {
		if args[i] == "--name" && i+1 < len(args) {
			name = args[i+1]
			i++
			continue
		}
		if strings.HasPrefix(args[i], "-") || dirArg != "" {
			return errors.New("usage: stamp project create <directory> [--name <name>]")
		}
		dirArg = args[i]
	}
	if dirArg == "" {
		return errors.New("usage: stamp project create <directory> [--name <name>]")
	}
	dir, err := filepath.Abs(dirArg)
	if err != nil {
		return err
	}
	p, err := project.Create(dir, name)
	if err != nil {
		return err
	}
	fmt.Printf("Created %s at %s\n", p.Name, dir)
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
  stamp project create <directory> [--name <name>]
  stamp preview [--dir <directory>]
  stamp status [--dir <directory>] [--json]
  stamp pack <project-directory> <archive.stamp>
  stamp unpack <archive.stamp> <directory>
  stamp version
`)
}
