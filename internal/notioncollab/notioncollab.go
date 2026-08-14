package notioncollab

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/jhleao/stamp/internal/bundle"
	"github.com/jhleao/stamp/internal/notion"
	"github.com/jhleao/stamp/internal/project"
	"github.com/jhleao/stamp/internal/render"
)

const stateName = "notion.json"

type State struct {
	PageID        string           `json:"pageId"`
	URL           string           `json:"url"`
	Revision      int              `json:"revision"`
	ArchiveHash   string           `json:"archiveHash"`
	ArchiveSHA    string           `json:"archiveSha,omitempty"`
	ArchiveBlock  string           `json:"archiveBlock"`
	DocumentsPage string           `json:"documentsPage,omitempty"`
	DecksPage     string           `json:"decksPage,omitempty"`
	ExportsPage   string           `json:"exportsPage,omitempty"`
	SystemPage    string           `json:"systemPage,omitempty"`
	Encoding      int              `json:"encoding,omitempty"`
	Documents     map[string]Lease `json:"documents,omitempty"`
}

type Lease struct {
	PageID     string `json:"pageId"`
	EditedTime string `json:"editedTime"`
	Hash       string `json:"hash"`
}

type Status struct {
	State         State `json:"state"`
	LocalChanged  bool  `json:"localChanged"`
	RemoteChanged bool  `json:"remoteChanged"`
}

func StatePath(root string) string { return filepath.Join(root, ".stamp", stateName) }

func ReadState(root string) (State, error) {
	data, err := os.ReadFile(StatePath(root))
	if errors.Is(err, os.ErrNotExist) {
		return State{Documents: map[string]Lease{}}, nil
	}
	if err != nil {
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, err
	}
	if state.Documents == nil {
		state.Documents = map[string]Lease{}
	}
	return state, nil
}

func writeState(root string, state State) error {
	if err := os.MkdirAll(filepath.Join(root, ".stamp"), 0o700); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(state, "", "  ")
	return os.WriteFile(StatePath(root), append(data, '\n'), 0o600)
}

func Create(ctx context.Context, client *notion.Client, parent, root, name string) (State, error) {
	manifest, err := project.Load(root)
	if err != nil {
		return State{}, err
	}
	if name == "" {
		name = manifest.Name
	}
	page, err := client.CreatePage(ctx, parent, name)
	if err != nil {
		return State{}, err
	}
	state := State{PageID: page.ID, URL: page.URL, Documents: map[string]Lease{}}
	if err := writeState(root, state); err != nil {
		return State{}, err
	}
	return Push(ctx, client, root, "Create project", "")
}

func Open(ctx context.Context, client *notion.Client, value, destination string) (State, error) {
	pageID := notion.ID(value)
	page, err := client.GetPage(ctx, pageID)
	if err != nil {
		return State{}, err
	}
	remote, archive, err := remoteArchive(ctx, client, pageID)
	if err != nil {
		return State{}, err
	}
	if destination == "" {
		destination = "stamp-notion-project"
	}
	if entries, err := os.ReadDir(destination); err == nil && len(entries) > 0 {
		return State{}, fmt.Errorf("%s is not empty", destination)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return State{}, err
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return State{}, err
	}
	if err := bundle.UnpackReader(bytes.NewReader(archive), int64(len(archive)), destination); err != nil {
		return State{}, fmt.Errorf("unpack Notion project: %w", err)
	}
	if err := project.EnsureAgentCompatibility(destination); err != nil {
		return State{}, err
	}
	remote.URL = page.URL
	remote.Documents, err = pullDocuments(ctx, client, remote, destination, nil, true)
	if err != nil {
		return State{}, err
	}
	remote.Encoding = 2
	if err := writeState(destination, remote); err != nil {
		return State{}, err
	}
	return remote, nil
}

