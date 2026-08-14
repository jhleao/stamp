# Google Drive

Stamp uses its first-party OAuth desktop client with the narrow `drive.file`
scope. Sign in once:

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
# Choose the shared project folder in Google Picker.
cd board-pack
stamp studio
```

Picker is required because a shared URL alone does not grant an application
using `drive.file` access to that folder.

Stamp stores the canonical project as one `.stamp` archive. Each Push creates a
retained Drive revision and uses its immutable content version as a lease. The
visible files in the project’s `Current` folder are derived mirrors.
