# The whole workflow

## 1. Set up a Space once

Install Stamp and its three rendering applications:

```sh
brew install go node pandoc libreoffice
brew install --cask google-chrome
npm install -g @tailwindcss/cli
npm install && make install
```

Connect Stamp's bundled Google application, then create a shared Space:

```sh
stamp doctor
stamp login
stamp space create 'Product Team'
```

Stamp adds only `Projects/` and `Templates/`. There is no server or database.

## 2. Start a project

```sh
stamp project create board-pack --name 'Board Pack' --space '<Space URL or ID>'
stamp studio --dir board-pack
```

Project creation prepares agent integration automatically. With `--space`, it
also renders and pushes the initial version; Studio opens only after that
connection exists. Change its words, then choose
**Save & preview** when you want a fresh result. Press **Push** when it is ready for other people. An agent pointed at
the folder reads `AGENTS.md` (or its `CLAUDE.md` compatibility link) and follows
the same pull → edit → preview → push loop automatically. Studio also hosts the
project's MCP endpoint, so Claude Code needs no second Stamp process.

To make a reusable theme first:

```sh
stamp template create my-theme
# Ask an agent to style my-theme/examples and add components.
stamp template preview my-theme
stamp project create board-pack --name 'Board Pack' --template my-theme --space '<Space URL or ID>'
```

Themes are ordinary HTML templates, Tailwind utilities, assets, examples, and small reusable
components. Read [templates.md](templates.md) for the complete format.

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
stamp studio
stamp push --message 'tighten the narrative'
```

Opening a project already downloads its current version. On later visits, Pull
lights up only when Drive has something newer.

Pull replaces a clean workspace. If local and Drive both changed, it refuses.
Use `stamp pull --incoming` to inspect the remote files beside local work, or
`stamp pull --replace` to save a local recovery archive before replacing them.

Push also refuses if Drive advanced beyond the version that was pulled. After
reviewing that exact remote version, `--force-with-lease <version>` is the only
override. This is intentionally a warning boundary, not a lock service.
