package project

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const recentProjectLimit = 20

type KnownProject struct {
	Name string
	Path string
}

type recentProjects struct {
	Paths []string `json:"paths"`
}

// Remember records a connected project as the most recently opened workspace.
func Remember(root string) error {
	path, err := canonicalPath(root)
	if err != nil {
		return err
	}
	if _, err := validKnownProject(path); err != nil {
		return err
	}
	registry, err := readRecentProjects()
	if err != nil {
		return err
	}
	paths := []string{path}
	for _, known := range registry.Paths {
		if known != path && len(paths) < recentProjectLimit {
			paths = append(paths, known)
		}
	}
	return writeRecentProjects(recentProjects{Paths: paths})
}

// KnownProjects returns valid connected workspaces in most-recently-opened
// order and removes stale entries from the registry.
func KnownProjects() ([]KnownProject, error) {
	registry, err := readRecentProjects()
	if err != nil {
		return nil, err
	}
	projects := make([]KnownProject, 0, len(registry.Paths))
	paths := make([]string, 0, len(registry.Paths))
	seen := map[string]bool{}
	changed := false
	for _, saved := range registry.Paths {
		path, err := canonicalPath(saved)
		if err != nil || seen[path] {
			changed = true
			continue
		}
		manifest, err := validKnownProject(path)
		if err != nil {
			changed = true
			continue
		}
		changed = changed || path != saved
		seen[path] = true
		paths = append(paths, path)
		projects = append(projects, KnownProject{Name: manifest.Name, Path: path})
	}
	if changed {
		if err := writeRecentProjects(recentProjects{Paths: paths}); err != nil {
			return nil, err
		}
	}
	return projects, nil
}

func validKnownProject(root string) (Manifest, error) {
	manifest, err := Load(root)
	if err != nil {
		return Manifest{}, err
	}
	connected, err := Connected(root)
	if err != nil {
		return Manifest{}, err
	}
	if !connected {
		return Manifest{}, errors.New("project is not connected")
	}
	return manifest, nil
}

func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}

func recentProjectsPath() (string, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "Stamp", "projects.json"), nil
}

func readRecentProjects() (recentProjects, error) {
	path, err := recentProjectsPath()
	if err != nil {
		return recentProjects{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return recentProjects{}, nil
	}
	if err != nil {
		return recentProjects{}, err
	}
	var registry recentProjects
	if err := json.Unmarshal(data, &registry); err != nil {
		empty := recentProjects{}
		return empty, writeRecentProjects(empty)
	}
	return registry, nil
}

func writeRecentProjects(registry recentProjects) error {
	path, err := recentProjectsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(filepath.Dir(path), "projects-*.json")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
