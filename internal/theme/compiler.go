package theme

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CompileIfNeeded refreshes generated page.css and deck.css when Tailwind
// authoring sources changed. Projects without tailwind.css remain supported.
func CompileIfNeeded(ctx context.Context, root string) error {
	input := filepath.Join(root, "theme", "tailwind.css")
	if _, err := os.Stat(input); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	stale, err := staleOutputs(root)
	if err != nil || !stale {
		return err
	}
	binary, commandDir := Compiler(root)
	if binary == "" {
		return errors.New("Tailwind theme changed, but the tailwindcss compiler is unavailable; install Stamp's authoring tools or rebuild the theme in Studio")
	}
	compilerInput := input
	if tailwindCSS := filepath.Join(commandDir, "node_modules", "tailwindcss", "index.css"); exists(tailwindCSS) {
		source, err := os.ReadFile(input)
		if err != nil {
			return err
		}
		rewritten := strings.Replace(string(source), `"tailwindcss"`, fmt.Sprintf("%q", filepath.ToSlash(tailwindCSS)), 1)
		temp, err := os.CreateTemp(filepath.Dir(input), ".stamp-tailwind-*.css")
		if err != nil {
			return err
		}
		compilerInput = temp.Name()
		defer os.Remove(compilerInput)
		if _, err = temp.WriteString(rewritten); err == nil {
			err = temp.Close()
		} else {
			_ = temp.Close()
		}
		if err != nil {
			return err
		}
	}
	temporary, err := os.CreateTemp(filepath.Join(root, "theme"), ".stamp-page-*.css")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		return err
	}
	defer os.Remove(temporaryPath)
	command := exec.CommandContext(ctx, binary, "-i", compilerInput, "-o", temporaryPath, "--minify")
	command.Dir = commandDir
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("Tailwind build: %s", strings.TrimSpace(string(output)))
	}
	data, err := os.ReadFile(temporaryPath)
	if err != nil {
		return err
	}
	for _, name := range []string{"page.css", "deck.css"} {
		if err := writeAtomic(filepath.Join(root, "theme", name), data); err != nil {
			return err
		}
	}
	return nil
}

func staleOutputs(root string) (bool, error) {
	oldestOutput := int64(1<<63 - 1)
	for _, name := range []string{"page.css", "deck.css"} {
		info, err := os.Stat(filepath.Join(root, "theme", name))
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		if timestamp := info.ModTime().UnixNano(); timestamp < oldestOutput {
			oldestOutput = timestamp
		}
	}
	newestSource := int64(0)
	err := filepath.WalkDir(filepath.Join(root, "theme"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Name() == "page.css" || entry.Name() == "deck.css" || strings.HasPrefix(entry.Name(), ".stamp-") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if timestamp := info.ModTime().UnixNano(); timestamp > newestSource {
			newestSource = timestamp
		}
		return nil
	})
	return newestSource > oldestOutput, err
}

// Compiler returns the Tailwind executable and the directory that owns its
// package installation. It is also used by doctor so setup checks match builds.
func Compiler(root string) (string, string) {
	if binary := os.Getenv("STAMP_TAILWIND_BIN"); binary != "" {
		return binary, packageRoot(binary, root)
	}
	if binary, err := exec.LookPath("tailwindcss"); err == nil {
		return binary, packageRoot(binary, root)
	}
	if cwd, err := os.Getwd(); err == nil {
		candidate := filepath.Join(cwd, "node_modules", ".bin", "tailwindcss")
		if exists(candidate) {
			return candidate, cwd
		}
	}
	if executable, err := os.Executable(); err == nil {
		packageDir := filepath.Clean(filepath.Join(filepath.Dir(executable), ".."))
		candidate := filepath.Join(packageDir, "node_modules", ".bin", "tailwindcss")
		if exists(candidate) {
			return candidate, packageDir
		}
	}
	return "", root
}

func packageRoot(binary, fallback string) string {
	binDir := filepath.Dir(binary)
	if filepath.Base(binDir) == ".bin" && filepath.Base(filepath.Dir(binDir)) == "node_modules" {
		return filepath.Dir(filepath.Dir(binDir))
	}
	return fallback
}

func writeAtomic(path string, data []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".stamp-css-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if _, err = temp.Write(data); err == nil {
		err = temp.Close()
	} else {
		_ = temp.Close()
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}

func exists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
