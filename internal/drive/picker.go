package drive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jhleao/stamp/internal/diagnostic"
	"golang.org/x/oauth2"
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
	handoff  bool
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

type authorizationItem struct {
	ID   string
	Name string
}

const exactPickerBatchSize = 75

var pickerHandoff struct {
	sync.Mutex
	next chan string
}

func registerPickerHandoff() chan string {
	pickerHandoff.Lock()
	defer pickerHandoff.Unlock()
	pickerHandoff.next = make(chan string, 1)
	return pickerHandoff.next
}

func sendPickerHandoff(next string) bool {
	pickerHandoff.Lock()
	channel := pickerHandoff.next
	pickerHandoff.next = nil
	pickerHandoff.Unlock()
	if channel == nil {
		return false
	}
	channel <- next
	return true
}

// AuthorizeProjectItems grants drive.file access to the project's known
// folders and rendered files. Google displays only these exact IDs, flattened
// into a single multi-select list, so hierarchy does not multiply the number
// of onboarding steps.
func (c *Client) AuthorizeProjectItems(ctx context.Context, folders []FolderRef, files []FileRef) error {
	items := make([]authorizationItem, 0, len(folders)+len(files))
	seen := map[string]bool{}
	for _, folder := range folders {
		if folder.ID != "" && !seen[folder.ID] {
			seen[folder.ID] = true
			items = append(items, authorizationItem{ID: folder.ID, Name: folder.Path + "/"})
		}
	}
	for _, file := range files {
		if file.ID != "" && !seen[file.ID] {
			seen[file.ID] = true
			items = append(items, authorizationItem{ID: file.ID, Name: file.Name})
		}
	}
	if len(items) == 0 {
		return nil
	}
	missing := items
	if os.Getenv("STAMP_FORCE_PROJECT_AUTHORIZATION") == "" {
		missing = nil
		for _, item := range items {
			if _, err := c.Get(ctx, item.ID); err != nil {
				missing = append(missing, item)
			}
		}
	}
	if len(missing) == 0 {
		sendPickerHandoff("http://localhost:57184/done")
		return nil
	}
	for start := 0; start < len(missing); start += exactPickerBatchSize {
		end := min(start+exactPickerBatchSize, len(missing))
		if err := authorizeExactIDs(ctx, missing[start:end], start/exactPickerBatchSize+1, (len(missing)+exactPickerBatchSize-1)/exactPickerBatchSize); err != nil {
			return err
		}
	}
	return nil
}

