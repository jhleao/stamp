# Stamp

Stamp turns Markdown, TSX components, and Tailwind themes into polished PDFs.
Projects are ordinary folders; Google Drive stores the shared revision; Studio
is a focused local editor.

## Install

Download the archive for your platform from GitHub Releases, extract it, and put
`stamp` on `PATH`. On macOS:

```sh
xattr -d com.apple.quarantine stamp 2>/dev/null || true
install -m 755 stamp /usr/local/bin/stamp
stamp doctor
stamp login
```

## Start

```sh
stamp new quarterly --name "Quarterly update"
cd quarterly
stamp studio
```

Open an existing project instead:

```sh
stamp clone quarterly
# Choose the shared project in Google Drive.
cd quarterly && stamp studio
```

## Daily loop

```sh
stamp pull
# Edit and inspect in Studio
stamp push --message "Update quarterly summary"
```

Every project contains its own complete `theme/`: TSX components, Tailwind,
wrapper templates, assets, and visual examples. Edit it in Studio's Templates
section; there is no separate theme installation or lifecycle.

## Agents

No MCP registration or separate skill installation is required:

```sh
stamp skill
```

New projects contain the same guide in `AGENTS.md` and a `CLAUDE.md` symlink for
Claude Code. Agents edit ordinary files and use the CLI like everyone else.

## Commands

```text
stamp login | logout
stamp new <dir> [--name <name>]
stamp clone [dir]
stamp pull [--incoming|--replace]
stamp push [--message <text>] [--force-with-lease <version>]
stamp studio [--dir <dir>] [--no-open]
stamp skill
stamp tutorial
stamp doctor
stamp version
```

## Uninstall

```sh
rm /usr/local/bin/stamp
rm -rf "$HOME/Library/Application Support/Stamp"
```

Project folders and Drive data are never removed automatically.

## Reference

- [Getting started](docs/getting-started.md)
- [Google Drive](docs/drive.md)
- [Themes](docs/templates.md)
- [Studio](docs/studio.md)
- [Agents](docs/agents.md)
- [Uninstall](docs/uninstall.md)
