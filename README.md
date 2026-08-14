# Stamp

Local-first document packs for people and coding agents. Edit ordinary files, render PDF/XLSX outputs, and share revisioned projects through Notion or Google Drive.

---

## What?

Stamp turns Markdown, TSX components, Tailwind styles, and office source files into finished document packs. A project is an ordinary folder. Notion or Google Drive stores its canonical revision and derived outputs.

The workflow is deliberately small:

```text
Pull → edit → preview → Push
```

Use the CLI, the local Studio, or the bundled MCP server. They all operate on the same files and collaboration rules. There is no Stamp cloud service or database.

## Requirements

- macOS
- Go 1.25+
- Node.js and npm
- Google Chrome
- Pandoc
- LibreOffice
- Tailwind CLI

## Install

Download the archive for your platform from [GitHub Releases](https://github.com/jhleao/steamp/releases), extract it, and place `stamp` on your `PATH`:

```sh
install -m 0755 stamp ~/.local/bin/stamp
stamp version
```

Release archives are published for macOS (Apple silicon and Intel), Linux
(ARM64 and AMD64), and Windows (AMD64). Verify a download against the attached
`checksums.txt` before installing it.

To build from source instead:

```sh
brew install go node pandoc libreoffice
brew install --cask google-chrome
npm install -g @tailwindcss/cli

git clone https://github.com/jhleao/steamp.git
cd steamp
npm install
make install
```

`make install` builds Studio and installs `stamp` at `/usr/local/bin/stamp`.
Release builds include the first-party Google Picker browser key. Maintainers
building Picker locally use `PICKER_API_KEY='<key>' make install`.

To remove Stamp, its local credentials, and optional project integrations, see [Uninstall](docs/uninstall.md).

## Start

Connect a project and open Studio:

```sh
stamp login
stamp space create 'Product Team'
stamp project create board-pack --name 'Board Pack' --space '<Space URL or ID>'
stamp studio --dir board-pack
```

Studio runs at `http://127.0.0.1:57183`. The same process exposes the project MCP server at `http://127.0.0.1:57183/mcp`.

Create and apply a reusable theme:

```sh
stamp template create editorial
stamp template preview editorial
stamp project create board-pack --name 'Board Pack' --template editorial --space '<Space URL or ID>'
```

## Google Drive

Sign in with Stamp's bundled Google application, then create a shared Space:

```sh
stamp doctor
stamp login
stamp space create 'Product Team'
```

To choose an existing Drive folder, run the CLI Picker flow:

```sh
stamp space pick --name 'Product Team'
```

Create, connect, and publish a project before opening Studio:

```sh
stamp project create board-pack --name 'Board Pack' --space '<Space URL or ID>'
stamp studio --dir board-pack
```

Organizations can install their own desktop OAuth client with `stamp google-oauth <desktop-client.json>`. See [Google Drive setup](docs/drive.md) for credential precedence, Picker, and Shared Drive details.

## Notion

Create a Notion integration, connect it to a page, then enter its token through Stamp's hidden prompt. The token is stored in macOS Keychain.

```sh
stamp notion login
stamp notion status
stamp notion project create --parent '<Notion page URL>' --dir board-pack
```

To create a private top-level test project, omit `--parent`. To join an existing project:

```sh
stamp notion project open '<Notion project URL>' --dir board-pack
stamp studio --dir board-pack
```

After connection, the ordinary `stamp status`, `stamp pull`, and `stamp push` commands detect the backend automatically. Notion pages hold collaboratively editable Markdown. A managed source archive on the project page preserves TSX, Tailwind, templates, fonts, assets, configuration, and agent instructions byte-for-byte. See [Notion backend](docs/notion-backend.md).

## Command cheat sheet

### Daily workflow

Command | Purpose
--- | ---
`stamp studio [--dir <directory>] [--no-open]` | Open the editor, file tree, previews, and collaboration controls
`stamp status [--dir <directory>] [--json]` | Compare the local project with its last known remote revision
`stamp pull [--dir <directory>]` | Pull when the backend is newer; refuse unsafe local/remote conflicts
`stamp pull --incoming [--dir <directory>]` | Download remote files beside local work for inspection
`stamp pull --replace [--dir <directory>]` | Save a recovery archive, then replace local files from Drive
`stamp preview [--dir <directory>]` | Render every source into `outputs/`
`stamp push [--dir <directory>] [--message <text>]` | Render and publish a new backend revision
`stamp push --force-with-lease <version>` | Replace a reviewed conflicting remote revision only if that exact lease still holds

### Projects and Spaces

Command | Purpose
--- | ---
`stamp project create <directory> [--name <name>] [--template <theme-directory>] [--space <id-or-url>]` | Create a project; `--space` connects and publishes it so Studio can open
`stamp project list` | List accessible Drive projects
`stamp project open <Drive URL or ID> [--dir <directory>]` | Check out a Drive project
`stamp project rename <name> [--dir <directory>]` | Rename the local and linked Drive project
`stamp project reconnect --space <id-or-url> [--dir <directory>]` | Preserve an old Drive link and publish a new app-authorized project copy
`stamp space list` | List marked Drive Spaces
`stamp space create <name>` | Create a Space in My Drive
`stamp space pick [--name <name>]` | Choose and authorize an existing Drive folder through Google Picker
`stamp space init <Drive folder URL or ID> [--name <name>]` | Mark an existing folder as a Space
`stamp space rename <Drive URL or ID> <name>` | Rename a Space without changing its ID

### Templates

Command | Purpose
--- | ---
`stamp template create <directory>` | Create a reusable theme with examples and components
`stamp template preview <directory>` | Render every theme example as a visual test

### Agents and diagnostics

Command | Purpose
--- | ---
`stamp agent setup [--dir <directory>]` | Add project MCP configuration and the Claude compatibility link
`stamp mcp serve` | Run the bundled MCP server over stdio
`stamp doctor` | Check credentials and rendering dependencies
`stamp version` | Print the installed version

### Authentication and archives

Command | Purpose
--- | ---
`stamp google-oauth status` | Show the active default or override without exposing its secret
`stamp google-oauth <desktop-client.json>` | Install an organizational OAuth override
`stamp google-oauth reset` | Remove the installed override and return to Stamp's default
`stamp login` | Sign in to Google Drive
`stamp logout` | Remove the stored Drive refresh token
`stamp notion login` | Store a Notion integration token securely in macOS Keychain
`stamp notion status` | Verify the current Notion integration
`stamp notion pages` | List Notion pages visible to the integration
`stamp notion logout` | Remove the Notion token from Keychain
`stamp notion project create --parent <URL> [--dir <directory>]` | Publish a local project beneath a Notion page
`stamp notion project open <URL> [--dir <directory>]` | Reconstruct a complete project using only Notion
`stamp pack <project-directory> <archive.stamp>` | Create a portable project archive
`stamp unpack <archive.stamp> <directory>` | Extract a project archive safely

## Project files

Path | Purpose | Output
--- | --- | ---
`documents/**/*.page.md` | Written pages | PDF
`documents/**/*.doc.md` | Written pages | PDF
`decks/**/*.deck.md` | Slide decks | PDF
`decks/**/*.fodp` | LibreOffice presentations | PDF
`spreadsheets/**/*.fods` | LibreOffice spreadsheets | XLSX
`spreadsheets/**/*.xlsx` | Existing spreadsheets | XLSX, unchanged
`theme/components/*.tsx` | Reusable Preact components | Used by page/deck sources
`theme/tailwind.css` | Theme entry point and Tailwind configuration | Compiled during rendering
`outputs/` | Generated files mirroring source folders | Never hand-edit
`stamp.yaml` | Project identity and rendering configuration | —
`AGENTS.md` | Canonical coding-agent instructions | —
`CLAUDE.md` | Symlink to `AGENTS.md` for Claude Code | —
`.mcp.json` | Project-scoped MCP connection | —

Folders remain hierarchical. `documents/clickup/phase-two.page.md` renders to `outputs/documents/clickup/phase-two.pdf`.

## Components and themes

Themes contain page/deck shells, Preact TSX components, Tailwind styles, local assets, and representative examples. Content remains readable Markdown:

```md
# Quarterly update

<MetricCard value="$4.2M">
Up 18% year over year.
</MetricCard>
```

Keep visual logic in components and Tailwind utilities. Keep document-specific words and data in Markdown. Run `stamp template preview <theme>` after changing shared presentation code.

See [Templates](docs/templates.md) for component props, preview fixtures, page/deck context, and rendering constraints.

## Agent collaboration

No separate Stamp skill is required. Run this once inside a project:

```sh
stamp agent setup
stamp studio
```

Stamp keeps `AGENTS.md` as the single instruction source, creates `CLAUDE.md → AGENTS.md`, and merges the Studio MCP endpoint into `.mcp.json`. Codex reads `AGENTS.md`; Claude Code reads the linked `CLAUDE.md` and asks once to approve the project MCP server.

Agents may either edit files and call the CLI, or use the twelve structured MCP tools for projects, templates, previews, Drive links, Pull, Push, status, and diagnostics.

The collaboration model is asynchronous, not live multi-cursor editing. Studio detects external changes but does not overwrite an active draft.

See [Agents](docs/agents.md) for setup and the recommended agent loop.

## Conflict safety

- Pull refuses when local and Drive both changed.
- `pull --incoming` preserves both versions for inspection.
- `pull --replace` saves the current workspace under `.stamp/recovery/` first.
- Push uses the pulled Drive content revision as a lease.
- A stale push fails instead of overwriting newer remote work.
- `--force-with-lease` succeeds only against the exact reviewed revision.

## Develop

```sh
npm install
make build
make test
make smoke
```

Command | Purpose
--- | ---
`make frontend` | Build the Preact/Vite/Tailwind Studio bundle
`make build` | Build Studio and `bin/stamp`
`make test` | Run Studio and Go tests
`make smoke` | Build and exercise the end-to-end workflow
`make stress` | Render the generic stress project
`make install` | Install the binary to `/usr/local/bin/stamp`

### Releases

Every push to `main` runs the full test suite and semantic-release. Commit
messages determine the next version:

- `fix:` publishes a patch.
- `feat:` publishes a minor release.
- `BREAKING CHANGE:` or `type!:` publishes a major release.
- `docs:`, `test:`, `chore:`, and other non-product changes do not publish.

The release workflow updates `CHANGELOG.md`, creates a `vX.Y.Z` tag and GitHub
Release, and attaches macOS, Linux, and Windows archives plus SHA-256 checksums.

## Documentation

- [Getting started](docs/getting-started.md)
- [Studio](docs/studio.md)
- [Templates](docs/templates.md)
- [Agents and MCP](docs/agents.md)
- [Google Drive](docs/drive.md)
- [Notion backend plan](docs/notion-backend.md)
- [Google rollout checklist](GOOGLE_ROLLOUT.md)
- [Uninstall](docs/uninstall.md)
- [Tutorial](docs/tutorial/README.md)
- [Design constraints](DESIGN.md)
