# Google Drive

## Diagnose a sync problem

Run the failing operation with the global verbose flag and capture stderr:

```sh
stamp --verbose clone 2>&1 | tee stamp-debug.log
stamp --verbose pull 2>&1 | tee stamp-debug.log
stamp --verbose push --message "Debugging sync" 2>&1 | tee stamp-debug.log
```

Verbose output includes each OAuth and Drive HTTP request, status code, Google
request ID, duration, selected Drive object, lease comparison, rendered output,
and mirror create/update/delete decision. It never records authorization
headers, OAuth codes or tokens, API keys, query strings, or file contents.
Filenames and Drive object IDs remain visible so collaborators can identify the
failing operation; review those before sharing a log outside your team.

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

On the first Push from an older or newly shared checkout, Stamp may open a
second Picker titled **Connect published Stamp files**. Select every file shown
in `Current`; Picker is restricted to PDFs and spreadsheets and will not accept
an incomplete or different selection. This one-time grant lets Stamp update the
canonical files in place under `drive.file`. Their Drive IDs are then stored in
the shared project archive, so collaborators do not have to identify them
again. Stamp refuses the Push before writing anything if it cannot verify the
complete set—it never treats an inaccessible file as missing or silently
creates a same-named duplicate.

Stamp stores the canonical project as one `.stamp` archive. Each Push creates a
retained Drive revision and uses its immutable content version as a lease. The
visible files in the project’s `Current` folder are derived mirrors.
