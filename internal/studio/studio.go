package studio

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jhleao/stamp/internal/bundle"
	"github.com/jhleao/stamp/internal/collab"
	stampdrive "github.com/jhleao/stamp/internal/drive"
	"github.com/jhleao/stamp/internal/project"
	"github.com/jhleao/stamp/internal/render"
	"github.com/jhleao/stamp/internal/theme"
)

//go:embed static/*
var static embed.FS

const studioAddress = "127.0.0.1:57183"

type Server struct {
	root        string
	token       string
	origin      string
	version     string
	host        string
	clients     map[chan string]struct{}
	clientsMu   sync.Mutex
	renderMu    sync.Mutex
	lastChanges string
}

type fileItem struct {
	Path        string `json:"path"`
	Editable    bool   `json:"editable"`
	Previewable bool   `json:"previewable"`
	Section     string `json:"section"`
	Group       string `json:"group"`
	Label       string `json:"label"`
	PreviewPath string `json:"previewPath,omitempty"`
	Component   string `json:"component,omitempty"`
	Template    string `json:"templateLabel,omitempty"`
	Hidden      bool   `json:"hidden,omitempty"`
}

type pushRequest struct {
	Message string `json:"message"`
	Force   string `json:"forceWithLease"`
}

type pullRequest struct {
	Mode string `json:"mode"`
}

type componentRequest struct {
	Name string `json:"name"`
}

var componentName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

type syncStatus struct {
	State         string `json:"state"`
	Provider      string `json:"provider,omitempty"`
	LocalChanged  bool   `json:"localChanged"`
	RemoteChanged bool   `json:"remoteChanged"`
	DriveName     string `json:"driveName,omitempty"`
	DriveURL      string `json:"driveUrl,omitempty"`
	BaseVersion   string `json:"baseVersion,omitempty"`
	RemoteVersion string `json:"remoteVersion,omitempty"`
	FirstPush     bool   `json:"firstPush"`
	Message       string `json:"message,omitempty"`
}

type fileChange struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}

type syncDetails struct {
	Local  []fileChange `json:"local"`
	Remote []fileChange `json:"remote"`
}

func Start(ctx context.Context, root string, openBrowser bool, version string) error {
	connected, err := project.Connected(root)
	if err != nil {
		return err
	}
	if !connected {
		return errors.New("Studio opens only connected projects; create one with stamp new or check one out with stamp clone")
	}
	if err := theme.CompileIfNeeded(ctx, root); err != nil {
		return err
	}
	server := &Server{root: root, token: token(), clients: map[chan string]struct{}{}, version: version}
	listener, err := net.Listen("tcp", studioAddress)
	if err != nil {
		return fmt.Errorf("Studio needs %s; close the other Studio process and try again: %w", studioAddress, err)
	}
	server.host = listener.Addr().String()
	server.origin = "http://" + server.host
	httpServer := &http.Server{Handler: server.routes(), ReadHeaderTimeout: 5 * time.Second}
	go server.watch(ctx)
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdown)
	}()
	url := server.origin + "/" + server.token + "/"
	fmt.Println("Studio:", url)
	if openBrowser {
		_ = exec.Command("open", url).Start()
	}
	err = httpServer.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	base := "/" + s.token
	staticFS, _ := fs.Sub(static, "static")
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, base+"/", http.StatusFound) })
	mux.HandleFunc("GET "+base+"/", s.staticFile("static/index.html", "text/html; charset=utf-8"))
	mux.Handle("GET "+base+"/assets/", http.StripPrefix(base+"/", http.FileServer(http.FS(staticFS))))
	mux.HandleFunc("GET "+base+"/api/project", s.project)
	mux.HandleFunc("GET "+base+"/api/sync", s.sync)
	mux.HandleFunc("GET "+base+"/api/sync-details", s.syncDetails)
	mux.HandleFunc("GET "+base+"/api/file", s.readFile)
	mux.HandleFunc("PUT "+base+"/api/file", s.writeFile)
	mux.HandleFunc("GET "+base+"/api/preview", s.preview)
	mux.HandleFunc("GET "+base+"/api/component-preview", s.componentPreview)
	mux.HandleFunc("POST "+base+"/api/pull", s.pull)
	mux.HandleFunc("POST "+base+"/api/push", s.push)
	mux.HandleFunc("POST "+base+"/api/components", s.createComponent)
	mux.HandleFunc("GET "+base+"/api/events", s.events)
	mux.HandleFunc("GET "+base+"/files/", s.workspaceFile)
	return s.secure(mux)
}

