package studio

import (
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
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/weve-ai/stamp/internal/collab"
	stampdrive "github.com/weve-ai/stamp/internal/drive"
	"github.com/weve-ai/stamp/internal/project"
	"github.com/weve-ai/stamp/internal/render"
)

//go:embed static/*
var static embed.FS

type Server struct {
	root        string
	token       string
	origin      string
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
}

type pushRequest struct {
	Message string `json:"message"`
	Force   string `json:"forceWithLease"`
}

type pullRequest struct {
	Mode string `json:"mode"`
}

func Start(ctx context.Context, root string, openBrowser bool) error {
	server := &Server{root: root, token: token(), clients: map[chan string]struct{}{}}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
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
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, base+"/", http.StatusFound) })
	mux.HandleFunc("GET "+base+"/", s.staticFile("static/index.html", "text/html; charset=utf-8"))
	mux.HandleFunc("GET "+base+"/app.css", s.staticFile("static/app.css", "text/css; charset=utf-8"))
	mux.HandleFunc("GET "+base+"/app.js", s.staticFile("static/app.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET "+base+"/api/project", s.project)
	mux.HandleFunc("GET "+base+"/api/file", s.readFile)
	mux.HandleFunc("PUT "+base+"/api/file", s.writeFile)
	mux.HandleFunc("GET "+base+"/api/preview", s.preview)
	mux.HandleFunc("POST "+base+"/api/pull", s.pull)
	mux.HandleFunc("POST "+base+"/api/push", s.push)
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
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self' 'unsafe-inline'; script-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-src 'self'; base-uri 'self'; form-action 'none'; frame-ancestors 'self'")
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
	s.writeJSON(w, map[string]any{"project": manifest, "state": state, "status": status, "files": files})
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
	s.broadcast("change")
	s.writeJSON(w, map[string]any{"ok": true})
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
	if strings.HasSuffix(rel, ".page.md") || strings.HasSuffix(rel, ".deck.md") {
		base := "/" + s.token + "/files/"
		html, err := render.HTMLAt(s.root, rel, base)
		if err != nil {
			s.writeError(w, err, http.StatusUnprocessableEntity)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(html)
		return
	}
	output, err := render.BrowserPreview(s.root, rel)
	if err != nil {
		s.writeError(w, err, http.StatusUnprocessableEntity)
		return
	}
	http.ServeFile(w, r, output)
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
	state, err := collab.Push(r.Context(), drive, s.root, "", request.Message, request.Force)
	if err != nil {
		s.writeError(w, err, http.StatusConflict)
		return
	}
	s.broadcast("change")
	s.writeJSON(w, map[string]any{"ok": true, "message": "Pushed Drive version " + state.BaseVersion, "state": state})
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
	return files, err
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
	case ".md", ".tmpl", ".css", ".yaml", ".yml", ".json", ".fods", ".fodp":
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
