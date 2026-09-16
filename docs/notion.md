# Notion

Stamp can use a Notion page as its remote. The local project, `.stamp` archive,
PDF/XLSX rendering, Studio, clone, pull, and push workflows remain the same.
Google Drive remains the default for existing and new projects unless selected
explicitly.

## Connect

Run the guided setup once:

```sh
stamp setup notion
```

It installs missing rendering tools, connects Notion, guides you through creating
or cloning a project, and offers to launch Studio. To authenticate separately or
replace an expired token:

```sh
stamp login notion
```

Create a [personal access token](https://www.notion.so/developers/tokens) with
**Notion API** enabled for the project's workspace, then paste it into the hidden
terminal prompt. Stamp validates the token before replacing the saved credential
in macOS Keychain. Future shells and Studio sessions load it automatically.
No shell-profile export is needed. One token is saved per macOS user; logging in
again replaces it, allowing you to switch workspaces or accounts.

`stamp logout notion` removes the saved token locally; revoke the token in
Notion's developer portal when required. `STAMP_NOTION_TOKEN` overrides Keychain
for automation and non-macOS use. An invalid environment override is reported,
not silently replaced by a saved token. Unset it to use Keychain. Never put a
token in `stamp.yaml`, project files, or a command-line argument.

Each collaborator uses their own token and needs edit access to the entire
project page subtree. PATs follow the creator's page permissions and do not
require the integration-bot “Add connections” flow. They belong to one workspace
and stop working when expired, revoked, or disallowed by workspace policy.
Guests and restricted members cannot create PATs. On Business workspaces,
owners must enable member PAT creation if teammates need it. This makes PATs
appropriate for an opt-in CLI workflow, but not a universal replacement for
OAuth onboarding. See [Notion's PAT documentation](https://developers.notion.com/guides/get-started/personal-access-tokens).

```sh
stamp new board-pack --backend notion --notion-page '<parent page URL>'
# Omit --notion-page to create a new page in your Private section.

stamp clone board-pack-copy --backend notion --notion-page '<Stamp project URL>'
stamp push --dir board-pack --message 'Update board pack'
stamp pull --dir board-pack-copy
stamp studio --dir board-pack

stamp remote set --backend notion --notion-page '<same project, another remote>'
```

The destination supplied to `new` is a parent page; Stamp creates a dedicated
project beneath it. The destination supplied to `clone` or `remote set` is the
Stamp project page itself. `remote set` verifies project identity and does not
change source files. Ordinary push and pull choose the provider saved in local
`.stamp/state.json`; missing provider means Google Drive.

## Move an existing workspace to Notion

```sh
stamp remote create --backend notion --notion-page '<parent page URL>'
```

This renders and uploads a new Notion copy with the same project identity, then
connects the local workspace to it. It stages the upload separately: failure
preserves the original connection and local files. The previous remote remains
available and unchanged. To connect back to the original Drive copy, use
`stamp remote set --backend drive` and select its archive. This is a one-remote
workflow, not automatic two-way replication between Drive and Notion.

## Notion layout

```
Board Pack
  Board Pack.stamp             current archive attachment
  Published files/
    documents/
      overview.pdf            page containing an uploaded PDF block
    spreadsheets/
      budget.xlsx             page containing a downloadable file block
  Version history/            retained archive attachments with parent revisions
  Stamp project metadata      IDs of managed root pages
```

Notion's upload API rejects the `.stamp` extension. Stamp uploads the ZIP bytes
as `Board Pack.stamp.zip` and uses `Board Pack.stamp` as the visible attachment
name. Cloning uses the hosted bytes directly. No source Markdown is converted
into Notion blocks.

Existing output pages and attachment blocks are updated in place. Their links
remain stable when the output path is unchanged. Renaming or moving a local
output currently archives its old page and creates a page at its new path.
Removed output pages are moved to Notion trash. Notes on a retained output page
are preserved. Content on a removed output page stays in that archived page.

Only pages carrying Stamp's ownership markers and attachments carrying its
path captions participate in reconciliation. Do not edit the metadata, revision details, ownership markers, or managed
attachment captions manually.

## Collaboration and recovery

Notion has no documented conditional-write transaction for these APIs. A
read-before-write version check cannot implement a reliable distributed lock.
Stamp therefore retains immutable archive attachments in an append-only
revision graph. A revision's identity is the SHA-256 of its archive; its parents
are the remote heads observed by the writer. Independent concurrent children
remain visible as a conflict instead of silently losing one archive.

A stale push is refused. A concurrent push detected after the archive was
accepted reports that the archive was retained. Clone and pull refuse ambiguous
heads. Studio reports this conflict; resolve it through the CLI. Review/download both archives in `Version history`, reconcile them
locally, and use the exact conflict lease printed by Stamp:

```sh
stamp push --force-with-lease '<reviewed conflict lease>'
```

That new revision records all reviewed heads as parents. A changed lease is
refused. The current attachment and rendered pages are derived views, updated
after retaining the archive. They are not an atomic multi-page transaction:
concurrent publishing or a failed request can leave them temporarily mixed.
Stamp reports failures and verifies the head after publishing. Repair by
reviewing the remote and pushing with the current lease. Strict serialization
would require an external coordination service or a future Notion API primitive.

`pull --incoming` expands remote content beside dirty local work.
`pull --replace` saves a recovery `.stamp` archive before replacing dirty local
files. Archive contents are hash-verified and unpacked using Stamp's existing
path, symlink, and size protections.

## API constraints

- API version: `2026-03-11`.
- Upload limit comes from `users/me`; small files use a single upload and files
  over 20 MiB use multipart upload. Archive unpacking retains Stamp's existing
  256 MiB per-entry / 512 MiB total safety limits.
- Download URLs expire. Stamp requests a fresh URL for each download and sends
  no API authorization header to the file host.
- List operations paginate fully; partial listings are errors. The shared output
  catalog is limited to 100 rich-text chunks (180,000 Unicode characters).
- Explicit 429 and 529 responses honor `Retry-After`. Ambiguous failed mutations
  are not automatically retried, because they may already have succeeded.
- HTTP 404 means absent **or inaccessible**; it is not permission to recreate
  a known remote object. Revoked tokens and permission failures stop the operation.

Sources: [file uploads](https://developers.notion.com/guides/data-apis/uploading-small-files),
[multipart uploads](https://developers.notion.com/guides/data-apis/sending-larger-files),
[update blocks](https://developers.notion.com/reference/update-a-block),
[private page creation](https://developers.notion.com/reference/post-page),
[request limits](https://developers.notion.com/reference/request-limits).

## Workflow parity

| Workflow | Google Drive | Notion |
| --- | --- | --- |
| Sign in | OAuth / `stamp login` | `stamp login notion` / macOS Keychain |
| Choose destination | Google Picker | `--notion-page` URL or private root |
| New / clone | Existing commands | Same commands with `--backend notion` |
| Pull / incoming / replace | Version check and local recovery | Hash-verified revision head and local recovery |
| Push / force lease | Drive version lease | Retained revision graph and explicit conflict lease |
| Studio status, review, push, pull | Drive remote | Provider selected from workspace state |
| Move an existing workspace | Original remote kept | `remote create --backend notion`, then reconnect |
| Reconnect | Same project identity required | Same identity check, `remote set --backend notion` |
| Share | Drive folder permissions | Share project page; each member uses their own PAT |
| Existing output links | Update existing file IDs | Update existing page and attachment IDs |
| Rendered spreadsheets | Drive file | Downloadable Notion file attachment |
| History | Retained Drive revisions | Visible retained archive attachments |
| Concurrent rendered updates | Multiple remote writes | Multiple remote writes; mixed views possible |

The output catalog stores known folder, page, and attachment IDs independently
of display names. Clone, pull, and push verify catalog references. This catches
missing, inaccessible, moved, or altered known attachments before reconciliation.
Read access cannot prove write access; a later 403 still stops publishing and
may require a repair push after permissions are corrected.

## Verification

Normal checks do not use a Notion account:

```sh
go test ./...
npm run test:studio
npm run build:studio
```

An opt-in live test creates and archives its own child beneath a dedicated
parent, covering real upload/download, clone, stable archive identity, stale
writers, retained concurrent heads, and force-lease resolution:

```sh
# Supply STAMP_NOTION_TOKEN through your secret manager first.
STAMP_NOTION_TEST_PARENT='<dedicated test parent ID>' \
  go test ./internal/collab -run TestLiveNotionRoundTrip -count=1 -v -timeout 16m
```

### Implementation verification (2026-09-15)

Verified against a private Notion test subtree using a personal token:

- Real `.stamp.zip` upload, download, hash verification, and local clone.
- Repeated PDF publication with unchanged page/block IDs and preserved user notes.
- Stale writer rejection, normal pull, dirty pull refusal, incoming expansion,
  and replace with a local recovery archive.
- Two retained concurrent archive heads, refusal to select one implicitly, and
  explicit force-lease resolution through the production implementation.
- Missing managed page refusal, followed by restoring the test page.
- Nested output creation and removal, including archiving an empty managed folder.
- Migration failure preserving the original connection, successful copy,
  reconnecting the same project, and refusing a different project identity.
- Studio's Notion status, review dialog, push, and return to “Up to date”.

The live automated test archives its own project; one separate private demo was
retained for inspection. Separate users' guest/member/admin policy combinations
were researched in Notion's documentation, not exercised with additional real
accounts. Multipart byte integrity and protocol failure cases are covered by
local HTTP integration tests; the live test files were below 20 MiB.

## Appearance and customization

Stamp creates the initial Notion presentation. Its defaults live in
`internal/notion/presentation.go` and the page creation code:

| Element | Initial source | Customization |
| --- | --- | --- |
| Project title | Project name in `stamp.yaml` when created | Rename the Notion page; push preserves it |
| Library and history titles | Stamp defaults: Published files / Version history | Rename in Notion; identity is stored by ID |
| Folder titles | Rendered output directory names | Rename in Notion; known folders use catalog IDs |
| Document titles | Output filename with its extension removed, separators replaced, and first letter capitalized | Rename in Notion; push preserves the page title |
| Page icons | Stamp's initial emoji defaults (🔖 for the project) | Change or remove in Notion; ordinary pushes preserve your choice |
| Cover | None by default | Add any Notion cover; Stamp does not replace it |
| Introductions and block order | Stamp's initial page layout | Add notes/headings and arrange ordinary page content in Notion |
| Page font, spacing, and PDF viewer | Notion's native presentation | Notion's page settings; not Stamp CSS |
| The PDF itself | Source Markdown/TSX and the Stamp project's `theme/` | Edit the source/theme and push |

The root page ends with a compact **Project File** toggle containing the current
archive download and project metadata. The output catalog uses **Stamp details**,
and history entries use **Details** toggles. Keep these toggles,
their metadata blocks, and managed attachment captions intact. Moving the managed
attachment outside the root page or its **Project File** toggle is not supported. Ordinary notes,
page titles, covers, and icons are preserved on subsequent pushes. There is
currently no separate Notion presentation configuration in `stamp.yaml`.

History entries show the original creation date (UTC) in bold, with the archive’s
version message muted beneath it, in a quote above the download. Dividers separate
versions. Archives without version metadata use “Project version” and the Notion
upload time. Revision hashes and parent links live in a collapsed **Details**
toggle below each download. Older caption-based records remain readable; migration
writes the hidden record before clearing its caption, preserving archive IDs.