func authorizeExactIDs(ctx context.Context, items []authorizationItem, batch, batches int) error {
	ids := make([]string, len(items))
	for i, item := range items {
		ids[i] = item.ID
	}
	config, clientID, err := oauthConfig()
	if err != nil {
		return err
	}
	existing, err := loadToken(clientID)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("open project authorization: %w", err)
	}
	defer listener.Close()
	config.RedirectURL = "http://" + listener.Addr().String() + "/oauth/callback"
	state := randomToken()
	verifier := oauth2.GenerateVerifier()
	authURL, err := url.Parse(config.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce, oauth2.S256ChallengeOption(verifier)))
	if err != nil {
		return err
	}
	query := authURL.Query()
	query.Set("trigger_onepick", "true")
	query.Set("allow_multiple", "true")
	query.Set("allow_folder_selection", "true")
	query.Set("file_ids", strings.Join(ids, ","))
	query.Set("mimetypes", "application/vnd.google-apps.folder,application/pdf,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	authURL.RawQuery = query.Encode()

	type result struct {
		code string
		ids  []string
		err  error
	}
	completed := make(chan result, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, exactPickerIntro(authURL.String(), items, batch, batches))
	})
	mux.HandleFunc("GET /oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "Invalid Stamp authorization response.", http.StatusBadRequest)
			return
		}
		if oauthErr := r.URL.Query().Get("error"); oauthErr != "" {
			completed <- result{err: fmt.Errorf("Google Drive authorization was denied: %s", oauthErr)}
			http.Error(w, oauthErr, http.StatusBadRequest)
			return
		}
		picked := strings.Split(strings.TrimSpace(r.URL.Query().Get("picked_file_ids")), ",")
		if len(picked) == 1 && picked[0] == "" {
			picked = nil
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<!doctype html><meta charset="utf-8"><title>Stamp connected</title><style>:root{color-scheme:dark;font:15px system-ui,sans-serif;background:#0f1113;color:#e6e8ea}body{margin:0;min-height:100vh;display:grid;place-items:center}p{color:#a4a9af}</style><main><h1>Project connected</h1><p>You can return to Stamp.</p></main><script>setTimeout(()=>window.close(),900)</script>`)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		completed <- result{code: r.URL.Query().Get("code"), ids: picked}
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go server.Serve(listener)
	introURL := "http://" + listener.Addr().String() + "/"
	if !sendPickerHandoff(introURL) {
		if err := exec.Command("open", introURL).Start(); err != nil {
			return fmt.Errorf("open project authorization: %w", err)
		}
	}
	select {
	case selected := <-completed:
		go shutdownAfter(server, 3*time.Second)
		if selected.err != nil {
			return selected.err
		}
		if !sameIDs(ids, selected.ids) {
			return fmt.Errorf("select all %d project items before continuing", len(ids))
		}
		token, err := config.Exchange(diagnostic.HTTPContext(ctx, "google-oauth"), selected.code, oauth2.VerifierOption(verifier))
		if err != nil {
			return fmt.Errorf("exchange project authorization: %w", err)
		}
		if token.RefreshToken == "" {
			token.RefreshToken = existing.RefreshToken
		}
		return saveToken(clientID, token)
	case <-time.After(5 * time.Minute):
		_ = server.Shutdown(context.Background())
		return errors.New("Google Drive project authorization timed out")
	case <-ctx.Done():
		_ = server.Shutdown(context.Background())
		return ctx.Err()
	}
}

func sameIDs(want, got []string) bool {
	if len(want) != len(got) {
		return false
	}
	selected := make(map[string]bool, len(got))
	for _, id := range got {
		selected[id] = true
	}
	for _, id := range want {
		if !selected[id] {
			return false
		}
	}
	return true
}

func shutdownAfter(server *http.Server, delay time.Duration) {
	time.Sleep(delay)
	_ = server.Shutdown(context.Background())
}

func exactPickerIntro(authURL string, items []authorizationItem, batch, batches int) string {
	href, _ := json.Marshal(authURL)
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name)
	}
	namesJSON, _ := json.Marshal(names)
	progress := ""
	if batches > 1 {
		progress = fmt.Sprintf(" Batch %d of %d.", batch, batches)
	}
	return `<!doctype html><meta charset="utf-8"><title>Connect Stamp project</title>
<style>:root{color-scheme:dark;font:15px system-ui,sans-serif;background:#0f1113;color:#e6e8ea}*{box-sizing:border-box}body{margin:0;min-height:100vh;display:grid;place-items:center;padding:32px}main{width:min(620px,100%);border:1px solid #2b2e32;background:#15181b;padding:32px}h1{margin:0 0 12px;font-size:22px}p{color:#a4a9af;line-height:1.55}.steps{margin:20px 0;padding-left:22px;color:#d1d4d7;line-height:1.7}.files{margin:18px 0 24px;border:1px solid #2b2e32;max-height:220px;overflow:auto;list-style:none;padding:0}.files li{padding:8px 11px;border-bottom:1px solid #24272b;font:13px ui-monospace,SFMono-Regular,monospace}.files li:last-child{border-bottom:0}a{display:block;text-align:center;border:1px solid #e6e8ea;background:#e6e8ea;color:#111;padding:11px 14px;font-weight:600;text-decoration:none}</style>
<main><h1>Connect this Stamp project</h1><p>Stamp needs one-time access to ` + strconv.Itoa(len(items)) + ` project items so it can update published files in place.` + progress + `</p><ol class="steps"><li>Open the exact project-item set below.</li><li>Press <strong>⌘A</strong> to select everything shown, then click <strong>Select</strong>.</li></ol><ul class="files" id="files"></ul><a id="continue">Open project items</a></main>
<script>const names=` + string(namesJSON) + `;document.getElementById('files').innerHTML=names.map(name=>'<li>'+name.replace(/[&<>]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;'}[c]))+'</li>').join('');document.getElementById('continue').href=` + string(href) + `</script>`
}

