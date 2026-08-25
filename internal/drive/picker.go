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

// DefaultPickerAPIKey is public browser configuration, not a secret.
var DefaultPickerAPIKey = "REMOVED_GOOGLE_PICKER_API_KEY"

// PickFolder asks the signed-in user to select a shared Stamp project folder or
// its canonical archive. Selecting the archive is the drive.file escape hatch
// for projects whose contents were created before this user authorized Stamp.
func PickFolder(ctx context.Context) (string, error) {
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
		_, _ = io.WriteString(w, pickerHTML(access.AccessToken, developerKey))
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

func pickerHTML(accessToken, developerKey string) string {
	tokenJSON, _ := json.Marshal(accessToken)
	keyJSON, _ := json.Marshal(developerKey)
	return `<!doctype html>
<meta charset="utf-8">
<title>Clone a Stamp project</title>
<style>
  :root { color-scheme: dark; font: 15px system-ui, sans-serif; background:#111; color:#ddd }
  body { margin:0; min-height:100vh; display:grid; place-items:center }
  p { color:#888 }
</style>
<main><strong>Opening Google Drive…</strong><p>Choose the project folder or its .stamp archive.</p></main>
<script src="https://apis.google.com/js/api.js"></script>
<script>
const token = ` + string(tokenJSON) + `;
const developerKey = ` + string(keyJSON) + `;
function finish(path, body) {
  fetch(path, {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(body || {})})
    .finally(() => window.close());
}
function openPicker() {
  const folders = new google.picker.DocsView(google.picker.ViewId.FOLDERS)
    .setIncludeFolders(true).setSelectFolderEnabled(true)
    .setMode(google.picker.DocsViewMode.LIST);
  const archives = new google.picker.DocsView(google.picker.ViewId.DOCS)
    .setMode(google.picker.DocsViewMode.LIST);
  const picker = new google.picker.PickerBuilder()
    .addView(folders).addView(archives)
    .setOAuthToken(token).setDeveloperKey(developerKey)
    .setAppId('` + pickerAppID + `').setTitle('Clone a Stamp project')
    .setCallback(data => {
      if (data.action === google.picker.Action.PICKED) finish('/picked', {id:data.docs[0].id});
      if (data.action === google.picker.Action.CANCEL) finish('/cancel');
    }).build();
  picker.setVisible(true);
}
gapi.load('picker', openPicker);
</script>`
}
