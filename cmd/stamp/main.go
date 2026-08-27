package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jhleao/stamp/internal/collab"
	"github.com/jhleao/stamp/internal/diagnostic"
	"github.com/jhleao/stamp/internal/doctor"
	stampdrive "github.com/jhleao/stamp/internal/drive"
	"github.com/jhleao/stamp/internal/project"
	"github.com/jhleao/stamp/internal/render"
	"github.com/jhleao/stamp/internal/studio"
	"github.com/jhleao/stamp/internal/updater"
)

var version = "dev"

func main() {
	args, verbose := globalOptions(os.Args[1:])
	if verbose || diagnostic.EnabledByEnvironment() {
		diagnostic.Enable(os.Stderr)
	}
	started := time.Now()
	command := "help"
	if len(args) > 0 {
		command = args[0]
	}
	diagnostic.Log("cli", "start", "version", version, "command", command, "pid", os.Getpid())
	if automaticUpdateChecksEnabled() && shouldCheckForUpdate(args) {
		checkDone := diagnostic.Start("updater", "startup_check", "version", version)
		if update, err := updater.StartupCheck(version); err == nil && update.Available {
			command := "stamp update"
			if updater.ManagedByHomebrew() {
				command = "brew upgrade stamp"
			}
			fmt.Fprintf(os.Stderr, "Stamp %s is available. Run `%s`.\n", update.Latest, command)
			checkDone(nil)
		} else {
			checkDone(err)
		}
	}
	if err := run(args); err != nil {
		diagnostic.Log("cli", "exit", "command", command, "duration", time.Since(started).Round(time.Millisecond), "error", err)
		fmt.Fprintln(os.Stderr, "stamp:", err)
		os.Exit(1)
	}
	diagnostic.Log("cli", "exit", "command", command, "duration", time.Since(started).Round(time.Millisecond), "status", "ok")
}

func globalOptions(args []string) ([]string, bool) {
	verbose := false
	for len(args) > 0 && args[0] == "--verbose" {
		verbose = true
		args = args[1:]
	}
	return args, verbose
}

func automaticUpdateChecksEnabled() bool {
	if os.Getenv("CI") != "" || os.Getenv("STAMP_NO_UPDATE_CHECK") != "" {
		return false
	}
	info, err := os.Stderr.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
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
	case "version", "--version", "-v":
		fmt.Println(version)
		return nil
	case "update":
		return updateCommand(args[1:])
	case "setup":
		return setupCommand(args[1:])
	case "new":
		return newCommand(args[1:])
	case "clone":
		return cloneCommand(args[1:])
	case "pull":
		return pullCommand(args[1:])
	case "push":
		return pushCommand(args[1:])
	case "studio":
		return studioCommand(args[1:])
	case "skill":
		if len(args) != 1 {
			return errors.New("usage: stamp skill")
		}
		fmt.Print(project.AgentGuide())
		catalog, err := render.ComponentCatalog(".")
		if err != nil {
			return fmt.Errorf("read workspace components: %w", err)
		}
		if len(catalog) > 0 {
			fmt.Println("\n## Components in this workspace")
			for _, component := range catalog {
				fmt.Printf("\n- <%s>", component.Name)
				if component.Description != "" {
					fmt.Printf(": %s", component.Description)
				}
				if component.Usage != "" {
					fmt.Printf("\n  Use: %s", component.Usage)
				}
				fmt.Println()
			}
		}
		return nil
	case "tutorial":
		if len(args) != 1 {
			return errors.New("usage: stamp tutorial")
		}
		fmt.Print(tutorial)
		return nil
	case "doctor":
		return doctorCommand(args[1:])
	case "help", "--help", "-h":
		usage()
		return nil
	default:
		return fmt.Errorf("unknown command %q (try stamp help)", args[0])
	}
}

