package project

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const ManifestName = "stamp.yaml"

type Manifest struct {
	ID   string `yaml:"id" json:"id"`
	Name string `yaml:"name" json:"name"`
}

type RemoteState struct {
	FileID          string            `json:"fileId,omitempty"`
	ProjectFolderID string            `json:"projectFolderId,omitempty"`
	CurrentFolderID string            `json:"currentFolderId,omitempty"`
	BaseVersion     string            `json:"baseVersion,omitempty"`
	BaseHash        string            `json:"baseHash,omitempty"`
	WebURL          string            `json:"webUrl,omitempty"`
	Files           map[string]string `json:"files,omitempty"`
}

type ProjectStatus struct {
	Name  string `json:"name"`
	Root  string `json:"root"`
	Files int    `json:"files"`
	Lease string `json:"lease"`
	Dirty bool   `json:"dirty"`
}

func Create(root, name string) (Manifest, error) {
	if name == "" {
		name = filepath.Base(root)
	}
	if _, err := os.Stat(root); err == nil {
		entries, readErr := os.ReadDir(root)
		if readErr != nil {
			return Manifest{}, readErr
		}
		if len(entries) != 0 {
			return Manifest{}, fmt.Errorf("%s is not empty", root)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Manifest{}, err
	}
	for _, dir := range []string{"documents", "decks", "spreadsheets", "theme/examples", "assets", "outputs", ".stamp"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			return Manifest{}, err
		}
	}
	manifest := Manifest{ID: newID(), Name: name}
	if err := writeYAML(filepath.Join(root, ManifestName), manifest); err != nil {
		return Manifest{}, err
	}
	files := map[string]string{
		"theme/page.html.tmpl":                pageTemplate,
		"theme/deck.html.tmpl":                deckTemplate,
		"theme/page.css":                      pageCSS,
		"theme/deck.css":                      deckCSS,
		"theme/examples/welcome-page.page.md": "---\ntitle: Welcome\n---\n# Welcome\n\nThis is a new Stamp project.\n\n<callout>Change the words, CSS, or template and preview it again.</callout>\n",
		"theme/examples/welcome-deck.deck.md": "---\ntitle: Welcome deck\n---\n<slide>\n\n# Welcome\n\nA small deck made with Stamp.\n\n</slide>\n\n<slide>\n\n## One source, one preview\n\n- Edit ordinary files\n- Push one complete version\n\n</slide>\n",
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o644); err != nil {
			return Manifest{}, err
		}
	}
	return manifest, nil
}

func Load(root string) (Manifest, error) {
	data, err := os.ReadFile(filepath.Join(root, ManifestName))
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("read %s: %w", ManifestName, err)
	}
	if manifest.ID == "" || manifest.Name == "" {
		return Manifest{}, fmt.Errorf("%s needs id and name", ManifestName)
	}
	return manifest, nil
}

func Rename(root, name string) error {
	manifest, err := Load(root)
	if err != nil {
		return err
	}
	if strings.TrimSpace(name) == "" {
		return errors.New("project name is required")
	}
	manifest.Name = name
	return writeYAML(filepath.Join(root, ManifestName), manifest)
}

func FindRoot(start string) (string, error) {
	current, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	if info, statErr := os.Stat(current); statErr == nil && !info.IsDir() {
		current = filepath.Dir(current)
	}
	for {
		if _, err := os.Stat(filepath.Join(current, ManifestName)); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("no %s found from %s", ManifestName, start)
		}
		current = parent
	}
}

func ReadState(root string) (RemoteState, error) {
	data, err := os.ReadFile(filepath.Join(root, ".stamp", "state.json"))
	if errors.Is(err, os.ErrNotExist) {
		return RemoteState{}, nil
	}
	if err != nil {
		return RemoteState{}, err
	}
	var state RemoteState
	if err := json.Unmarshal(data, &state); err != nil {
		return RemoteState{}, err
	}
	return state, nil
}

func WriteState(root string, state RemoteState) error {
	if err := os.MkdirAll(filepath.Join(root, ".stamp"), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(root, ".stamp", "state.json"), data, 0o600)
}

func FileHashes(root string) (map[string]string, error) {
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if rel == ".stamp" || rel == "outputs" || strings.HasPrefix(rel, ".stamp"+string(filepath.Separator)) {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		files[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	return files, err
}

func Status(root string) (ProjectStatus, error) {
	manifest, err := Load(root)
	if err != nil {
		return ProjectStatus{}, err
	}
	state, err := ReadState(root)
	if err != nil {
		return ProjectStatus{}, err
	}
	hashes, err := FileHashes(root)
	if err != nil {
		return ProjectStatus{}, err
	}
	dirty := len(state.Files) > 0 && !equalHashes(hashes, state.Files)
	lease := state.BaseVersion
	if lease == "" {
		lease = "local only"
	}
	return ProjectStatus{Name: manifest.Name, Root: root, Files: len(hashes), Lease: lease, Dirty: dirty}, nil
}

func SortedFiles(hashes map[string]string) []string {
	files := make([]string, 0, len(hashes))
	for name := range hashes {
		files = append(files, name)
	}
	sort.Strings(files)
	return files
}

func equalHashes(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for name, hash := range a {
		if b[name] != hash {
			return false
		}
	}
	return true
}

func writeYAML(path string, value any) error {
	data, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func newID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16])
}

const pageTemplate = `<!doctype html>
<html><head><meta charset="utf-8"><base href="{{.BaseURL}}"><title>{{.Title}}</title><style>{{.CSS}}</style></head>
<body><main>{{.Content}}</main></body></html>`

const deckTemplate = `<!doctype html>
<html><head><meta charset="utf-8"><base href="{{.BaseURL}}"><title>{{.Title}}</title><style>{{.CSS}}</style></head>
<body>{{.Content}}</body></html>`

const pageCSS = `@page { size: A4; margin: 18mm; }
:root { font-family: -apple-system, BlinkMacSystemFont, "Helvetica Neue", sans-serif; color: #171717; }
body { margin: 0; line-height: 1.5; }
h1 { font-size: 34px; line-height: 1.05; margin: 0 0 24px; }
h2 { margin-top: 32px; }
callout { display: block; margin: 24px 0; padding: 18px; border-left: 4px solid #7357ff; background: #f3f0ff; }
img { max-width: 100%; }
`

const deckCSS = `@page { size: 13.333in 7.5in; margin: 0; }
:root { font-family: -apple-system, BlinkMacSystemFont, "Helvetica Neue", sans-serif; color: #171717; }
body { margin: 0; }
slide { box-sizing: border-box; display: flex; flex-direction: column; width: 13.333in; height: 7.5in; padding: .75in; break-after: page; overflow: hidden; }
slide:last-child { break-after: auto; }
h1 { font-size: 54px; line-height: 1.02; margin: 0 0 28px; }
h2 { font-size: 38px; }
p, li { font-size: 24px; line-height: 1.35; }
`
