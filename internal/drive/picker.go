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
)

const pickerAppID = "174648149574"
const pickerAddress = "127.0.0.1:57184"

var DefaultPickerAPIKey string

type pickerRequest struct {
	title   string
	prompt  string
	folders bool
	mime    string
}

// PickProjectArchive asks the signed-in user to authorize a shared Stamp
// project's canonical archive. Authorizing the archive gives the app access
// under drive.file without exposing unrelated Drive content.
func PickProjectArchive(ctx context.Context) (string, error) {
	return pick(ctx, pickerRequest{
		title:  "Choose a .stamp project file",
		prompt: "Choose the project’s .stamp file.",
		mime:   "application/vnd.stamp+zip",
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

func pick(ctx context.Context, request pickerRequest) (string, error) {
	developerKey := strings.TrimSpace(os.Getenv("STAMP_GOOGLE_PICKER_API_KEY"))
	if developerKey == "" {
		developerKey = strings.TrimSpace(DefaultPickerAPIKey)
	}
	if developerKey == "" {
		return "", errors.New("Google Picker is not configured in this build")
	}
	config, clientID, err := oauthConfig()
	if err != nil {
		return "", err
	}
	token, err := loadToken(clientID)
	if err != nil {
		return "", err
	}
	access, err := config.TokenSource(ctx, token).Token()
	if err != nil {
		return "", fmt.Errorf("refresh Google login: %w", err)
	}
	listener, err := net.Listen("tcp", pickerAddress)
	if err != nil {
		return "", fmt.Errorf("open Google Picker on localhost:57184: %w", err)
	}
	defer listener.Close()

	selection := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src https://apis.google.com 'unsafe-inline'; frame-src https://docs.google.com https://drive.google.com; style-src 'unsafe-inline'; connect-src 'self'; base-uri 'none'; form-action 'none'")
		_, _ = io.WriteString(w, pickerHTML(access.AccessToken, developerKey, request))
	})
	mux.HandleFunc("POST /picked", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var request struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&request); err != nil || strings.TrimSpace(request.ID) == "" {
			http.Error(w, "invalid selection", http.StatusBadRequest)
			return
		}
		select {
		case selection <- request.ID:
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /cancel", func(w http.ResponseWriter, _ *http.Request) {
		select {
		case selection <- "":
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go server.Serve(listener)
	url := "http://localhost:57184/"
	if err := exec.Command("open", url).Start(); err != nil {
		return "", fmt.Errorf("open Google Picker: %w", err)
	}
	defer server.Shutdown(context.Background())
	select {
	case id := <-selection:
		if id == "" {
			return "", errors.New("Google Drive project selection cancelled")
		}
		return id, nil
	case <-time.After(5 * time.Minute):
		return "", errors.New("Google Drive project selection timed out")
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func pickerHTML(accessToken, developerKey string, request pickerRequest) string {
	tokenJSON, _ := json.Marshal(accessToken)
	keyJSON, _ := json.Marshal(developerKey)
	titleJSON, _ := json.Marshal(request.title)
	promptJSON, _ := json.Marshal(request.prompt)
	foldersJSON, _ := json.Marshal(request.folders)
	mimeJSON, _ := json.Marshal(request.mime)
	return `<!doctype html>
<meta charset="utf-8">
<title>` + string(titleJSON) + `</title>
<style>
  :root { color-scheme: dark; font: 15px system-ui, sans-serif; background:#111; color:#ddd }
  body { margin:0; min-height:100vh; display:grid; place-items:center }
  p { color:#888 }
</style>
<main><strong>Opening Google Drive…</strong><p id="prompt"></p></main>
<script src="https://apis.google.com/js/api.js"></script>
<script>
const token = ` + string(tokenJSON) + `;
const developerKey = ` + string(keyJSON) + `;
const title = ` + string(titleJSON) + `;
const prompt = ` + string(promptJSON) + `;
const folders = ` + string(foldersJSON) + `;
const mime = ` + string(mimeJSON) + `;
document.getElementById('prompt').textContent = prompt;
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
  const builder = new google.picker.PickerBuilder()
    .addView(view).setOAuthToken(token).setDeveloperKey(developerKey)
    .setAppId('` + pickerAppID + `').setTitle(title);
  if (mime) builder.setSelectableMimeTypes(mime);
  const picker = builder
    .setCallback(data => {
      if (data.action === google.picker.Action.PICKED) finish('/picked', {id:data.docs[0].id});
      if (data.action === google.picker.Action.CANCEL) finish('/cancel');
    }).build();
  picker.setVisible(true);
}
gapi.load('picker', openPicker);
</script>`
}
