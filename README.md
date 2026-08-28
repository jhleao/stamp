<p align="center"><img src="website/assets/stamp-logo-pixel.png" width="220" alt="Stamp logo"></p>

# Stamp

Stamp turns Markdown, TSX components, and Tailwind themes into visually in-brand documents.
Projects are ordinary folders; Google Drive stores the shared revision; Studio
is a focused local editor.

<p align="center"><img src="website/assets/studio-document.png" width="920" alt="Stamp Studio editing a branded document with its live preview"></p>

## Quick start

```sh
brew install jhleao/tap/stamp
stamp setup
stamp new quarterly --name "Quarterly update"
cd quarterly
stamp studio
```

In Studio, click **Copy Agent Instructions** in the top-left. Paste the prompt
into your agent of choice, then work away.

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

## Commands

```text
stamp login | logout
stamp setup
stamp new <dir> [--name <name>] [--choose-drive-folder]
stamp clone [dir]
stamp remote [set [--dir <dir>] [--yes]]
stamp pull [--incoming|--replace]
stamp push [--message <text>] [--force-with-lease <version>]
stamp studio [--dir <dir>] [--no-open]
stamp skill
stamp tutorial
stamp doctor
stamp update [--check|--yes]
stamp version
```

## Diagnostics

Add `--verbose` before any command to print sanitized production diagnostics
to stderr. The log covers CLI lifecycle, Google OAuth and Picker, every Google
Drive HTTP request and response, collaboration decisions, rendering, output
mirroring, Studio startup, and updater traffic.

```sh
stamp --verbose clone 2>&1 | tee stamp-debug.log
stamp --verbose pull 2>&1 | tee stamp-debug.log
stamp --verbose push --message "Ready for review" 2>&1 | tee stamp-debug.log
```

`STAMP_VERBOSE=1` enables the same diagnostics. Authorization headers, OAuth
codes and tokens, API keys, URL query strings, and file contents are omitted or
redacted. Review filenames and Drive IDs before sharing a log outside your
team.

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