const tutorial = `# Stamp quickstart

Stamp keeps a document project in Google Drive and opens a local Studio where
you can edit its content, components, and visual theme.

## 1. Set up this computer

    stamp setup

Stamp checks the authoring tools, offers to install anything missing, connects
Google Drive, and guides you into creating or cloning a project.
Release builds also support ` + "`stamp update`" + ` and notify you when a newer
version is available.

## 2. Create your first project

    stamp new first-project --name "First project"
    cd first-project

Stamp creates the local folder and its connected Google Drive project together.
Add ` + "`--choose-drive-folder`" + ` to choose its Drive location; otherwise it is
created in My Drive.
The folder contains readable Markdown content and its complete visual theme.

## 3. Open Studio

    stamp studio

Edit Content for the words. Edit Templates for TSX components, Tailwind, and
assets. Save and inspect the preview before sharing.

## 4. Start a session with an AI agent

Most Stamp work is designed to happen with an AI agent. In Studio, click the
robot button at the top right of the sidebar. It copies a ready-to-paste prompt
that gives the agent this workspace's absolute path and asks it to run
` + "`stamp skill`" + ` before editing. Paste that prompt into your coding agent to begin.

You can also prepare an agent manually from the project folder:

    stamp skill

## Everyday collaboration

    stamp pull
    # Edit in Studio or ask an agent to edit this folder.
    stamp push --message "Describe what changed"

Pull before editing. Push only when the version is ready for other people.
Stamp refuses to overwrite a newer Drive version without an explicit review.

If collaboration fails, capture sanitized production diagnostics:

    stamp --verbose pull 2>&1 | tee stamp-debug.log

The same global flag works with clone, push, studio, login, and other commands.

## Add a colleague

Share the project folder with them in Google Drive. After they install Stamp and
run ` + "`stamp login`" + `:

    stamp clone first-project
    # Choose the shared .stamp project file in Google Drive.
    cd first-project
    stamp studio

The Studio robot button is the quickest way to start another agent session.
New projects also contain AGENTS.md and CLAUDE.md instructions.
`

func setupCommand(args []string) error {
	if len(args) != 0 {
		return errors.New("usage: stamp setup")
	}
	if !isTerminal(os.Stdin) {
		return errors.New("setup is interactive; run it in a terminal")
	}
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Stamp setup")
	fmt.Println()
	missing := false
	for _, check := range doctor.Run() {
		if !check.OK && check.Name != "Google OAuth" {
			missing = true
			fmt.Printf("missing  %-20s %s\n", check.Name, check.Detail)
		}
	}
	if missing {
		ok, err := confirm(reader, "Install missing tools with Homebrew?", true)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("setup stopped; run `stamp doctor` for the missing tools")
		}
		if err := doctor.InstallMissing(context.Background(), os.Stdout); err != nil {
			return err
		}
	}
	if err := doctorCommand(nil); err != nil {
		return err
	}

	ok, err := confirm(reader, "Connect Google Drive now?", true)
	if err != nil {
		return err
	}
	if ok {
		message, err := stampdrive.Login(context.Background())
		if err != nil {
			return err
		}
		fmt.Println(message)
	}

	fmt.Print("\nNext: [1] Clone a project  [2] Create a project  [3] Finish\nChoose [1]: ")
	choice, err := readLine(reader)
	if err != nil {
		return err
	}
	switch choice {
	case "", "1":
		return setupClone(reader)
	case "2":
		return setupNew(reader)
	case "3":
		fmt.Println("Setup complete. Run `stamp tutorial` whenever you need the workflow.")
		return nil
	default:
		return errors.New("choose 1, 2, or 3")
	}
}

func setupClone(reader *bufio.Reader) error {
	fmt.Print("Local workspace folder [stamp-project]: ")
	dir, err := readLine(reader)
	if err != nil {
		return err
	}
	dir = defaultString(dir, "stamp-project")
	if err := cloneCommand([]string{dir}); err != nil {
		return err
	}
	return offerStudio(reader, dir)
}