func Pull(ctx context.Context, client *notion.Client, root string, replace bool) (State, error) {
	local, err := ReadState(root)
	if err != nil {
		return State{}, err
	}
	remote, archive, err := remoteArchive(ctx, client, local.PageID)
	if err != nil {
		return State{}, err
	}
	localHash, err := archiveHash(root)
	if err != nil {
		return State{}, err
	}
	if localHash != local.ArchiveHash && remote.ArchiveHash != local.ArchiveHash && !replace {
		return State{}, errors.New("Notion source and local files both changed; inspect them and rerun with --replace")
	}
	if remote.ArchiveHash != local.ArchiveHash {
		if !replace && localHash != local.ArchiveHash {
			return State{}, errors.New("local source changed; commit or use --replace before pulling Notion")
		}
		if replace {
			recovery := filepath.Join(root, ".stamp", "recovery", "notion-before-pull.stamp")
			if err := os.MkdirAll(filepath.Dir(recovery), 0o700); err != nil {
				return State{}, err
			}
			if err := bundle.PackFile(root, recovery); err != nil {
				return State{}, err
			}
		}
		if err := replaceFromArchive(root, archive); err != nil {
			return State{}, err
		}
	}
	remote.URL = local.URL
	remote.Documents, err = pullDocuments(ctx, client, remote, root, local.Documents, replace)
	if err != nil {
		return State{}, err
	}
	remote.Encoding = local.Encoding
	if err := writeState(root, remote); err != nil {
		return State{}, err
	}
	return remote, nil
}

func Push(ctx context.Context, client *notion.Client, root, message, forceRevision string) (State, error) {
	if _, err := render.All(root); err != nil {
		return State{}, err
	}
	state, err := ReadState(root)
	if err != nil {
		return State{}, err
	}
	if state.PageID == "" {
		return State{}, errors.New("project has no Notion backend; run stamp notion project create")
	}
	state, err = ensureWorkspace(ctx, client, state)
	if err != nil {
		return State{}, err
	}
	if state.Revision > 0 {
		remote, _, err := remoteArchive(ctx, client, state.PageID)
		if err != nil {
			return State{}, err
		}
		if remote.Revision != state.Revision && forceRevision != strconv.Itoa(remote.Revision) {
			return State{}, fmt.Errorf("push refused: local Notion lease is %d but remote is %d; pull first or use --force-with-lease %d after review", state.Revision, remote.Revision, remote.Revision)
		}
	}
	documents, err := pushDocuments(ctx, client, state, root, state.Documents)
	if err != nil {
		return State{}, err
	}
	var archive bytes.Buffer
	if err := bundle.PackSource(root, &archive); err != nil {
		return State{}, err
	}
	hash, err := sourceHash(root)
	if err != nil {
		return State{}, err
	}
	archiveSHA := digest(archive.Bytes())
	revision := state.Revision + 1
	upload, err := client.Upload(ctx, fmt.Sprintf("project-source-v%d.zip", revision), "application/zip", archive.Bytes())
	if err != nil {
		return State{}, err
	}
	caption := fmt.Sprintf("stamp-source:%d:%s:%s:%s", revision, archiveSHA, hash, strings.ReplaceAll(strings.TrimSpace(message), ":", "-"))
	before, err := client.Children(ctx, state.SystemPage)
	if err != nil {
		return State{}, err
	}
	if err := client.Append(ctx, state.SystemPage, []map[string]any{notion.FileBlock(upload.ID, caption)}); err != nil {
		return State{}, err
	}
	after, err := client.Children(ctx, state.SystemPage)
	if err != nil {
		return State{}, err
	}
	archiveBlock := ""
	for _, block := range after {
		if block.Type == "file" && notion.FileCaption(block) == caption {
			archiveBlock = block.ID
		}
	}
	if archiveBlock == "" {
		return State{}, errors.New("Notion accepted source upload but its attached block was not found")
	}
	state.Revision, state.ArchiveHash, state.ArchiveSHA, state.ArchiveBlock, state.Documents = revision, hash, archiveSHA, archiveBlock, documents
	oldOutputs, err := client.Children(ctx, state.ExportsPage)
	if err != nil {
		return State{}, err
	}
	if err := uploadOutputs(ctx, client, state.ExportsPage, root, revision); err != nil {
		return State{}, fmt.Errorf("source revision %d is active, but outputs need retrying: %w", revision, err)
	}
	if err := writeState(root, state); err != nil {
		return State{}, err
	}
	for _, block := range before {
		caption := notion.FileCaption(block)
		if block.Type == "file" && (strings.HasPrefix(caption, "stamp-source:") || strings.HasPrefix(caption, "stamp-output:")) {
			_ = client.DeleteBlock(ctx, block.ID)
		}
	}
	for _, block := range oldOutputs {
		if block.Type == "file" {
			_ = client.DeleteBlock(ctx, block.ID)
		}
	}
	cleanupWorkspace(ctx, client, state)
	return state, nil
}

