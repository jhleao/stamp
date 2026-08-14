package themepreview

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jhleao/stamp/internal/project"
	"github.com/jhleao/stamp/internal/render"
)

// All renders a standalone theme's visual examples into <theme>/outputs.
// The temporary project hides the project-shaped renderer from theme authors.
func All(themeDir string) ([]render.Result, error) {
	themeDir, err := filepath.Abs(themeDir)
	if err != nil {
		return nil, err
	}
	workspace, err := os.MkdirTemp("", "stamp-theme-preview-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(workspace)
	if _, err := project.CreateWithTheme(workspace, "Theme preview", themeDir); err != nil {
		return nil, err
	}
	results, renderErr := render.All(workspace)
	var copied []render.Result
	for _, result := range results {
		if !strings.HasPrefix(result.Source, "theme/examples/") {
			continue
		}
		name := strings.TrimPrefix(result.Output, "outputs/theme/examples/")
		destination := filepath.Join(themeDir, "outputs", filepath.FromSlash(name))
		if err := copyFile(filepath.Join(workspace, result.Output), destination); err != nil {
			return copied, err
		}
		copied = append(copied, render.Result{Source: strings.TrimPrefix(result.Source, "theme/"), Output: filepath.ToSlash(filepath.Join("outputs", name))})
	}
	if renderErr != nil {
		return copied, renderErr
	}
	if len(copied) == 0 {
		return nil, fmt.Errorf("theme has no previewable examples")
	}
	return copied, nil
}

func copyFile(source, destination string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	return os.WriteFile(destination, data, 0o644)
}
