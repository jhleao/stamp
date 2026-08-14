# Uninstall Stamp

Uninstalling Stamp does not delete projects or anything stored in Google Drive.

## 1. Stop Studio

Return to the terminal running Studio and press `Control-C`. If it is running elsewhere, find the process before stopping it:

```sh
lsof -nP -iTCP:57183 -sTCP:LISTEN
```

## 2. Disconnect Google Drive

Remove Stamp's refresh token from macOS Keychain while the OAuth configuration is still installed:

```sh
stamp logout
```

Then remove any installed organization OAuth override and its now-empty directory:

```sh
rm "$HOME/Library/Application Support/Stamp/google-oauth.json"
rmdir "$HOME/Library/Application Support/Stamp" 2>/dev/null || true
```

The bundled first-party client is part of the binary and disappears with it. If `STAMP_GOOGLE_OAUTH_CONFIG` points somewhere else, remove that file instead if it is dedicated to Stamp. Stamp does not copy or manage files at a custom path.

For defense in depth, you may also revoke Stamp under Google Account → Security → Third-party apps and services. This invalidates any token that remains outside the current Mac.

## 3. Remove the binary

The source installation places one binary in `/usr/local/bin`:

```sh
sudo rm /usr/local/bin/stamp
```

Confirm that it is gone:

```sh
command -v stamp || echo 'Stamp is not installed'
```

If `command -v stamp` reports another path, remove the binary using the package manager or installation method that owns that path.

## 4. Remove project agent integration (optional)

`stamp agent setup` adds a `stamp` entry to each project's `.mcp.json` and creates `CLAUDE.md` as a symlink to `AGENTS.md`. These files are project-local and are not removed automatically.

For each project you no longer want connected:

1. Remove only the `mcpServers.stamp` object from `.mcp.json`. Preserve other MCP servers. Delete `.mcp.json` only if it is otherwise empty.
2. Remove `CLAUDE.md` only if it is still a symlink to `AGENTS.md`.
3. Keep `AGENTS.md` if its project instructions are still useful.

Check the link before removing it:

```sh
test "$(readlink CLAUDE.md)" = 'AGENTS.md' && rm CLAUDE.md
```

Codex and Claude Code may cache an approved project MCP server. Removing the project entry prevents it from being registered the next time that project opens.

## 5. Remove source and dependencies (optional)

Delete the cloned Stamp repository only if it contains no work you need. Stamp projects normally live elsewhere and are not part of the repository.

The prerequisites are installed independently. Keep them if other tools use them. If Stamp was their only consumer:

```sh
npm uninstall -g @tailwindcss/cli
brew uninstall pandoc libreoffice
brew uninstall --cask google-chrome
```

Do not remove Go or Node.js unless you are sure no other development tools need them.

## What remains

- Local Stamp project folders and generated outputs
- Google Drive Spaces, projects, revisions, and mirrored outputs
- Project `AGENTS.md` files
- Chrome, LibreOffice, Pandoc, Go, Node.js, and Tailwind unless explicitly removed

Remove project folders or Drive content separately only when you intend to delete that work.
