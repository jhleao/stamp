# Getting started

Run the built-in walkthrough at any time:

```sh
stamp tutorial
```

The complete first-time path is:

```sh
brew install jhleao/tap/stamp
stamp setup
```

Setup checks the machine, offers to install missing tools, connects Google
Drive, and guides you through creating or cloning a project and opening Studio.
Run the individual commands below when you already know which project you want:

```sh
stamp new board-pack --name "Board Pack"
cd board-pack && stamp studio
```

`stamp new` creates the local workspace and connected Google Drive project as
one operation. Add `--choose-drive-folder` if it should live somewhere other
than My Drive. Every project contains its content, TSX components, Tailwind
theme, templates, examples, and assets.

Use Studio to edit and inspect previews. To collaborate:

```sh
stamp pull
# Edit and inspect in Studio.
stamp push --message "Tighten the narrative"
```

Share the project folder in Google Drive. A colleague checks it out with:

```sh
stamp login
stamp clone board-pack
# Follow the permission checklist for the .stamp archive and published files.
cd board-pack
stamp studio
```

Pull refuses to replace local edits when Drive also changed. Push uses an exact
Drive revision lease and refuses to overwrite a newer shared version.
Clone verifies every existing published file before creating the workspace.
Stamp remembers those Drive identities, so normal Pull and Push operations do
not repeat the setup. If a teammate publishes a new output later, the next Pull
asks only for access to that new item.