func setupNew(reader *bufio.Reader) error {
	fmt.Print("Local workspace folder [my-project]: ")
	dir, err := readLine(reader)
	if err != nil {
		return err
	}
	dir = defaultString(dir, "my-project")
	fmt.Print("Project name [My Project]: ")
	name, err := readLine(reader)
	if err != nil {
		return err
	}
	name = defaultString(name, "My Project")
	args := []string{dir, "--name", name}
	chooseFolder, err := confirm(reader, "Choose a Google Drive folder?", false)
	if err != nil {
		return err
	}
	if chooseFolder {
		args = append(args, "--choose-drive-folder")
	}
	if err := newCommand(args); err != nil {
		return err
	}
	return offerStudio(reader, dir)
}

func offerStudio(reader *bufio.Reader, dir string) error {
	ok, err := confirm(reader, "Open Studio now?", true)
	if err != nil || !ok {
		return err
	}
	return studioCommand([]string{"--dir", dir})
}

func confirm(reader *bufio.Reader, prompt string, defaultYes bool) (bool, error) {
	suffix := " [y/N] "
	if defaultYes {
		suffix = " [Y/n] "
	}
	fmt.Print(prompt + suffix)
	answer, err := readLine(reader)
	if err != nil {
		return false, err
	}
	if answer == "" {
		return defaultYes, nil
	}
	answer = strings.ToLower(answer)
	return answer == "y" || answer == "yes", nil
}

func readLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func newCommand(args []string) error {
	pos, opts, flags, err := parseArgs(args, []string{"name"}, []string{"choose-drive-folder"})
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("usage: stamp new <directory> [--name <name>] [--choose-drive-folder]")
	}
	dir, err := filepath.Abs(pos[0])
	if err != nil {
		return err
	}
	drive, err := stampdrive.New(context.Background())
	if err != nil {
		return fmt.Errorf("connect Google Drive: %w", err)
	}
	parentID := "root"
	if flags["choose-drive-folder"] {
		parentID, err = stampdrive.PickDestinationFolder(context.Background())
		if err != nil {
			return fmt.Errorf("choose Google Drive destination: %w", err)
		}
	}
	manifest, err := project.Create(dir, opts["name"])
	if err != nil {
		return err
	}
	state, err := collab.Create(context.Background(), drive, dir, parentID)
	if err != nil {
		return fmt.Errorf("created %s locally, but could not create its Drive project: %w", dir, err)
	}
	rememberProject(dir)
	fmt.Printf("Created %s\n%s\n%s\n", manifest.Name, dir, state.WebURL)
	return nil
}

func cloneCommand(args []string) error {
	if len(args) > 1 {
		return errors.New("usage: stamp clone [directory]")
	}
	destination := ""
	if len(args) == 1 {
		destination = args[0]
	}
	id, err := stampdrive.PickProjectArchive(context.Background())
	if err != nil {
		return err
	}
	drive, err := stampdrive.New(context.Background())
	if err != nil {
		return err
	}
	workspace, err := collab.Open(context.Background(), drive, id, destination)
	if err != nil {
		return err
	}
	rememberProject(workspace.Root)
	fmt.Printf("Cloned Drive version %s\n", workspace.State.BaseVersion)
	return nil
}

func rememberProject(root string) {
	if err := project.Remember(root); err != nil {
		fmt.Fprintln(os.Stderr, "warning: could not remember this workspace:", err)
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
	pos, opts, _, err := parseArgs(args, []string{"dir", "message", "force-with-lease"}, nil)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return errors.New("usage: stamp push [--dir <directory>] [--message <text>] [--force-with-lease <version>]")
	}
	root, err := project.FindRoot(defaultString(opts["dir"], "."))
	if err != nil {
		return err
	}
	drive, err := stampdrive.New(context.Background())
	if err != nil {
		return err
	}
	state, err := collab.Push(context.Background(), drive, root, opts["message"], opts["force-with-lease"])
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
	root, err := studioRoot(opts["dir"])
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return studio.Start(ctx, root, !flags["no-open"], version)
}

