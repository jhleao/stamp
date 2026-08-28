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
	"time"

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
	Outputs         map[string]string `json:"outputs,omitempty"`
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
	for _, dir := range []string{"documents", "decks", "spreadsheets", "theme/components", "theme/examples", "assets", "outputs", ".stamp"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			return Manifest{}, err
		}
	}
	manifest := Manifest{ID: newID(), Name: name}
	if err := writeYAML(filepath.Join(root, ManifestName), manifest); err != nil {
		return Manifest{}, err
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(agentGuide), 0o644); err != nil {
		return Manifest{}, err
	}
	if err := EnsureAgentCompatibility(root); err != nil {
		return Manifest{}, err
	}
	if err := writeStarterTheme(filepath.Join(root, "theme")); err != nil {
		return Manifest{}, err
	}
	for name, contents := range map[string]string{
		"documents/start-here.page.md": starterPage,
		"decks/start-here.deck.md":     starterDeck,
	} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte(contents), 0o644); err != nil {
			return Manifest{}, err
		}
	}
	return manifest, nil
}

// EnsureAgentCompatibility lets Claude Code consume the same canonical
// instructions as agents that read AGENTS.md. Existing custom CLAUDE.md files
// are preserved; archives containing the dereferenced guide are restored to a
// local symlink after opening or pulling.
func EnsureAgentCompatibility(root string) error {
	agentsPath := filepath.Join(root, "AGENTS.md")
	if _, err := os.Stat(agentsPath); errors.Is(err, os.ErrNotExist) {
		if writeErr := os.WriteFile(agentsPath, []byte(agentGuide), 0o644); writeErr != nil {
			return writeErr
		}
	} else if err != nil {
		return err
	}
	claudePath := filepath.Join(root, "CLAUDE.md")
	info, err := os.Lstat(claudePath)
	if errors.Is(err, os.ErrNotExist) {
		return os.Symlink("AGENTS.md", claudePath)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(claudePath)
		if err != nil {
			return err
		}
		if target == "AGENTS.md" {
			return nil
		}
		return fmt.Errorf("CLAUDE.md points to %s; expected AGENTS.md", target)
	}
	claude, readErr := os.ReadFile(claudePath)
	agents, agentsErr := os.ReadFile(agentsPath)
	if readErr != nil || agentsErr != nil || string(claude) != string(agents) {
		return nil
	}
	if err := os.Remove(claudePath); err != nil {
		return err
	}
	return os.Symlink("AGENTS.md", claudePath)
}

