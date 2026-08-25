# Stamp

Stamp turns Markdown, TSX components, and Tailwind themes into polished PDFs.
Projects are ordinary folders; Google Drive stores the shared revision; Studio
is a focused local editor.

## Install

Install with Homebrew, then let Stamp prepare the authoring tools and Google
Drive connection:

```sh
brew install jhleao/tap/stamp
stamp setup
```

Without Homebrew, use the signed release installer:

```sh
curl -fsSL https://raw.githubusercontent.com/jhleao/stamp/main/scripts/install.sh | sh
stamp setup
```

To build from a source checkout instead:

```sh
npm ci
make install
```

`make install` builds the embedded Studio and installs Stamp to
`$HOME/.local/bin`. Override `PREFIX` or `BINDIR` if needed.

## Update

Release builds used from an interactive terminal check GitHub for a newer
version at most once per day. The check has a short timeout, never blocks a
command after its cached result is fresh, and can be disabled with
`STAMP_NO_UPDATE_CHECK=1`.

```sh
stamp update --check       # check without changing anything
stamp update               # review and install the latest release
stamp update --yes         # install without the confirmation prompt
```

Homebrew installations stay owned by Homebrew; Stamp reports available updates
and directs those installations to `brew upgrade stamp`.

Stamp downloads the archive for the current operating system and architecture,
verifies it against the release's SHA-256 manifest, validates the embedded
version, and atomically replaces the current binary. Self-update is intended
for release binaries installed directly in a user-writable location such as
`$HOME/.local/bin`; source builds should be rebuilt with `make install`.

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
Studio's robot button copies a ready-to-paste agent prompt containing the
workspace path and the instruction to run `stamp skill`.

## Commands

```text
stamp login | logout
stamp setup
stamp new <dir> [--name <name>]
stamp clone [dir]
stamp pull [--incoming|--replace]
stamp push [--message <text>] [--force-with-lease <version>]
stamp studio [--dir <dir>] [--no-open]
stamp skill
stamp tutorial
stamp doctor
stamp update [--check|--yes]
stamp version
```

## Uninstall

```sh
stamp logout
brew uninstall stamp
rm -rf "$HOME/Library/Application Support/Stamp"
```

For fallback or source installations, remove `$HOME/.local/bin/stamp` instead.
Project folders and Drive data are never removed automatically.

## Reference

- [Getting started](docs/getting-started.md)
- [Google Drive](docs/drive.md)
- [Themes](docs/templates.md)
- [Studio](docs/studio.md)
- [Agents](docs/agents.md)
- [Uninstall](docs/uninstall.md)
