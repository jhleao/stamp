# The small Stamp

## Product

Stamp lets a person or coding agent clone a shared document project, edit
ordinary local files, inspect exact output, and push a recoverable version to
Google Drive. It produces written and slide PDFs plus XLSX workbooks.

Stamp is a local macOS application. Google Drive supplies sharing and revision
history; Stamp has no hosted document service.

## Vocabulary

- **Project:** the complete content, theme, assets, and generated outputs for
  one body of work.
- **Workspace:** a project's expanded local working copy.
- **Version:** one complete pushed source archive and its rendered mirrors.
- **Lease:** the immutable Drive content version observed by the workspace.
- **Studio:** the local browser editor and preview surface.

There is deliberately no separate space, repository, or theme lifecycle.

## Workflow

```text
stamp clone board-pack
# Choose the shared project in Google Picker.
cd board-pack
stamp pull
stamp studio
stamp push --message "Update the board pack"
```

Local saves remain private. Push renders every supported source, then shares
one complete version.

## Project contents

```text
stamp.yaml
documents/       # *.page.md and *.doc.md -> PDF
decks/           # *.deck.md and *.fodp -> PDF
spreadsheets/    # *.fods -> XLSX; *.xlsx is preserved directly
theme/           # TSX components, Tailwind, shells, examples, fonts
assets/
outputs/         # generated locally; never hand-edit
AGENTS.md
CLAUDE.md         # symlink to AGENTS.md
```

The Drive project contains a canonical source archive and disposable output
mirrors:

```text
Q3 Board Pack/
  Q3 Board Pack.stamp
  Current/
    Investment Memo.pdf
    Board Deck.pdf
    Operating Model.xlsx
```

Generated outputs are uploaded separately from the source archive so rendering
does not dirty the source lease. Deleted local sources remove their obsolete
mirrors on the next Push.

## Conflict rules

The workspace records the canonical archive ID, observed Drive version, source
hash, output IDs, and project-folder IDs in `.stamp/state.json`.

- Pull replaces a clean workspace.
- Pull refuses when local and Drive sources both changed.
- `pull --incoming` expands Drive's version under `.stamp/incoming/`.
- `pull --replace` first saves the local project under `.stamp/recovery/`.
- Push refuses when Drive moved beyond the observed lease.
- `push --force-with-lease <version>` works only after explicit review of that
  exact remote version.

Drive does not offer a project-level atomic compare-and-swap. The lease catches
normal drift rather than pretending to be a distributed lock; Drive revisions
keep a narrow simultaneous-push race recoverable.

## Rendering

- Markdown is expanded through bounded TSX components.
- Tailwind compiles the theme into ordinary local CSS.
- Chromium renders HTML/CSS to PDF.
- LibreOffice renders FODS, FODP, and spreadsheet previews.
- Pandoc plus LibreOffice handles DOC.MD compatibility.

TSX components execute in a restricted JavaScript runtime with no DOM, files,
network, events, remote modules, or shell access. Their output is inert HTML
styled by local CSS. Push always runs the complete renderer before modifying
Drive; Studio uses those same generated PDFs for page and deck previews.

## Studio

`stamp studio` binds a tokenized server to `127.0.0.1:57183`. Its Preact,
Signals, Vite, Tailwind, and Monaco interface provides a hierarchical file tree,
source editor, preview, diagnostics, Pull, and Push. It watches external file
changes without replacing an active draft. Agents remain external and use the
ordinary filesystem and CLI.

## Theme authoring

Every project owns one complete theme:

```text
theme/
  page.html.tmpl
  deck.html.tmpl
  components/*.tsx
  examples/
  tailwind.css
  page.css             # generated
  deck.css             # generated
  assets/
  fonts/
```

Markdown carries words and semantic component tags. TSX components carry
reusable structure and Tailwind utilities. Components may use props, children,
front matter, format-aware variants, nested components, arrays, conditions, and
inline SVG. Component metadata tells people and agents when a primitive should
be used. There is no standalone theme command, package, registry, or upgrade
protocol.

## Agent surface

`stamp skill` prints the canonical project instructions and the current
component catalog. New and cloned workspaces contain `AGENTS.md`; `CLAUDE.md`
points to it. Agents use the same Pull, edit, inspect, and Push flow as people.
There is no MCP server or parallel agent API.

## Complexity budget

Prefer the standard library and small, mature dependencies. Keep the CLI
Git-like: `new`, `clone`, `pull`, `push`, `studio`. A direct function is better
than a provider framework used once.

Do not build speculative theme distribution, storage plugins, WYSIWYG layout,
automatic document merging, real-time multi-cursor editing, or a hosted service.