func (s *Server) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != s.host {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != s.origin {
			http.Error(w, "forbidden origin", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self' 'unsafe-inline'; script-src 'self'; worker-src 'self' blob:; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-src 'self'; base-uri 'self'; form-action 'none'; frame-ancestors 'self'")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) staticFile(name, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		data, err := static.ReadFile(name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(data)
	}
}

func (s *Server) project(w http.ResponseWriter, _ *http.Request) {
	manifest, err := project.Load(s.root)
	if err != nil {
		s.writeError(w, err, http.StatusInternalServerError)
		return
	}
	state, _ := project.ReadState(s.root)
	status, _ := project.Status(s.root)
	files, err := s.files()
	if err != nil {
		s.writeError(w, err, http.StatusInternalServerError)
		return
	}
	workspacePath, err := filepath.Abs(s.root)
	if err != nil {
		s.writeError(w, err, http.StatusInternalServerError)
		return
	}
	s.writeJSON(w, map[string]any{"project": manifest, "state": state, "status": status, "files": files, "tailwindClasses": s.tailwindClasses(), "workspacePath": workspacePath})
}

var classAttribute = regexp.MustCompile(`(?i)\bclass(?:name)?\s*=\s*["']([^"']+)["']`)
var themeToken = regexp.MustCompile(`--(color|font)-([a-zA-Z0-9_-]+)\s*:`)

func (s *Server) tailwindClasses() []string {
	classes := map[string]bool{}
	_ = filepath.WalkDir(filepath.Join(s.root, "theme"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		if entry.Name() == "page.css" || entry.Name() == "deck.css" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || len(data) > 2<<20 {
			return nil
		}
		for _, match := range classAttribute.FindAllSubmatch(data, -1) {
			for _, class := range strings.Fields(string(match[1])) {
				if !strings.ContainsAny(class, "{}") {
					classes[class] = true
				}
			}
		}
		if entry.Name() == "tailwind.css" {
			for _, match := range themeToken.FindAllSubmatch(data, -1) {
				kind, name := string(match[1]), string(match[2])
				if kind == "color" {
					for _, prefix := range []string{"bg-", "text-", "border-", "fill-", "stroke-"} {
						classes[prefix+name] = true
					}
				} else {
					classes["font-"+name] = true
				}
			}
		}
		return nil
	})
	result := make([]string, 0, len(classes))
	for class := range classes {
		result = append(result, class)
	}
	sort.Strings(result)
	return result
}

func (s *Server) sync(w http.ResponseWriter, r *http.Request) {
	state, err := project.ReadState(s.root)
	if err != nil {
		s.writeError(w, err, http.StatusInternalServerError)
		return
	}
	status, err := project.Status(s.root)
	if err != nil {
		s.writeError(w, err, http.StatusInternalServerError)
		return
	}
	if state.FileID == "" {
		s.writeJSON(w, classifySync(state, status, ""))
		return
	}
	drive, err := stampdrive.New(r.Context())
	if err != nil {
		s.writeJSON(w, unavailableSync(state, status, err))
		return
	}
	remote, err := drive.Get(r.Context(), state.FileID)
	if err != nil {
		s.writeJSON(w, unavailableSync(state, status, err))
		return
	}
	s.writeJSON(w, classifySync(state, status, remote.Version))
}

func classifySync(state project.RemoteState, status project.ProjectStatus, remoteVersion string) syncStatus {
	identity := syncStatus{Provider: "drive", DriveName: status.Name, DriveURL: state.WebURL, BaseVersion: state.BaseVersion}
	if state.FileID == "" {
		identity.State, identity.LocalChanged, identity.FirstPush = "local-only", true, true
		return identity
	}
	localChanged := status.Dirty
	remoteChanged := remoteVersion != "" && remoteVersion != state.BaseVersion
	result := identity
	result.LocalChanged, result.RemoteChanged, result.RemoteVersion = localChanged, remoteChanged, remoteVersion
	switch {
	case localChanged && remoteChanged:
		result.State = "diverged"
	case localChanged:
		result.State = "local-ahead"
	case remoteChanged:
		result.State = "remote-ahead"
	default:
		result.State = "up-to-date"
	}
	return result
}

func unavailableSync(state project.RemoteState, status project.ProjectStatus, err error) syncStatus {
	return syncStatus{State: "unavailable", LocalChanged: status.Dirty, DriveName: status.Name, DriveURL: state.WebURL, BaseVersion: state.BaseVersion, Message: err.Error()}
}

func (s *Server) syncDetails(w http.ResponseWriter, r *http.Request) {
	state, err := project.ReadState(s.root)
	if err != nil {
		s.writeError(w, err, http.StatusInternalServerError)
		return
	}
	local, err := project.FileHashes(s.root)
	if err != nil {
		s.writeError(w, err, http.StatusInternalServerError)
		return
	}
	result := syncDetails{Local: diffHashes(state.Files, local), Remote: []fileChange{}}
	if state.FileID == "" {
		s.writeJSON(w, result)
		return
	}
	drive, err := stampdrive.New(r.Context())
	if err != nil {
		s.writeError(w, err, http.StatusUnauthorized)
		return
	}
	contents, err := drive.Download(r.Context(), state.FileID)
	if err != nil {
		s.writeError(w, err, http.StatusBadGateway)
		return
	}
	staging, err := os.MkdirTemp("", "stamp-sync-review-")
	if err != nil {
		s.writeError(w, err, http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(staging)
	if err := bundle.UnpackReader(bytes.NewReader(contents), int64(len(contents)), staging); err != nil {
		s.writeError(w, fmt.Errorf("inspect Drive project: %w", err), http.StatusUnprocessableEntity)
		return
	}
	remote, err := project.FileHashes(staging)
	if err != nil {
		s.writeError(w, err, http.StatusInternalServerError)
		return
	}
	result.Remote = diffHashes(state.Files, remote)
	s.writeJSON(w, result)
}

func diffHashes(base, current map[string]string) []fileChange {
	changes := make([]fileChange, 0)
	for path, hash := range current {
		kind := "modified"
		if _, ok := base[path]; !ok {
			kind = "added"
		} else if base[path] == hash {
			continue
		}
		changes = append(changes, fileChange{Path: path, Kind: kind})
	}
	for path := range base {
		if _, ok := current[path]; !ok {
			changes = append(changes, fileChange{Path: path, Kind: "removed"})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes
}

func (s *Server) readFile(w http.ResponseWriter, r *http.Request) {
	path, err := s.resolve(r.URL.Query().Get("path"))
	if err != nil {
		s.writeError(w, err, http.StatusBadRequest)
		return
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() > 2<<20 {
		s.writeError(w, errors.New("file is unavailable or too large to edit"), http.StatusBadRequest)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		s.writeError(w, err, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(data)
}

func (s *Server) writeFile(w http.ResponseWriter, r *http.Request) {
	path, err := s.resolve(r.URL.Query().Get("path"))
	if err != nil || !editable(path) {
		s.writeError(w, errors.New("that file is not editable in Studio"), http.StatusBadRequest)
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 2<<20+1))
	if err != nil || len(data) > 2<<20 {
		s.writeError(w, errors.New("file is too large"), http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		s.writeError(w, err, http.StatusInternalServerError)
		return
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".stamp-write-")
	if err != nil {
		s.writeError(w, err, http.StatusInternalServerError)
		return
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, err = temp.Write(data); err == nil {
		err = temp.Chmod(0o644)
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tempName, path)
	}
	if err != nil {
		s.writeError(w, err, http.StatusInternalServerError)
		return
	}
	warning := ""
	if strings.HasPrefix(filepath.ToSlash(strings.TrimPrefix(path, s.root+string(filepath.Separator))), "theme/") {
		if buildErr := theme.CompileIfNeeded(r.Context(), s.root); buildErr != nil {
			warning = buildErr.Error()
		}
	}
	s.broadcast("change")
	s.writeJSON(w, map[string]any{"ok": true, "warning": warning})
}

func (s *Server) preview(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	path, err := s.resolve(rel)
	if err != nil {
		s.writeError(w, err, http.StatusBadRequest)
		return
	}
	if strings.HasPrefix(filepath.ToSlash(rel), "outputs/") {
		http.ServeFile(w, r, path)
		return
	}
	s.renderMu.Lock()
	defer s.renderMu.Unlock()
	output, err := render.BrowserPreview(s.root, rel)
	if err != nil {
		s.writeError(w, err, http.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Content-Disposition", "inline")
	http.ServeFile(w, r, output)
}

func (s *Server) componentPreview(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if !componentName.MatchString(name) {
		s.writeError(w, errors.New("invalid component name"), http.StatusBadRequest)
		return
	}
	s.renderMu.Lock()
	defer s.renderMu.Unlock()
	base := "/" + s.token + "/files/"
	props := make(map[string]string)
	for key, values := range r.URL.Query() {
		if strings.HasPrefix(key, "prop.") && len(values) > 0 {
			props[strings.TrimPrefix(key, "prop.")] = values[len(values)-1]
		}
	}
	html, err := render.ComponentHTMLAtWith(s.root, name, base, props)
	if err != nil {
		s.writeError(w, err, http.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(html)
}

func (s *Server) workspaceFile(w http.ResponseWriter, r *http.Request) {
	prefix := "/" + s.token + "/files/"
	rel := strings.TrimPrefix(r.URL.Path, prefix)
	path, err := s.resolve(rel)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	http.ServeFile(w, r, path)
}

func (s *Server) pull(w http.ResponseWriter, r *http.Request) {
	var request pullRequest
	_ = json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&request)
	mode := collab.PullMode(request.Mode)
	if mode == "" {
		mode = collab.PullSafe
	}
	drive, err := stampdrive.New(r.Context())
	if err == nil {
		var message string
		message, err = collab.Pull(r.Context(), drive, s.root, mode)
		if err == nil {
			s.broadcast("change")
			s.writeJSON(w, map[string]any{"ok": true, "message": message})
			return
		}
	}
	s.writeError(w, err, http.StatusConflict)
}

func (s *Server) push(w http.ResponseWriter, r *http.Request) {
	var request pushRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&request); err != nil {
		s.writeError(w, err, http.StatusBadRequest)
		return
	}
	drive, err := stampdrive.New(r.Context())
	if err != nil {
		s.writeError(w, err, http.StatusUnauthorized)
		return
	}
	state, err := collab.Push(r.Context(), drive, s.root, request.Message, request.Force)
	if err != nil {
		s.writeError(w, err, http.StatusConflict)
		return
	}
	s.broadcast("change")
	s.writeJSON(w, map[string]any{"ok": true, "message": "Pushed Drive version " + state.BaseVersion, "state": state})
}

func (s *Server) createComponent(w http.ResponseWriter, r *http.Request) {
	var request componentRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&request); err != nil {
		s.writeError(w, err, http.StatusBadRequest)
		return
	}
	request.Name = strings.TrimSpace(strings.ToLower(request.Name))
	if !componentName.MatchString(request.Name) {
		s.writeError(w, errors.New("use a lowercase component name such as metric-card"), http.StatusBadRequest)
		return
	}
	rel := filepath.ToSlash(filepath.Join("theme", "components", request.Name+".tsx"))
	path, err := s.resolve(rel)
	if err != nil {
		s.writeError(w, err, http.StatusBadRequest)
		return
	}
	if _, err := os.Stat(path); err == nil {
		s.writeError(w, errors.New("that component already exists"), http.StatusConflict)
		return
	} else if !errors.Is(err, os.ErrNotExist) {
		s.writeError(w, err, http.StatusInternalServerError)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		s.writeError(w, err, http.StatusInternalServerError)
		return
	}
	starter := `export default function Component({ props, children }) {
  return (
    <section className="` + request.Name + ` my-6 border-y border-stone-300 py-4 text-sm leading-relaxed">
      {props.label && <p className="mb-2 text-xs font-semibold uppercase tracking-wide text-stone-500">{props.label}</p>}
      {children}
    </section>
  );
}
`
	if err := os.WriteFile(path, []byte(starter), 0o644); err != nil {
		s.writeError(w, err, http.StatusInternalServerError)
		return
	}
	s.broadcast("change")
	s.writeJSON(w, map[string]any{"ok": true, "path": rel, "message": "Created " + request.Name})
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Connection", "keep-alive")
	client := make(chan string, 4)
	s.clientsMu.Lock()
	s.clients[client] = struct{}{}
	s.clientsMu.Unlock()
	defer func() {
		s.clientsMu.Lock()
		delete(s.clients, client)
		s.clientsMu.Unlock()
	}()
	_, _ = io.WriteString(w, "event: ready\ndata: {}\n\n")
	flusher.Flush()
	for {
		select {
		case event := <-client:
			_, _ = fmt.Fprintf(w, "event: %s\ndata: {}\n\n", event)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) watch(ctx context.Context) {
	ticker := time.NewTicker(750 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			hashes, err := project.FileHashes(s.root)
			if err != nil {
				continue
			}
			data, _ := json.Marshal(hashes)
			fingerprint := string(data)
			if s.lastChanges != "" && fingerprint != s.lastChanges {
				if theme.CompileIfNeeded(ctx, s.root) == nil {
					hashes, _ = project.FileHashes(s.root)
					data, _ = json.Marshal(hashes)
					fingerprint = string(data)
				}
				s.broadcast("change")
			}
			s.lastChanges = fingerprint
		case <-ctx.Done():
			return
		}
	}
}

func (s *Server) broadcast(event string) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	for client := range s.clients {
		select {
		case client <- event:
		default:
		}
	}
}

func (s *Server) files() ([]fileItem, error) {
	var files []fileItem
	err := filepath.WalkDir(s.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(s.root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if rel == ".stamp" {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		rel = filepath.ToSlash(rel)
		files = append(files, fileItem{Path: rel, Editable: editable(path), Previewable: previewable(rel)})
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	decorateFiles(files)
	return files, err
}

func decorateFiles(files []fileItem) {
	pageExample := firstPathWithSuffix(files, ".page.md")
	deckExample := firstPathWithSuffix(files, ".deck.md")
	tailwindTheme := hasFilePath(files, "theme/tailwind.css")
	for i := range files {
		file := &files[i]
		file.Label = filepath.Base(file.Path)
		if !strings.HasPrefix(file.Path, "theme/") {
			file.Section = "content"
			file.PreviewPath = previewPath(file.Path, file.Previewable)
			switch {
			case strings.HasPrefix(file.Path, "documents/"):
				file.Group, file.Template = "Written pages", "Page template"
			case strings.HasPrefix(file.Path, "decks/"):
				file.Group, file.Template = "Slide decks", "Deck template"
			case strings.HasPrefix(file.Path, "spreadsheets/"):
				file.Group, file.Template = "Spreadsheets", "Spreadsheet"
			case strings.HasPrefix(file.Path, "assets/"):
				file.Group = "Assets"
			default:
				file.Group = "Project"
			}
			continue
		}
		file.Section = "templates"
		rel := strings.TrimPrefix(file.Path, "theme/")
		switch {
		case rel == "page.html.tmpl":
			file.Group, file.Label, file.PreviewPath = "Page template", "Structure", pageExample
		case rel == "page.css":
			file.Group, file.Label, file.PreviewPath = "Page template", "Styles", pageExample
			file.Hidden = tailwindTheme
		case rel == "deck.html.tmpl":
			file.Group, file.Label, file.PreviewPath = "Deck template", "Structure", deckExample
		case rel == "deck.css":
			file.Group, file.Label, file.PreviewPath = "Deck template", "Styles", deckExample
			file.Hidden = tailwindTheme
		case rel == "tailwind.css":
			file.Group, file.Label, file.PreviewPath = "Design system", "Tailwind theme", pageExample
		case strings.HasPrefix(rel, "components/"):
			name := strings.TrimSuffix(filepath.Base(rel), ".tsx")
			file.Group, file.Component = "Components", name
			file.Label = name
		case strings.HasPrefix(rel, "examples/"):
			file.Group, file.Label, file.PreviewPath = "Examples", filepath.Base(rel), file.Path
		case rel == "README.md":
			file.Group, file.Label = "About templates", "How templates work"
		default:
			file.Group = "Template files"
		}
	}
}

func hasFilePath(files []fileItem, path string) bool {
	for _, file := range files {
		if file.Path == path {
			return true
		}
	}
	return false
}

func firstPathWithSuffix(files []fileItem, suffix string) string {
	for _, file := range files {
		if strings.HasPrefix(file.Path, "theme/examples/") && strings.HasSuffix(file.Path, suffix) {
			return file.Path
		}
	}
	return ""
}

func previewPath(path string, canPreview bool) string {
	if canPreview {
		return path
	}
	return ""
}

func (s *Server) resolve(rel string) (string, error) {
	rel = filepath.Clean(filepath.FromSlash(rel))
	if rel == "." || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("invalid project path")
	}
	target := filepath.Join(s.root, rel)
	check, err := filepath.Rel(s.root, target)
	if err != nil || check == ".." || strings.HasPrefix(check, ".."+string(filepath.Separator)) {
		return "", errors.New("project path escapes workspace")
	}
	return target, nil
}

func (s *Server) writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) writeError(w http.ResponseWriter, err error, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
}

func editable(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".tmpl", ".tsx", ".css", ".yaml", ".yml", ".json", ".fods", ".fodp":
		return true
	default:
		return false
	}
}

func previewable(path string) bool {
	for _, suffix := range []string{".page.md", ".deck.md", ".doc.md", ".fods", ".fodp", ".xlsx", ".pdf"} {
		if strings.HasSuffix(strings.ToLower(path), suffix) {
			return true
		}
	}
	return false
}

func token() string {
	data := make([]byte, 24)
	_, _ = rand.Read(data)
	return base64.RawURLEncoding.EncodeToString(data)
}
