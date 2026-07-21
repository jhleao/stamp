# The whole workflow

## 1. Set up a Space once

Install Stamp and its three rendering applications:

```sh
brew install go pandoc libreoffice
brew install --cask google-chrome
make install
stamp doctor
```

Create a Google OAuth desktop client as described in [drive.md](drive.md), then
mark an existing Drive folder as the shared Space:

```sh
stamp login
stamp space init '<Drive folder URL>' --name 'Weve'
```

Stamp adds only `Projects/` and `Templates/`. There is no server or database.

## 2. Vibe-code a template

```sh
stamp project create board-pack --name 'Board Pack'
cd board-pack
stamp studio
```

Edit the examples in `theme/examples/` beside `theme/page.html.tmpl`,
`theme/deck.html.tmpl`, and their CSS. The Studio refreshes the result as files
change, including changes made by an external coding agent.

Templates are ordinary Go HTML templates. Stamp intentionally has no custom
template language, component runtime, registry, or package manager.

## 3. Make and preview the pack

Put content in the ordinary folders:

```text
documents/*.page.md or *.doc.md  -> PDF
decks/*.deck.md or *.fodp        -> PDF
spreadsheets/*.fods              -> XLSX
spreadsheets/*.xlsx              -> XLSX unchanged
```

Use Studio for quick editing, or run `stamp preview`. Manual edits and AI edits
are the same thing: filesystem changes. Generated files appear under `outputs/`.

## 4. Push one Drive version

There is no separate Save and Publish:

```sh
stamp push --space '<Space URL or ID>' --message 'first useful draft'
```

Push renders everything, updates the canonical `.stamp` file, and mirrors the
current PDF/XLSX outputs into Drive. The canonical file's Drive revision is the
project version, so a bad push remains recoverable.

## 5. Continue someone else's work

The other person opens the Drive project once:

```sh
stamp project open '<project folder or .stamp URL>' --dir board-pack
cd board-pack
stamp pull
stamp studio
stamp push --message 'tighten the narrative'
```

Pull replaces a clean workspace. If local and Drive both changed, it refuses.
Use `stamp pull --incoming` to inspect the remote files beside local work, or
`stamp pull --replace` to save a local recovery archive before replacing them.

Push also refuses if Drive advanced beyond the version that was pulled. After
reviewing that exact remote version, `--force-with-lease <version>` is the only
override. This is intentionally a warning boundary, not a lock service.
