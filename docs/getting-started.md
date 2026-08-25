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
one operation. Every project contains its content, TSX components, Tailwind
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
# Choose the shared project folder in Google Picker.
cd board-pack
stamp studio
```

Pull refuses to replace local edits when Drive also changed. Push uses an exact
Drive revision lease and refuses to overwrite a newer shared version.
