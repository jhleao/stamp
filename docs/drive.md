# Google Drive setup

Stamp ships with its first-party Google OAuth desktop client and requests the
per-file `drive.file` scope. Normal setup requires no Google Cloud project or
downloaded credential:

```sh
stamp google-oauth status
stamp login
```

The browser login uses PKCE and stores the refresh token in macOS Keychain.

Organizations that require their own Google application can install a Desktop
app credential as an override:

```sh
stamp google-oauth ~/Downloads/client_secret_....json
stamp login
```

Stamp validates and copies an installed override with private permissions to:

```text
~/Library/Application Support/Stamp/google-oauth.json
```

An environment override has highest precedence:

```sh
export STAMP_GOOGLE_OAUTH_CONFIG=/path/to/google-oauth.json
```

Credential precedence is environment override, installed organization override,
then the bundled Stamp default. Return to the default with:

```sh
stamp google-oauth reset
stamp login
```

Create a new Space entirely from the CLI:

```sh
stamp space create 'Product Team'
stamp space list
```

To use an existing folder, let the CLI open Google Picker. Selecting it grants
Stamp access to that folder without granting access to the rest of Drive:

```sh
stamp space pick --name 'Product Team'
```

`stamp space init <folder-url-or-id>` remains available for a folder already
authorized to the active OAuth client. It cannot bypass `drive.file` access.

Google Picker also requires a browser API key from the same Google Cloud
project as the OAuth client. Official release builds embed Stamp's restricted
browser key. Source builds can supply a development key without storing it in
the repository:

```sh
PICKER_API_KEY='<browser-api-key>' make build
```

The key should permit only Google Picker API requests and localhost referrers.
For a one-off development run, `STAMP_GOOGLE_PICKER_API_KEY` overrides the
compiled value.

Rename a Space or a checked-out project without changing their Drive IDs:

```sh
stamp space rename '<Space URL or ID>' Stamp
stamp project rename 'Quarterly Plan' --dir ./quarterly-plan
```

The refresh token is stored in macOS Keychain under the service
`sh.stamp.google-drive`. `stamp logout` removes it.

With `drive.file`, Space and project listings include files created by Stamp or
explicitly authorized through Picker. Stamp cannot inspect unrelated Drive
content. Organization administrators may still need to trust the Stamp OAuth
client under their Workspace application policy.

## First project

```sh
stamp project create board-pack --name 'Board Pack' --space '<space folder ID>'
stamp studio --dir board-pack
```

Later checkouts can use either the project folder or canonical file URL:

```sh
stamp project list
stamp project open '<Drive URL>' --dir board-pack
```

Projects created under a different OAuth client do not inherit `drive.file`
authorization. Migrate a checked-out workspace without modifying the old Drive
project:

```sh
stamp project reconnect --dir board-pack --space '<authorized Space ID>'
```

Stamp saves the previous Drive state under `.stamp/recovery/` and publishes a
new canonical project through the active OAuth client.

Stamp stores the canonical project in one `.stamp` file. Each push updates that
file, producing a Google Drive revision marked `keepForever`. The immutable
head content revision ID is the workspace lease; unrelated Drive metadata does
not create false conflicts. Drive allows up
to 200 retained revisions for a binary file; that is Stamp's intentional
version-history ceiling. The visible files under the project's `Current`
folder are derived mirrors and can always be regenerated.
