# Google Drive

`stamp setup` connects Google Drive during first-time setup. To reconnect or
switch accounts later, sign in directly:

```sh
stamp login
```

Create a project locally and in Drive as one operation:

```sh
stamp new board-pack --name "Board Pack"
cd board-pack
stamp studio
```

By default, Stamp creates the remote project in My Drive. Add
`--choose-drive-folder` to select another Drive folder before it uploads the
project. The command prints the resulting Drive folder URL; share that folder
with collaborators using ordinary Google Drive permissions.

To work on a shared project:

```sh
stamp clone board-pack
# Choose the project's .stamp file in Google Picker.
cd board-pack
stamp studio
```

Stamp requests only Google’s per-file `drive.file` permission. Picker makes the
project grant explicit without exposing the rest of the user’s Drive. Picker
shows only Stamp project archives, so select the `.stamp` file shared by the
project owner. Editor access to its containing folder lets Stamp maintain the
canonical archive and publish the generated files in `Current`.

Stamp stores the canonical project as one `.stamp` archive. Each Push creates a
retained Drive revision and uses its immutable content version as a lease. The
visible files in the project’s `Current` folder are derived mirrors.