// PickProjectArchive asks the signed-in user to authorize a shared Stamp
// project's canonical archive. Authorizing the archive gives the app access
// under drive.file without exposing unrelated Drive content.
func PickProjectArchive(ctx context.Context) (string, error) {
	return pick(ctx, pickerRequest{
		title:   "Choose a .stamp project file",
		prompt:  "Choose the project’s .stamp file.",
		mime:    "application/vnd.stamp+zip",
		handoff: true,
		intro: &pickerIntro{
			Title: "Connect the shared Stamp project",
			Body:  "Clone is the complete one-time setup for this project. Stamp verifies the source archive, its published folders, and every existing rendered file before creating your local working copy.",
			Steps: []string{
				"Select the file whose name ends in .stamp—not its folder or a rendered PDF.",
				"If Google asks, follow Stamp’s checklist to connect Current, its folders, and the published files inside them.",
				"Stamp creates the local workspace only after the complete project is connected.",
			},
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

func PickPublishedFolder(ctx context.Context, parentID, name string) (string, error) {
	return pick(ctx, pickerRequest{
		title:   "Choose the “" + name + "” folder",
		prompt:  "Choose “" + name + "” to continue connecting this project.",
		folders: true,
		parent:  parentID,
		intro: &pickerIntro{
			Title:  "Connect published folder",
			Body:   "Stamp needs one-time access to the “" + name + "” folder and the published files inside it.",
			Steps:  []string{"Open the folder shown by Stamp.", "Select the folder named “" + name + "”.", "Stamp verifies its name and location before continuing."},
			Action: "Choose “" + name + "”",
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
	if !request.handoff {
		defer listener.Close()
	}

	selection := make(chan []pickedFile, 1)
	var handoff chan string
	if request.handoff {
		handoff = registerPickerHandoff()
	}
	mux := http.NewServeMux()
	var server *http.Server
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
	mux.HandleFunc("GET /handoff", func(w http.ResponseWriter, _ *http.Request) {
		if handoff == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		select {
		case next := <-handoff:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"url": next})
			go shutdownAfter(server, 3*time.Second)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	})
	mux.HandleFunc("GET /done", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<!doctype html><meta charset="utf-8"><title>Stamp connected</title><style>:root{color-scheme:dark;font:15px system-ui,sans-serif;background:#0f1113;color:#e6e8ea}body{margin:0;min-height:100vh;display:grid;place-items:center}p{color:#a4a9af}</style><main><h1>Project connected</h1><p>You can return to Stamp.</p></main><script>setTimeout(()=>window.close(),900)</script>`)
	})
	server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go server.Serve(listener)
	url := "http://localhost:57184/"
	if err := exec.Command("open", url).Start(); err != nil {
		done(err)
		return nil, fmt.Errorf("open Google Picker: %w", err)
	}
	if !request.handoff {
		defer server.Shutdown(context.Background())
	} else {
		go shutdownAfter(server, 5*time.Minute)
	}
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
	handoffJSON, _ := json.Marshal(request.handoff)
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
const handoff = ` + string(handoffJSON) + `;
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
    .then(() => {
      if (!handoff) { window.close(); return; }
      document.body.innerHTML = '<main><strong>Preparing project access…</strong><p>Stamp is verifying the project and will continue here automatically.</p></main>';
      const wait = () => fetch('/handoff').then(async response => {
        if (response.status === 204) { setTimeout(wait, 250); return; }
        const result = await response.json();
        window.location.assign(result.url);
      }).catch(() => setTimeout(wait, 500));
      wait();
    });
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
