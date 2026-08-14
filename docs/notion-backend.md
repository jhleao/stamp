# Notion backend plan

Status: experimental implementation. Notion and Google Drive are both available backends.

## Product boundary

Notion should be a complete alternative collaboration backend. A new machine
must be able to reconstruct the whole Stamp project from Notion alone. The
local workspace is a checkout and render environment, not the only copy of any
project source.

- Notion pages are the collaborative source for document content.
- Notion file attachments store byte-exact TSX components, Tailwind, templates,
  fonts, images, configuration, and other non-page source.
- Stamp pulls Notion pages into readable Markdown and pushes deliberate local
  content changes back into Notion.
- Stamp renders locally and attaches the resulting PDF or XLSX to the matching
  Notion document page.
- Studio remains the place to author themes, components, and complex layouts.

This is a typed filesystem encoded in Notion: content uses native collaborative
pages; files that Notion cannot faithfully edit use attached archives. Nothing
requires Google Drive or another remote store.

## Authoring workspace

A user connects one top-level Notion page. Stamp deliberately presents a small,
human-facing workspace and moves implementation details out of the way:

```text
Project name
├── 📝 Documents                       editable pages
│   └── Client / project folders       mirrors the local hierarchy
├── 📊 Presentations                   editable slide narratives
├── 📥 Exports                         latest finished files
└── ⚙️ Project settings                Stamp-managed source package
```

The project landing page contains only these four destinations. Documents do
not display filesystem paths, hashes, component syntax, or synchronization
metadata. Presentation pages use ordinary headings and dividers; Stamp restores
the structural slide wrappers in the local checkout. Theme examples are test
artifacts and are not uploaded into Exports.

Each document is an ordinary nested Notion page. Its position beneath Documents
or Presentations reconstructs the local folder path; its title becomes the file
name. Stable Notion page IDs remain in the local lease state and never appear in
the authoring UI.

### Project source archive

`Project settings` holds the active `project-source-vN.zip` attachment. Its
managed caption carries the schema revision and integrity hashes used by Stamp.

The archive contains everything that must remain byte-exact:

```text
stamp.yaml
theme/**
assets/**
AGENTS.md
CLAUDE.md
.mcp.json
spreadsheets/**                 when the source is not Notion-native
documents/** and decks/**       recovery snapshots of Notion-native content
```

Document snapshots make an archive independently inspectable and recoverable,
but the live Notion pages are authoritative when their leases are newer.

One archive is preferable to turning every TSX line, font, and image into a
Notion page: it preserves bytes and hierarchy atomically, uses far fewer API
requests, and keeps implementation files out of the human editing surface.

## Encoding decision

Three encodings are possible:

Approach | Strength | Failure mode
--- | --- | ---
One archive containing everything | Simple and byte-exact | Notion edits are invisible inside the archive
One Notion record per file | Visible hierarchy | Poor editing for code/binary data, many API calls, weak atomicity
Native documents plus source archive | Collaborative content and exact project recovery | Requires an explicit merge contract

Use the third approach. It gives each kind of data one authoritative
representation:

- a document's words and supported structure are authoritative in its Notion
  page;
- theme code, templates, configuration, and binary assets are authoritative in
  the active source archive;
- PDFs and XLSX files are derived outputs;
- document copies inside the archive are recovery snapshots, never a second
  live authority.

This distinction is what prevents ambiguous two-way reconciliation while still
making Notion a complete backend.

## Round-trip contract

The first release should support a deliberately small, lossless subset:

- headings, paragraphs, lists, quotes, dividers, code, links, and simple tables;
- images and files that Stamp can download into `assets/`;
- a small set of Stamp component blocks represented by synced-block-like
  callouts or fenced code with explicit component data;
- page properties mapped to Markdown front matter.

Unsupported Notion blocks must never disappear silently. Pull should preserve
them as an explicit opaque block or stop with a precise diagnostic. Push should
update only content Stamp owns and understands.

Decks and highly composed documents may use Notion for copy while keeping
layout directives in front matter or stable Stamp component blocks. Arbitrary
TSX is edited locally but stored in the Notion source archive.

## Storage abstraction

The current collaboration package exposes a Drive-shaped file API. Do not make
Notion implement fake folders, revisions, and files. Introduce a backend at the
product-operation level instead:

```go
type Backend interface {
    Kind() string
    Connect(ctx context.Context, target string) (RemoteProject, error)
    Open(ctx context.Context, ref string, destination string) (RemoteState, error)
    Status(ctx context.Context, root string) (SyncStatus, error)
    Pull(ctx context.Context, root string, mode PullMode) (PullResult, error)
    Push(ctx context.Context, root string, request PushRequest) (RemoteState, error)
    Link(ctx context.Context, root string) (string, error)
}
```

`DriveBackend` keeps the existing archive-and-revision behavior behind this
interface. `NotionBackend` owns page traversal, Markdown conversion, uploads,
and Notion-specific leases. Rendering and local recovery remain shared
services called by the orchestration layer.