func StatusOf(ctx context.Context, client *notion.Client, root string) (Status, error) {
	state, err := ReadState(root)
	if err != nil {
		return Status{}, err
	}
	remote, _, err := remoteArchive(ctx, client, state.PageID)
	if err != nil {
		return Status{}, err
	}
	hash, err := archiveHash(root)
	if err != nil {
		return Status{}, err
	}
	return Status{State: state, LocalChanged: hash != state.ArchiveHash, RemoteChanged: remote.Revision != state.Revision}, nil
}

func remoteArchive(ctx context.Context, client *notion.Client, pageID string) (State, []byte, error) {
	areas, err := workspacePages(ctx, client, pageID)
	if err != nil {
		return State{}, nil, err
	}
	systemID := areas["Project settings"]
	if systemID == "" {
		// Backward compatibility with the original flat prototype.
		systemID = pageID
	}
	blocks, err := client.Children(ctx, systemID)
	if err != nil {
		return State{}, nil, err
	}
	if systemID != pageID {
		legacy, err := client.Children(ctx, pageID)
		if err != nil {
			return State{}, nil, err
		}
		blocks = append(blocks, legacy...)
	}
	var best notion.Block
	bestRevision := -1
	bestArchiveSHA, bestSourceHash := "", ""
	for _, block := range blocks {
		if block.Type != "file" {
			continue
		}
		parts := strings.SplitN(notion.FileCaption(block), ":", 6)
		if len(parts) < 3 || parts[0] != "stamp-source" {
			continue
		}
		revision, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		if revision > bestRevision {
			best, bestRevision, bestArchiveSHA = block, revision, parts[2]
			if len(parts) >= 5 {
				bestSourceHash = parts[3]
			} else {
				bestSourceHash = parts[2]
			}
		}
	}
	if bestRevision < 0 {
		return State{}, nil, errors.New("Notion page has no Stamp source archive")
	}
	data, err := client.Download(ctx, notion.FileURL(best))
	if err != nil {
		return State{}, nil, err
	}
	if digest(data) != bestArchiveSHA {
		return State{}, nil, errors.New("Notion source archive hash does not match its revision metadata")
	}
	return State{PageID: pageID, Revision: bestRevision, ArchiveHash: bestSourceHash, ArchiveSHA: bestArchiveSHA, ArchiveBlock: best.ID,
		DocumentsPage: areas["Documents"], DecksPage: areas["Presentations"], ExportsPage: areas["Exports"], SystemPage: systemID, Documents: map[string]Lease{}}, data, nil
}

func archiveHash(root string) (string, error) {
	return sourceHash(root)
}

