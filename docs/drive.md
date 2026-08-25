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

The command prints the Drive folder URL. Share that project folder with
collaborators using ordinary Google Drive permissions.

To work on a shared project:

```sh
stamp clone board-pack
# Choose the project folder or its .stamp archive in Google Picker.
cd board-pack
stamp studio
```

Stamp requests only Google’s per-file `drive.file` permission. Picker makes the
project grant explicit without exposing the rest of the user’s Drive. Select
the project folder first. If Stamp says its archive is not authorized, run
`stamp clone` again and select the `.stamp` archive inside that folder; this is
needed only when the archive predates that collaborator’s Stamp authorization.

Stamp stores the canonical project as one `.stamp` archive. Each Push creates a
retained Drive revision and uses its immutable content version as a lease. The
visible files in the project’s `Current` folder are derived mirrors.