Project state becomes backend-neutral and stores provider details separately:

```yaml
backend: notion
remote:
  project_id: <notion page id>
  url: <notion url>
```

Provider-only cursors, page IDs, and edit timestamps live in `.stamp/state.json`
and are not exposed throughout Studio or the renderer.

## Conflict model

Notion does not provide Google Drive-style immutable content revisions for this
workflow. Use two kinds of optimistic lease:

- store the pulled page ID, `last_edited_time`, and normalized content hash;
- before pushing a changed local document, retrieve the page again;
- refuse if the remote timestamp/hash changed since Pull;
- allow unchanged documents to proceed independently;
- lease the source archive by its revision, attachment ID, and SHA-256 hash;
- refuse a theme/source push when that archive lease changed;
- offer incoming and reviewed replacement flows using the same language as the
  Drive backend;
- keep a local recovery archive before any destructive replacement.

Do not use the top-level project page timestamp as the lease: unrelated Notion
activity would create false conflicts.

Notion has no multi-object transaction. A push therefore stages file uploads
first, validates every lease, updates changed content, and changes the active
source attachment/revision last. If a late step fails, the previous source
revision remains active and the operation reports exactly what needs retrying.
Outputs are derived and never participate in the source lease.

## Authentication

### First implementation: user-supplied internal integration token

Notion calls this an internal integration. It is the closest supported option
to a personal API key: a workspace owner creates an integration, copies its
static token, and explicitly connects the chosen Stamp page to it.

Implemented CLI:

```sh
stamp notion login
stamp notion project create --parent '<Notion page URL>' --dir ./project
stamp notion project open '<Notion project URL>' --dir ./checkout
stamp status --dir ./checkout
stamp notion logout
```

`stamp notion login` accepts the token through a hidden prompt and stores it in
macOS Keychain. Never put it in `stamp.yaml`, shell history, or a shared file.
This path needs no Stamp-owned client ID, hosted callback, or global secret.

Limitations: the person creating the internal integration generally needs to
own the workspace, and the token represents the integration rather than each
collaborator's individual identity. A team may share access to the same
integration-connected pages, but should not send the token in chat.

### Later default: public Notion integration with OAuth

For general distribution, create one public Stamp integration. Each user
installs it into their workspace and receives a workspace-scoped access token.
This is the friendlier onboarding path, but it requires a Stamp client ID and
secret plus a stable HTTPS redirect service to exchange authorization codes.
That small hosted broker should return credentials to the local CLI without
ever serving or storing project content.

Do not block the backend prototype on public OAuth. Keep authentication behind
a token source so the internal-token and public-OAuth flows use the same API
client.

## Notion CLI

Notion now documents an `ntn` CLI, including file uploads. It is useful for
manual validation, but Stamp should call the versioned REST API directly from
Go. Making `ntn` a runtime dependency would add installation and output-format
coupling without removing the need for Stamp's mapping and conflict logic.

## Implemented behavior

The experimental backend currently provides:

- Keychain-backed internal integration authentication;
- workspace-private or parented Notion project creation;
- native Notion Markdown pages for documents and decks;
- a complete attached source archive for TSX, Tailwind, templates, assets,
  configuration, and agent instructions;
- PDF/XLSX output attachments;
- per-document and source-revision conflict leases;
- safe Pull, replacement Pull with recovery, Push, status, links, Studio, and MCP;
- clean reconstruction and rendering on a second checkout using Notion alone.

## Remaining rollout work

- public OAuth and its small callback broker;
- richer Notion data-source properties and output placement;
- a first-party Drive-to-Notion migration command;
- production retry/backoff and large-file soak tests;
- a published compatibility policy for unsupported Notion Markdown constructs.

### Phase 4 — public OAuth

- create the public Stamp integration and minimal callback broker;
- implement browser authorization and Keychain token storage;
- document workspace installation and admin approval;
- retain internal-token login as the advanced/self-hosted path.

## Explicit non-goals for the first release

- editing TSX components or Tailwind in Notion;
- reproducing every Notion block type;
- live multi-cursor synchronization into the local filesystem;
- representing every binary or implementation file as editable Notion blocks;
- silently flattening unsupported content;
- requiring the Notion CLI at runtime.

## Technical constraints to test early

- Notion averages about three API requests per second per integration and
  requires retry handling for HTTP 429.
- API payloads have block-count and total-size limits, so traversal and updates
  need pagination and batching.
- file uploads are a lifecycle: create, send, complete when multipart, then
  attach; unattached uploads expire.
- Notion's block model is not Markdown, so conversion correctness is a product
  boundary, not a serialization detail.

## Official references

- [Authorization](https://developers.notion.com/guides/get-started/authorization)
- [Public integrations and OAuth](https://developers.notion.com/guides/get-started/public-connections)
- [Request and payload limits](https://developers.notion.com/reference/request-limits)
- [Files and media](https://developers.notion.com/guides/data-apis/working-with-files-and-media)
- [Notion CLI file uploads](https://developers.notion.com/cli/guides/file-uploads)
