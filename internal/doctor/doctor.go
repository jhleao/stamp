package doctor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"

	stampdrive "github.com/jhleao/stamp/internal/drive"
	"github.com/jhleao/stamp/internal/render"
	"github.com/jhleao/stamp/internal/theme"
)

var brewPackages = map[string][]string{
	"Chrome":      {"install", "--cask", "google-chrome"},
	"LibreOffice": {"install", "--cask", "libreoffice"},
	"Pandoc":      {"install", "pandoc"},
	"Tailwind":    {"install", "tailwindcss"},
}

type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

func Run() []Check {
	checks := []Check{{Name: "macOS", OK: runtime.GOOS == "darwin", Detail: runtime.GOOS}}
	path, err := render.ChromePath()
	checks = append(checks, executable("Chrome", path, err))
	path, err = exec.LookPath("soffice")
	checks = append(checks, executable("LibreOffice", path, err))
	path, err = exec.LookPath("pandoc")
	checks = append(checks, executable("Pandoc", path, err))
	root, _ := os.Getwd()
	path, _ = theme.Compiler(root)
	if path == "" {
		err = exec.ErrNotFound
	} else {
		err = nil
	}
	checks = append(checks, executable("Tailwind", path, err))
	credentials, credentialErr := stampdrive.Credentials()
	detail := string(credentials.Source)
	if credentials.Path != "" {
		detail += " (" + credentials.Path + ")"
	}
	checks = append(checks, Check{Name: "Google OAuth", OK: credentialErr == nil, Detail: detail})
	return checks
}

// InstallMissing installs Stamp's external authoring tools with Homebrew.
// Google authentication is deliberately handled by `stamp login` instead.
func InstallMissing(ctx context.Context, output io.Writer) error {
	brew, err := exec.LookPath("brew")
	if err != nil {
		return errors.New("Homebrew is required to install missing tools; install it from https://brew.sh, then run `stamp setup` again")
	}
	for _, check := range Run() {
		args, installable := brewPackages[check.Name]
		if check.OK || !installable {
			continue
		}
		fmt.Fprintf(output, "Installing %s...\n", check.Name)
		command := exec.CommandContext(ctx, brew, args...)
		command.Stdout = output
		command.Stderr = output
		if err := command.Run(); err != nil {
			return fmt.Errorf("install %s: %w", check.Name, err)
		}
	}
	return nil
}

func executable(name string, path string, err error) Check {
	if err != nil {
		return Check{Name: name, Detail: err.Error()}
	}
	return Check{Name: name, OK: true, Detail: path}
}