func writeStarterTheme(root string) error {
	for _, dir := range []string{"components", "examples"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			return err
		}
	}
	files := []struct{ name, contents string }{
		// These bootstraps make the folder self-describing before its first
		// preview. Tailwind authoring sources are written afterwards, marking
		// both generated files for a real compile on first Studio/CLI use.
		{"page.css", pageCSS},
		{"deck.css", deckCSS},
		{"page.html.tmpl", pageTemplate},
		{"deck.html.tmpl", deckTemplate},
		{"tailwind.css", tailwindSource},
		{"components/Callout.tsx", `export const metadata = {
  description: "A quiet aside for supporting information.",
  usage: "Use sparingly for context that should not interrupt the main narrative."
};

export default function Callout({ children }) {
  return (
    <aside className="callout my-6 border-y border-stone-300 bg-stone-50 px-5 py-4 text-sm leading-relaxed">
      {children}
    </aside>
  );
}
`},
		{"README.md", themeGuide},
		{"examples/welcome-page.page.md", "---\ntitle: Welcome\n---\n# Welcome\n\nThis is a new Stamp project.\n\n<Callout>Change the words, components, or Tailwind theme and watch this preview update.</Callout>\n"},
		{"examples/welcome-deck.deck.md", "---\ntitle: Welcome deck\n---\n<slide>\n\n# Welcome\n\nA small deck made with Stamp.\n\n</slide>\n\n<slide>\n\n## One source, one preview\n\n- Edit ordinary files\n- Push one complete version\n\n</slide>\n"},
	}
	for _, file := range files {
		path := filepath.Join(root, filepath.FromSlash(file.name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(file.contents), 0o644); err != nil {
			return err
		}
	}
	stale := time.Now().Add(-time.Second)
	for _, name := range []string{"page.css", "deck.css"} {
		if err := os.Chtimes(filepath.Join(root, name), stale, stale); err != nil {
			return err
		}
	}
	return nil
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

func Connected(root string) (bool, error) {
	state, err := ReadState(root)
	if err != nil {
		return false, err
	}
	return state.FileID != "" && state.ProjectFolderID != "" && state.CurrentFolderID != "" && state.BaseVersion != "", nil
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
			if filepath.ToSlash(rel) != "CLAUDE.md" {
				return nil
			}
			target, readErr := os.Readlink(path)
			if readErr != nil || target != "AGENTS.md" {
				return nil
			}
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
	dirty := state.FileID == "" || !equalHashes(hashes, state.Files)
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
<body class="bg-stone-50 text-stone-900 antialiased"><main class="stamp-page mx-auto max-w-[72rem] px-[18mm] py-[15mm]">{{.Content}}</main></body></html>`

const deckTemplate = `<!doctype html>
<html><head><meta charset="utf-8"><base href="{{.BaseURL}}"><title>{{.Title}}</title><style>{{.CSS}}</style></head>
<body class="stamp-deck bg-stone-950 text-stone-50 antialiased">{{.Content}}</body></html>`

const starterPage = `---
title: Start here
---

# Start here

Replace this with the first useful thing your team needs to say.
`

const starterDeck = `---
title: Start here
---

<slide>

# Start here

Replace this with the first useful story your team needs to present.

</slide>
`

const tailwindSource = `@import "tailwindcss" source("./");

@theme {
  --font-sans: "Avenir Next", Avenir, sans-serif;
}

@page { size: A4; margin: 0; }
@page deck { size: 13.333in 7.5in; margin: 0; }

@layer base {
  html { font-family: var(--font-sans); print-color-adjust: exact; }
  body { margin: 0; }
  h1 { @apply mb-6 text-4xl font-semibold leading-none tracking-tight; }
  h2 { @apply mt-8 mb-4 text-2xl font-semibold tracking-tight; }
  p, li { @apply leading-relaxed; }
  img { @apply max-w-full; }
}

@layer components {
  .stamp-page { min-height: 297mm; }
  .stamp-deck { page: deck; }
  .stamp-deck > slide { @apply flex h-[7.5in] w-[13.333in] flex-col overflow-hidden p-[.75in]; break-after: page; }
  .stamp-deck > slide:last-child { break-after: auto; }
  .stamp-deck h1 { @apply text-6xl; }
  .stamp-deck h2 { @apply text-4xl; }
  .stamp-deck p, .stamp-deck li { @apply text-2xl; }
}
`

const pageCSS = `@page { size: A4; margin: 18mm; }
:root { font-family: -apple-system, BlinkMacSystemFont, "Helvetica Neue", sans-serif; color: #171717; }
body { margin: 0; line-height: 1.5; }
h1 { font-size: 34px; line-height: 1.05; margin: 0 0 24px; }
h2 { margin-top: 32px; }
.callout { display: block; margin: 24px 0; padding: 18px; border: 1px solid #d9d2ca; background: #f5f1eb; }
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

const agentGuide = `# Working with this Stamp project

This workspace is a shared document pack. Keep the loop small:

1. Run ` + "`stamp pull`" + ` before changing an existing shared project.
2. Before authoring, inspect theme/README.md, theme/components/, and the theme
   examples. Open representative existing documents in Studio to learn the
   workspace's typography, spacing, composition, and visual vocabulary.
3. Reuse existing components and their variants before creating new ones. Read
   each component's exported metadata and implementation; its description and
   usage are design instructions, not just API documentation.
4. Edit Markdown in documents/ or decks/, and spreadsheets in spreadsheets/.
5. Use Studio's preview and inspect every affected page or slide. Match the
   established visual language and test both sparse and dense content.
6. Run ` + "`stamp push --message update-summary`" + ` only when the person asks to share.

The theme/ folder controls appearance. Read theme/README.md before changing it.
Never edit outputs/ directly. If pull or push reports a conflict, preserve both
versions and explain the choice instead of forcing it.
`

// AgentGuide is the canonical, tool-agnostic guide written into projects and
// printed by `stamp skill`. Agents need only ordinary files and the Stamp CLI.
func AgentGuide() string { return agentGuide }

const themeGuide = `# Theme

A Stamp theme is Tailwind utility markup compiled to inert HTML and CSS:

- page.html.tmpl and deck.html.tmpl wrap written pages and slide decks.
- tailwind.css holds design tokens, shared rules, and print primitives.
- page.css and deck.css are generated; never hand-edit them.
- components/<PascalCaseName>.tsx defines a reusable Markdown component with familiar Preact-style JSX.
- examples/ are the theme's visual test cases.

Content stays readable:

    <MetricCard value="$4.2M">Up 18% year over year.</MetricCard>

The matching components/MetricCard.tsx can use:

    export const metadata = {
      description: "A headline metric with a short supporting caption.",
      usage: "Use for one key number; group related metrics with Columns."
    };

    export default function MetricCard({ props, children }) {
      return <figure className="grid grid-cols-[1fr_auto] gap-3 border-y border-stone-300 py-5">
        <strong className="text-4xl tracking-tight">{props.value}</strong>
        <figcaption className="col-span-2 text-sm">{children}</figcaption>
      </figure>;
    }

Each component receives props (tag attributes), children (its rendered body),
meta (the document's YAML front matter), and format (page, deck, or component
for an isolated preview). Ordinary TypeScript expressions can build
data-driven layouts and inline SVG charts. Components may contain other
components. Put presentation in Tailwind utilities and local assets; scripts, event
handlers, remote resources, and shell hooks are intentionally unavailable.

Keep at least one realistic and one stress-test example. Preview every example
after changing a component or shared style.
`