func studioRoot(directory string) (string, error) {
	if directory != "" {
		return project.FindRoot(directory)
	}
	if root, err := project.FindRoot("."); err == nil {
		return root, nil
	}
	if !isTerminal(os.Stdin) {
		return "", errors.New("no Stamp project found; run stamp studio in a project or pass --dir <directory>")
	}
	projects, err := project.KnownProjects()
	if err != nil {
		return "", fmt.Errorf("read known Stamp projects: %w", err)
	}
	if len(projects) == 0 {
		return "", errors.New("no known Stamp projects; create one with stamp new or check one out with stamp clone")
	}

	fmt.Println("Select a Stamp project:")
	for index, known := range projects {
		fmt.Printf("  %d. %s\n     %s\n", index+1, known.Name, known.Path)
	}
	fmt.Print("Choose [1]: ")
	choice, err := readLine(bufio.NewReader(os.Stdin))
	if err != nil {
		return "", err
	}
	if choice == "" {
		return projects[0].Path, nil
	}
	selection, err := strconv.Atoi(choice)
	if err != nil || selection < 1 || selection > len(projects) {
		return "", fmt.Errorf("choose a number from 1 to %d", len(projects))
	}
	return projects[selection-1].Path, nil
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

func updateCommand(args []string) error {
	pos, _, flags, err := parseArgs(args, nil, []string{"check", "yes"})
	if err != nil {
		return err
	}
	if len(pos) != 0 || (flags["check"] && flags["yes"]) {
		return errors.New("usage: stamp update [--check|--yes]")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if flags["check"] {
		result, err := updater.Check(ctx, version)
		if err != nil {
			return err
		}
		if result.Available {
			fmt.Printf("Stamp %s is available (installed: %s)\n%s\n", result.Latest, result.Current, result.PageURL)
		} else {
			fmt.Printf("Stamp %s is up to date.\n", version)
		}
		return nil
	}

	result, err := updater.Check(ctx, version)
	if err != nil {
		return err
	}
	if !result.Available {
		fmt.Printf("Stamp %s is up to date.\n", version)
		return nil
	}
	if updater.ManagedByHomebrew() {
		fmt.Printf("Stamp %s is available. This installation is managed by Homebrew; run `brew upgrade stamp`.\n", result.Latest)
		return nil
	}
	if !flags["yes"] {
		info, err := os.Stdin.Stat()
		if err != nil || info.Mode()&os.ModeCharDevice == 0 {
			return fmt.Errorf("Stamp %s is available; rerun with --yes to install it", result.Latest)
		}
		fmt.Printf("Stamp %s is available:\n%s\nUpdate Stamp %s → %s? [y/N] ", result.Latest, result.PageURL, version, result.Latest)
		var answer string
		if _, err := fmt.Fscanln(os.Stdin, &answer); err != nil {
			fmt.Println("Update cancelled.")
			return nil
		}
		if strings.ToLower(strings.TrimSpace(answer)) != "y" && strings.ToLower(strings.TrimSpace(answer)) != "yes" {
			fmt.Println("Update cancelled.")
			return nil
		}
	}
	release, installed, err := updater.Install(ctx, version, result.Latest)
	if err != nil {
		return err
	}
	if !installed {
		fmt.Printf("Stamp %s is up to date.\n", version)
		return nil
	}
	fmt.Printf("Updated Stamp to %s. Restart any running Studio process.\n", release.Version)
	return nil
}

func shouldCheckForUpdate(args []string) bool {
	if len(args) == 0 {
		return true
	}
	switch args[0] {
	case "update", "version", "--version", "-v", "help", "--help", "-h":
		return false
	default:
		return true
	}
}

func usage() {
	fmt.Print(`Stamp makes document projects with people and agents.

Usage:
	stamp [--verbose] <command>
  stamp setup
  stamp login | logout
  stamp new <directory> [--name <name>] [--choose-drive-folder]
  stamp clone [directory]
  stamp pull [--incoming|--replace]
  stamp push [--message <text>] [--force-with-lease <version>]
  stamp studio [--dir <directory>] [--no-open]
  stamp tutorial
  stamp skill
  stamp doctor
  stamp update [--check|--yes]
  stamp version

Use --verbose before a command to print sanitized production
diagnostics to stderr. STAMP_VERBOSE=1 enables the same logging.
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
