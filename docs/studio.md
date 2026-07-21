# Studio

Studio is a local editing and preview surface for a Stamp workspace:

```sh
cd board-pack
stamp studio
```

It opens a capability URL on `127.0.0.1`. The project never leaves the Mac
unless the user presses Push. Edits made by an agent or another local editor
appear automatically.

## Preview behavior

- Page and deck Markdown render directly to HTML while editing.
- DOC.MD, FODP, FODS, and XLSX render to a cached PDF through the compatibility
  tools.
- FODS and XLSX conversions are serialized; the previous preview stays visible
  while LibreOffice works.
- A push always performs the exact PDF/XLSX build before touching Drive.

Template examples live under `theme/examples/` and behave exactly like project
documents. Editing the Go HTML templates or CSS refreshes the last selected
example.

Studio deliberately uses a plain textarea. It is enough for agent-assisted
editing and keeps the executable free of a frontend package tree. A richer
editor can be added later if real use demonstrates that selection, search, or
syntax highlighting is worth its footprint.

Studio is not a WYSIWYG layout tool, AI chat client, or collaboration server.
Pull and Push are the collaboration boundary.

