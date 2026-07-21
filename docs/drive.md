# Google Drive setup

Stamp uses a Google OAuth desktop client owned by the organization. In Google
Cloud, enable the Drive API, configure the consent screen, and create an OAuth
client with application type **Desktop app**. Install the downloaded JSON:

```sh
stamp google-oauth ~/Downloads/client_secret_....json
```

Stamp validates and copies it with private permissions to:

```text
~/Library/Application Support/Stamp/google-oauth.json
```

For a different location:

```sh
export STAMP_GOOGLE_OAUTH_CONFIG=/path/to/google-oauth.json
```

Then connect and initialize an ordinary folder in My Drive or a Shared Drive:

```sh
stamp login
stamp space init 'https://drive.google.com/drive/folders/...' --name Weve
stamp space list
```

Or let Stamp create a new folder at the root of My Drive:

```sh
stamp space create Weve
```

Rename a Space or a checked-out project without changing their Drive IDs:

```sh
stamp space rename '<Space URL or ID>' Stamp
stamp project rename Test --dir ./weve-stamp
```

The refresh token is stored in macOS Keychain under the service
`sh.stamp.google-drive`. `stamp logout` removes it.

Stamp uses the full Drive scope so it can discover marked spaces and projects
shared with the signed-in person. For an organization-internal OAuth app, keep
the consent screen internal. Public distribution may require Google's scope
verification; that deployment decision is outside the executable.

## First project

```sh
stamp project create board-pack --name 'Board Pack'
cd board-pack
stamp preview
stamp push --space '<space folder ID>' --message 'first draft'
```

Later checkouts can use either the project folder or canonical file URL:

```sh
stamp project list
stamp project open '<Drive URL>' --dir board-pack
```

Stamp stores the canonical project in one `.stamp` file. Each push updates that
file, producing a Google Drive revision marked `keepForever`. The immutable
head content revision ID is the workspace lease; unrelated Drive metadata does
not create false conflicts. Drive allows up
to 200 retained revisions for a binary file; that is Stamp's intentional
version-history ceiling. The visible files under the project's `Current`
folder are derived mirrors and can always be regenerated.
