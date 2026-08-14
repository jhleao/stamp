# The small Stamp

## Product

Stamp helps a person or agent open a shared document project, edit ordinary
files locally, see the result, and push a recoverable version to Google Drive.

It produces:

- written documents as PDF;
- slide decks as PDF;
- spreadsheets as XLSX.

It runs on macOS and treats Google Drive as the collaboration surface.

## Vocabulary

- **Space:** an initialized folder in My Drive or a Shared Drive.
- **Project:** one canonical `.stamp` file plus visible current outputs.
- **Workspace:** the expanded local copy of a project.
- **Version:** a Google Drive revision of the canonical `.stamp` file.
- **Lease:** the canonical file's immutable Drive content revision ID.
- **Studio:** a local browser editor and preview, never a hosted service.

## Workflow

```text
stamp project open <drive-url>
stamp pull
stamp studio
stamp push --message "update the board pack"
```

There is no distinction between save and publish. Local edits are private;
every push is a complete, rendered, shared version.

## Project contents

```text
stamp.yaml
documents/       # *.page.md and *.doc.md -> PDF
decks/           # *.deck.md and *.fodp -> PDF
spreadsheets/    # *.fods -> XLSX; *.xlsx is preserved directly
theme/           # component sources, compiled theme assets, examples
assets/
outputs/         # generated and included in the canonical archive
```

The Drive folder contains the canonical archive and disposable output mirrors:

```text
Q3 Board Pack/
  Q3 Board Pack.stamp
  Current/
    Investment Memo.pdf
    Board Deck.pdf
    Operating Model.xlsx
```

Drive revisions are project versions. Stamp does not build a version graph,
lock server, database, release protocol, or per-file remote sync engine.
Canonical revisions are retained using Drive's `keepForever` flag, within
Drive's 200-revision limit for binary files.

## Conflict rules

The local workspace records the canonical Drive file ID, head revision, and hash.

- Pull replaces a clean workspace.
- Pull refuses when both the workspace and Drive changed.
- `pull --incoming` expands the remote version beside local work.
- `pull --replace` first moves local work to a recovery directory.
- Push refuses when the Drive version differs from the local lease.
- There is no unqualified force push.
- `push --force-with-lease <version>` proceeds only after observing that version.

Drive does not provide a documented project-level atomic compare-and-swap. The
lease is a strong warning, not a distributed lock. Drive revisions make the
narrow simultaneous-push race recoverable. We will not add a coordination
service unless real usage proves it necessary.

## Rendering

Use existing wheels:

- Markdown and bounded component templates for content composition;
- Tailwind as the theme authoring system, compiled to ordinary local CSS before
  a theme is packed or pushed;
- Chromium for HTML/CSS to PDF;
- LibreOffice for FODS to XLSX, FODP to PDF, and exact spreadsheet previews;
- Pandoc plus LibreOffice for DOC.MD compatibility.

Fast Studio previews may render HTML directly. Push always runs exact output
rendering. FODS preview is debounced and queued through LibreOffice; Stamp never
implements a spreadsheet layout or formula engine.

## Studio

`stamp studio` binds a tokenized server to `127.0.0.1`. Its Preact, Signals,
Vite, and Tailwind interface provides a file tree, a restrained Monaco editor,
preview, diagnostics, conflicts, pull, and push. Agents remain external and use
the ordinary CLI; Studio does not contain chat or collaboration primitives.

## Theme authoring

Content and presentation are different products inside one workspace:

```text
theme/
  page.html.tmpl       # page shell
  deck.html.tmpl       # deck shell
  components/          # shared, named content primitives
  examples/            # visual fixtures and stress tests
  tailwind.css         # authoring source: tokens and print primitives
  page.css             # generated Tailwind output; not hand-authored
  deck.css             # generated Tailwind output; not hand-authored
```

Template and component markup may use Tailwind utility classes. Stamp compiles
the utilities into a static local stylesheet and embeds that stylesheet in the
rendered artifact. Compilation is an authoring concern; previewing, sharing, and
opening an already-built project must not fetch a CDN or execute a template's
JavaScript.

Preact belongs in Studio. Executing arbitrary TSX inside the document renderer
would make templates code, weaken the security boundary, and require a
JavaScript runtime in the standalone Go binary. If JSX authoring is added, it
must compile into the same inert component bundle before the project is shared;
raw TSX is never the canonical collaboration format.

## Agent surface

`stamp skill` prints the canonical project instructions. Agents edit ordinary
files and call the same CLI as people; there is no parallel agent protocol.

## Complexity budget

Prefer the standard library and small, mature dependencies. One binary plus
the renderer applications is the distribution. A little duplication is better
than a framework. A direct function is better than a port, registry, provider,
or generalized protocol used once.

Do not build:

- a second general-purpose template programming language;
- automatic template upgrades;
- a generic pipeline or storage plugin system;
- automatic document or spreadsheet merging;
- deterministic archives or byte parity machinery;
- WYSIWYG layout editing;
- real-time multi-user editing;
- a hosted service;
- abstractions justified only by possible future backends.

The target is roughly 4,000-6,000 readable Go and browser lines. Less is better.