func sourceHash(root string) (string, error) {
	files, err := project.FileHashes(root)
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, name := range names {
		_, _ = io.WriteString(h, name+"\x00"+files[name]+"\n")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func sourceFiles(root string) ([]string, error) {
	var files []string
	for _, top := range []string{"documents", "decks"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			if strings.HasSuffix(entry.Name(), ".md") {
				rel, err := filepath.Rel(root, path)
				if err != nil {
					return err
				}
				files = append(files, filepath.ToSlash(rel))
			}
			return nil
		})
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	sort.Strings(files)
	return files, nil
}

func workspacePages(ctx context.Context, client *notion.Client, projectID string) (map[string]string, error) {
	children, err := client.Children(ctx, projectID)
	if err != nil {
		return nil, err
	}
	pages := map[string]string{}
	for _, child := range children {
		if title := notion.ChildTitle(child); title != "" {
			pages[title] = child.ID
		}
	}
	return pages, nil
}

func ensureWorkspace(ctx context.Context, client *notion.Client, state State) (State, error) {
	pages, err := workspacePages(ctx, client, state.PageID)
	if err != nil {
		return State{}, err
	}
	type area struct{ title, icon string }
	areas := []area{
		{"Documents", "📝"},
		{"Presentations", "📊"},
		{"Exports", "📥"},
		{"Project settings", "⚙️"},
	}
	for _, item := range areas {
		if pages[item.title] != "" {
			continue
		}
		page, err := client.CreatePageMarkdownIcon(ctx, state.PageID, item.title, "", item.icon)
		if err != nil {
			return State{}, err
		}
		pages[item.title] = page.ID
	}
	migrating := state.SystemPage == ""
	state.DocumentsPage = pages["Documents"]
	state.DecksPage = pages["Presentations"]
	state.ExportsPage = pages["Exports"]
	state.SystemPage = pages["Project settings"]
	for _, pageID := range []string{state.DocumentsPage, state.DecksPage, state.ExportsPage, state.SystemPage} {
		if err := clearAreaText(ctx, client, pageID); err != nil {
			return State{}, err
		}
	}
	if migrating {
		// Old document pages live directly on the project. Recreate them in the
		// clean authoring hierarchy; cleanup happens only after the push succeeds.
		state.Documents = map[string]Lease{}
	}
	if state.Encoding < 2 {
		for path, lease := range state.Documents {
			lease.Hash = ""
			state.Documents[path] = lease
		}
		state.Encoding = 2
	}
	return state, nil
}

func clearAreaText(ctx context.Context, client *notion.Client, pageID string) error {
	children, err := client.Children(ctx, pageID)
	if err != nil {
		return err
	}
	for _, child := range children {
		if child.Type != "child_page" && child.Type != "file" {
			if err := client.DeleteBlock(ctx, child.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func prettyName(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(value, "-", " "), "_", " "))
	if value == "" {
		return "Untitled"
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func documentTitle(path string) string {
	name := filepath.Base(path)
	for _, suffix := range []string{".page.md", ".deck.md", ".doc.md", ".md"} {
		name = strings.TrimSuffix(name, suffix)
	}
	return prettyName(name)
}

func documentIcon(path string) string {
	if strings.HasPrefix(filepath.ToSlash(path), "decks/") {
		return "📊"
	}
	return "📝"
}

func documentArea(state State, path string) (string, []string) {
	parts := strings.Split(filepath.ToSlash(path), "/")
	parent := state.DocumentsPage
	if len(parts) > 0 && parts[0] == "decks" {
		parent = state.DecksPage
	}
	if len(parts) <= 2 {
		return parent, nil
	}
	return parent, parts[1 : len(parts)-1]
}

func childPage(ctx context.Context, client *notion.Client, parentID, title string) (string, error) {
	children, err := client.Children(ctx, parentID)
	if err != nil {
		return "", err
	}
	for _, child := range children {
		if notion.ChildTitle(child) == title {
			return child.ID, nil
		}
	}
	return "", nil
}

func ensureDocumentFolder(ctx context.Context, client *notion.Client, state State, path string) (string, error) {
	parent, folders := documentArea(state, path)
	for _, folder := range folders {
		title := prettyName(folder)
		id, err := childPage(ctx, client, parent, title)
		if err != nil {
			return "", err
		}
		if id == "" {
			page, err := client.CreatePageMarkdownIcon(ctx, parent, title, "", "📁")
			if err != nil {
				return "", err
			}
			id = page.ID
		}
		parent = id
	}
	return parent, nil
}

func findDocumentPage(ctx context.Context, client *notion.Client, state State, path string) (string, error) {
	parent, folders := documentArea(state, path)
	for _, folder := range folders {
		var err error
		parent, err = childPage(ctx, client, parent, prettyName(folder))
		if err != nil || parent == "" {
			return "", err
		}
	}
	return childPage(ctx, client, parent, documentTitle(path))
}

func cleanupWorkspace(ctx context.Context, client *notion.Client, state State) {
	keep := map[string]bool{state.DocumentsPage: true, state.DecksPage: true, state.ExportsPage: true, state.SystemPage: true}
	children, err := client.Children(ctx, state.PageID)
	if err != nil {
		return
	}
	for _, child := range children {
		if !keep[child.ID] {
			_ = client.DeleteBlock(ctx, child.ID)
		}
	}
}

func pushDocuments(ctx context.Context, client *notion.Client, state State, root string, leases map[string]Lease) (map[string]Lease, error) {
	files, err := sourceFiles(root)
	if err != nil {
		return nil, err
	}
	result := map[string]Lease{}
	for _, path := range files {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			return nil, err
		}
		hash := digest(data)
		pageID := leases[path].PageID
		lease := leases[path]
		if pageID != "" && lease.PageID == pageID && lease.Hash == hash {
			result[path] = lease
			continue
		}
		if pageID == "" {
			parentID, err := ensureDocumentFolder(ctx, client, state, path)
			if err != nil {
				return nil, err
			}
			page, err := client.CreatePageMarkdownIcon(ctx, parentID, documentTitle(path), encodeDocument(path, string(data)), documentIcon(path))
			if err != nil {
				return nil, err
			}
			pageID = page.ID
		} else {
			page, err := client.GetPage(ctx, pageID)
			if err != nil {
				return nil, err
			}
			if lease.PageID != "" && lease.EditedTime != "" && page.LastEditedTime != lease.EditedTime {
				return nil, fmt.Errorf("Notion document %s changed since pull; pull before pushing", path)
			}
			if _, err := client.ReplaceMarkdown(ctx, pageID, encodeDocument(path, string(data))); err != nil {
				return nil, err
			}
		}
		page, err := client.GetPage(ctx, pageID)
		if err != nil {
			return nil, err
		}
		remoteMarkdown, err := client.Markdown(ctx, pageID)
		if err != nil {
			return nil, err
		}
		normalized := decodeDocument(path, remoteMarkdown.Markdown)
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), []byte(normalized), 0o644); err != nil {
			return nil, err
		}
		result[path] = Lease{PageID: pageID, EditedTime: page.LastEditedTime, Hash: digest([]byte(normalized))}
	}
	return result, nil
}

func pullDocuments(ctx context.Context, client *notion.Client, state State, root string, leases map[string]Lease, replace bool) (map[string]Lease, error) {
	result := map[string]Lease{}
	files, err := sourceFiles(root)
	if err != nil {
		return nil, err
	}
	for _, path := range files {
		pageID := leases[path].PageID
		if pageID == "" {
			pageID, err = findDocumentPage(ctx, client, state, path)
			if err != nil {
				return nil, err
			}
		}
		if pageID == "" {
			continue
		}
		page, err := client.GetPage(ctx, pageID)
		if err != nil {
			return nil, err
		}
		markdown, err := client.Markdown(ctx, pageID)
		if err != nil {
			return nil, err
		}
		content := decodeDocument(path, markdown.Markdown)
		clean := filepath.Clean(filepath.FromSlash(path))
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("unsafe Notion document path %q", path)
		}
		data := []byte(content)
		hash := digest(data)
		old := leases[path]
		localPath := filepath.Join(root, clean)
		if local, err := os.ReadFile(localPath); err == nil && old.Hash != "" && digest(local) != old.Hash && old.Hash != hash && !replace {
			return nil, fmt.Errorf("Notion document %s and local file both changed; rerun pull with --replace after review", path)
		}
		if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(localPath, data, 0o644); err != nil {
			return nil, err
		}
		result[path] = Lease{PageID: pageID, EditedTime: page.LastEditedTime, Hash: hash}
	}
	return result, nil
}

func encodeDocument(path, content string) string {
	content = stripTitleFrontmatter(content)
	if strings.HasPrefix(filepath.ToSlash(path), "decks/") {
		content = strings.ReplaceAll(content, `\\<slide\\>`, "<slide>")
		content = strings.ReplaceAll(content, `\\</slide\\>`, "</slide>")
		content = strings.ReplaceAll(content, `\<slide\>`, "<slide>")
		content = strings.ReplaceAll(content, `\</slide\>`, "</slide>")
		content = strings.TrimSpace(content)
		for strings.HasPrefix(content, "<slide>") && strings.HasSuffix(content, "</slide>") {
			content = strings.TrimSpace(strings.TrimPrefix(content, "<slide>"))
			content = strings.TrimSpace(strings.TrimSuffix(content, "</slide>"))
		}
		content = strings.ReplaceAll(content, "</slide>\n\n<slide>", "---")
	}
	return strings.TrimSpace(content) + "\n"
}

func decodeDocument(path, markdown string) string {
	markdown = stripTitleFrontmatter(markdown)
	lines := strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n")
	// Read pages made by the original prototype without showing its path marker
	// in the redesigned authoring surface.
	if len(lines) > 0 && strings.HasPrefix(lines[0], "> Stamp source: `") && strings.HasSuffix(lines[0], "`") {
		lines = lines[1:]
	}
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	content := strings.TrimSpace(strings.Join(lines[start:], "\n"))
	if strings.HasPrefix(filepath.ToSlash(path), "decks/") && !strings.Contains(content, "<slide>") {
		parts := strings.Split(content, "\n---\n")
		for index := range parts {
			parts[index] = "<slide>\n" + strings.TrimSpace(parts[index]) + "\n</slide>"
		}
		content = strings.Join(parts, "\n\n")
	}
	return content + "\n"
}

func stripTitleFrontmatter(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(content, "---\n") {
		return content
	}
	if end := strings.Index(content[4:], "\n---\n"); end >= 0 {
		frontmatter := content[4 : 4+end]
		if strings.Contains(frontmatter, "title:") {
			return content[4+end+5:]
		}
	}
	return content
}

func uploadOutputs(ctx context.Context, client *notion.Client, projectID, root string, revision int) error {
	return filepath.WalkDir(filepath.Join(root, "outputs"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(filepath.Join(root, "outputs"), path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "theme/") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		contentType := "application/octet-stream"
		if strings.EqualFold(filepath.Ext(path), ".pdf") {
			contentType = "application/pdf"
		}
		if strings.EqualFold(filepath.Ext(path), ".xlsx") {
			contentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
		}
		upload, err := client.Upload(ctx, outputTitle(rel), contentType, data)
		if err != nil {
			return err
		}
		return client.Append(ctx, projectID, []map[string]any{notion.FileBlock(upload.ID, "")})
	})
}

func outputTitle(path string) string {
	text := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return prettyName(text) + filepath.Ext(path)
}

func replaceFromArchive(root string, data []byte) error {
	staging, err := os.MkdirTemp(filepath.Dir(root), ".stamp-notion-pull-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := bundle.UnpackReader(bytes.NewReader(data), int64(len(data)), staging); err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() != ".stamp" {
			if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
				return err
			}
		}
	}
	staged, err := os.ReadDir(staging)
	if err != nil {
		return err
	}
	for _, entry := range staged {
		if entry.Name() != ".stamp" {
			if err := os.Rename(filepath.Join(staging, entry.Name()), filepath.Join(root, entry.Name())); err != nil {
				return err
			}
		}
	}
	return project.EnsureAgentCompatibility(root)
}
