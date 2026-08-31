package drive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jhleao/stamp/internal/diagnostic"
)

const pickerAppID = "174648149574"
const pickerAddress = "127.0.0.1:57184"

var DefaultPickerAPIKey string

type pickerRequest struct {
	title    string
	prompt   string
	folders  bool
	mime     string
	parent   string
	required []FileRef
	intro    *pickerIntro
}

type pickerIntro struct {
	Title  string
	Body   string
	Steps  []string
	Action string
}

type pickedFile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// PickProjectArchive asks the signed-in user to authorize a shared Stamp
// project's canonical archive. Authorizing the archive gives the app access
// under drive.file without exposing unrelated Drive content.
func PickProjectArchive(ctx context.Context) (string, error) {
	return pick(ctx, pickerRequest{
		title:  "Choose a .stamp project file",
		prompt: "Choose the project’s .stamp file.",
		mime:   "application/vnd.stamp+zip",
		intro: &pickerIntro{
			Title:  "Choose the shared Stamp project",
			Body:   "A Stamp project is stored in Google Drive as one .stamp file. Select that file—not its folder or a rendered PDF—to create your local working copy.",
			Steps:  []string{"Open the shared project folder in Google Drive.", "Select the file whose name ends in .stamp, then click Select."},
			Action: "Choose a .stamp project file",
		},
	})
}

// PickRemoteArchive selects the canonical archive that an existing local
// workspace should use from now on.
func PickRemoteArchive(ctx context.Context) (string, error) {
	return pick(ctx, pickerRequest{
		title:  "Choose the new .stamp remote",
		prompt: "Choose the new remote’s .stamp file.",
		mime:   "application/vnd.stamp+zip",
		intro: &pickerIntro{
			Title:  "Connect this workspace to another Drive location",
			Body:   "Stamp will verify that the selected .stamp file belongs to this project. Your local files will stay exactly as they are.",
			Steps:  []string{"Open the target project folder in Google Drive.", "Select its .stamp file—not the folder or a rendered PDF.", "After selection, review the old and new Drive locations before confirming."},
			Action: "Choose the new .stamp remote",
		},
	})
}

// PickDestinationFolder asks where a new project should be created in Drive.
func PickDestinationFolder(ctx context.Context) (string, error) {
	return pick(ctx, pickerRequest{
		title:   "Choose where to create the Stamp project",
		prompt:  "Choose a Google Drive folder.",
		folders: true,
	})
}

// PickCurrentFolder grants Stamp access to a shared project's Current folder
// when drive.file cannot discover a sibling created by another account.
func PickCurrentFolder(ctx context.Context, projectFolderID string) (string, error) {
	return pick(ctx, pickerRequest{
		title:   "Choose this project’s Current folder",
		prompt:  "Choose the Current folder inside this Stamp project.",
		folders: true,
		parent:  projectFolderID,
		intro: &pickerIntro{
			Title:  "Connect the published documents",
			Body:   "Google Drive requires a one-time permission for folders created by another collaborator. Stamp needs the Current folder so it can update PDFs and spreadsheets in place.",
			Steps:  []string{"Open the same project folder that contains the .stamp file you just selected.", "Select the folder named Current.", "Stamp will verify its name and location before changing anything."},
			Action: "Choose the Current folder",
		},
	})
}

func pick(ctx context.Context, request pickerRequest) (string, error) {
	files, err := pickFiles(ctx, request)
	if err != nil {
		return "", err
	}
	if len(files) != 1 {
		return "", errors.New("Google Drive selection did not return one item")
	}
	return files[0].ID, nil
}

