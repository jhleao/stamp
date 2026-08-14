# Studio

Studio is a local Preact editing and preview surface for a Stamp workspace:

```sh
cd board-pack
stamp studio
```

Studio intentionally refuses local-only projects. Connect the project first
with `stamp project create ... --space <space>` or an initial `stamp push
--space <space>`. Opening an existing Drive project is already connected. This
keeps account selection, Picker authorization, and first publication in the CLI.

It opens a capability URL on `127.0.0.1:57183`. The same process exposes a
project-bound Streamable HTTP MCP endpoint at `http://127.0.0.1:57183/mcp`.
The project never leaves the Mac
unless the user presses Push. Edits made by an agent or another local editor
appear automatically when the editor is idle. Studio never replaces an active
draft; it shows **File changed** and lets the user reload it deliberately.

The left side separates **Content** from **Templates**. Content contains the
words and data people collaborate on. Templates contain clearly grouped Page
and Deck structures, a Tailwind design system, reusable Components, and
Examples. Generated CSS stays hidden. Selecting a template previews its matching
example, and the `+` beside Components creates a new shared primitive.

## Preview behavior

- Typing changes only the in-memory draft. Save with **Save** or Command/Ctrl+S.
- Editing pauses and clears the old preview. **Save & preview** writes the draft
  and runs the renderer; after saving, **Refresh preview** only reruns rendering.
- Page and deck Markdown use the same generated PDF in Studio and in exports.
- DOC.MD, FODP, FODS, and XLSX render to a cached PDF through the compatibility
  tools.
- FODS and XLSX conversions are serialized while LibreOffice works.
- A push always performs the exact PDF/XLSX build before touching Drive.

Template examples live under `theme/examples/` and behave exactly like project
documents. Refreshing an HTML template, component, or Tailwind theme renders its
matching example. Selecting a component renders that component by itself.

Studio uses a restrained Monaco editor with TSX, HTML, CSS, JSON, and Markdown
language support. Components are familiar Preact-style TSX with `props`,
`children`, `meta`, and `format`. Templates get Tailwind utility completion inside
class attributes, built-in formatting, and normal editor keyboard shortcuts.
The minimap and developer-heavy chrome stay hidden. Vite and
Tailwind are build-time authoring tools; Go embeds the compiled interface.

Stamp recompiles `tailwind.css` whenever theme sources change, whether the
change came from Studio, a coding agent, or the CLI preview flow. Generated
`page.css` and `deck.css` remain hidden and should never be hand-edited.

Studio is not a WYSIWYG layout tool, AI chat client, or cloud collaboration
server. Its local MCP endpoint lets an agent operate the current workspace;
Pull and Push remain the collaboration boundary.