func pickFiles(ctx context.Context, request pickerRequest) ([]pickedFile, error) {
	done := diagnostic.Start("picker", "select", "folders", request.folders, "mime", request.mime)
	developerKey := strings.TrimSpace(os.Getenv("STAMP_GOOGLE_PICKER_API_KEY"))
	if developerKey == "" {
		developerKey = strings.TrimSpace(DefaultPickerAPIKey)
	}
	if developerKey == "" {
		err := errors.New("Google Picker is not configured in this build")
		done(err)
		return nil, err
	}
	config, clientID, err := oauthConfig()
	if err != nil {
		done(err)
		return nil, err
	}
	token, err := loadToken(clientID)
	if err != nil {
		done(err)
		return nil, err
	}
	access, err := config.TokenSource(diagnostic.HTTPContext(ctx, "google-oauth"), token).Token()
	if err != nil {
		done(err)
		return nil, fmt.Errorf("refresh Google login: %w", err)
	}
	listener, err := net.Listen("tcp", pickerAddress)
	if err != nil {
		done(err)
		return nil, fmt.Errorf("open Google Picker on localhost:57184: %w", err)
	}
	defer listener.Close()

	selection := make(chan []pickedFile, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src https://apis.google.com 'unsafe-inline'; frame-src https://docs.google.com https://drive.google.com; style-src 'unsafe-inline'; connect-src 'self'; base-uri 'none'; form-action 'none'")
		_, _ = io.WriteString(w, pickerHTML(access.AccessToken, developerKey, request))
	})
	mux.HandleFunc("POST /picked", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload struct {
			Files []pickedFile `json:"files"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&payload); err != nil || len(payload.Files) == 0 {
			http.Error(w, "invalid selection", http.StatusBadRequest)
			return
		}
		select {
		case selection <- payload.Files:
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /cancel", func(w http.ResponseWriter, _ *http.Request) {
		select {
		case selection <- nil:
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go server.Serve(listener)
	url := "http://localhost:57184/"
	if err := exec.Command("open", url).Start(); err != nil {
		done(err)
		return nil, fmt.Errorf("open Google Picker: %w", err)
	}
	defer server.Shutdown(context.Background())
	select {
	case files := <-selection:
		if len(files) == 0 {
			err := errors.New("Google Drive project selection cancelled")
			done(err)
			return nil, err
		}
		diagnostic.Log("picker", "selected", "files", len(files))
		done(nil)
		return files, nil
	case <-time.After(5 * time.Minute):
		err := errors.New("Google Drive project selection timed out")
		done(err)
		return nil, err
	case <-ctx.Done():
		done(ctx.Err())
		return nil, ctx.Err()
	}
}

func pickerHTML(accessToken, developerKey string, request pickerRequest) string {
	tokenJSON, _ := json.Marshal(accessToken)
	keyJSON, _ := json.Marshal(developerKey)
	titleJSON, _ := json.Marshal(request.title)
	promptJSON, _ := json.Marshal(request.prompt)
	foldersJSON, _ := json.Marshal(request.folders)
	mimeJSON, _ := json.Marshal(request.mime)
	parentJSON, _ := json.Marshal(request.parent)
	required := request.required
	if required == nil {
		required = []FileRef{}
	}
	requiredJSON, _ := json.Marshal(required)
	introJSON, _ := json.Marshal(request.intro)
	return `<!doctype html>
<meta charset="utf-8">
<title>` + string(titleJSON) + `</title>
<style>
  :root { color-scheme: dark; font: 15px system-ui, sans-serif; background:#0f1113; color:#e6e8ea }
  * { box-sizing:border-box }
  body { margin:0; min-height:100vh; display:grid; place-items:center; padding:32px }
  main { width:min(520px, 100%); border:1px solid #2b2e32; background:#15181b; padding:32px }
  h1 { margin:0 0 12px; font-size:22px; line-height:1.2 }
  p { margin:0; color:#a4a9af; line-height:1.55 }
  ol { margin:24px 0; padding:0; list-style:none; counter-reset:steps }
  li { position:relative; min-height:32px; padding:4px 0 16px 44px; color:#d1d4d7; line-height:1.45; counter-increment:steps }
  li::before { content:counter(steps); position:absolute; left:0; top:0; display:grid; place-items:center; width:28px; height:28px; border:1px solid #3a3e43; color:#fff }
  button { width:100%; border:1px solid #e6e8ea; background:#e6e8ea; color:#111; padding:11px 14px; font:600 14px system-ui, sans-serif; cursor:pointer }
  button:disabled { cursor:wait; opacity:.45 }
  small { display:block; margin-top:14px; color:#777e85; line-height:1.45 }
</style>
<main id="ordinary"><strong>Opening Google Drive…</strong><p id="prompt"></p></main>
<main id="migration" hidden>
  <h1 id="intro-title"></h1>
  <p id="intro-body"></p>
  <ol id="intro-steps"></ol>
  <button id="choose" type="button" disabled>Loading Google Drive…</button>
</main>
<script src="https://apis.google.com/js/api.js"></script>
<script>
const token = ` + string(tokenJSON) + `;
const developerKey = ` + string(keyJSON) + `;
const title = ` + string(titleJSON) + `;
const prompt = ` + string(promptJSON) + `;
const folders = ` + string(foldersJSON) + `;
const mime = ` + string(mimeJSON) + `;
const parent = ` + string(parentJSON) + `;
const required = ` + string(requiredJSON) + `;
const configuredIntro = ` + string(introJSON) + `;
document.getElementById('prompt').textContent = prompt;
const migration = required.length > 0;
const intro = migration ? {
  Title: 'Connect existing published files',
  Body: 'Stamp needs permission to update ' + required.length + ' file' + (required.length === 1 ? '' : 's') + ' already published by this project. Connecting the exact files prevents Google Drive from creating duplicate copies.',
  Steps: ['In Google Drive, press <strong>⌘A</strong> to select every file shown.', 'Click <strong>Select</strong>. Stamp verifies the complete set before changing anything.'],
  Action: 'Choose ' + required.length + ' published file' + (required.length === 1 ? '' : 's'),
} : configuredIntro;
if (intro) {
  document.getElementById('ordinary').hidden = true;
  document.getElementById('migration').hidden = false;
  document.getElementById('intro-title').textContent = intro.Title;
  document.getElementById('intro-body').textContent = intro.Body;
  document.getElementById('intro-steps').innerHTML = intro.Steps.map(step => '<li>' + step + '</li>').join('');
}
function finish(path, body) {
  fetch(path, {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(body || {})})
    .finally(() => window.close());
}
function openPicker() {
  const view = folders
    ? new google.picker.DocsView(google.picker.ViewId.FOLDERS)
        .setIncludeFolders(true).setSelectFolderEnabled(true)
        .setMode(google.picker.DocsViewMode.LIST)
    : new google.picker.DocsView(google.picker.ViewId.DOCS)
        .setMimeTypes(mime).setMode(google.picker.DocsViewMode.LIST);
  if (parent) view.setParent(parent);
  const builder = new google.picker.PickerBuilder()
    .addView(view).setOAuthToken(token).setDeveloperKey(developerKey)
    .setAppId('` + pickerAppID + `').setTitle(title);
  if (mime) builder.setSelectableMimeTypes(mime);
  if (required.length > 1) builder.enableFeature(google.picker.Feature.MULTISELECT_ENABLED);
  const picker = builder
    .setCallback(data => {
      if (data.action === google.picker.Action.PICKED) {
        const files = data.docs.map(doc => ({id:doc.id, name:doc.name}));
        const unmatched = required.filter(want => !files.some(got => want.id ? got.id === want.id : got.name === want.name));
        if (required.length > 0 && (files.length !== required.length || unmatched.length)) {
          alert('Select exactly the ' + required.length + ' published file' + (required.length === 1 ? '' : 's') + ' shown in this folder. Stamp will not create duplicates.');
          picker.setVisible(true);
          return;
        }
        finish('/picked', {files});
      }
      if (data.action === google.picker.Action.CANCEL) finish('/cancel');
    }).build();
  picker.setVisible(true);
}
gapi.load('picker', () => {
  if (!intro) {
    openPicker();
    return;
  }
  const choose = document.getElementById('choose');
  choose.disabled = false;
  choose.textContent = intro.Action;
  choose.addEventListener('click', openPicker);
});
</script>`
}
