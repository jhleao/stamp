package notion

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
)

type Revision struct {
	ID      string   `json:"id"`
	Parents []string `json:"parents"`
	BlockID string   `json:"-"`
}
type Snapshot struct {
	PageID        string
	ProjectFileID string
	HistoryID     string
	ArchiveID     string
	CurrentID     string
	CatalogID     string
	MetadataID    string
	Name          string
	URL           string
	Revisions     []Revision
	Heads         []Revision
	Lease         string
}
type Output struct {
	Path string
	MIME string
	Data []byte
}
type publishedFile struct {
	PageID  string `json:"pageId"`
	BlockID string `json:"blockId"`
	Path    string `json:"path"`
}

func Digest(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }
func caption(block Object) string {
	media, _ := block[String(block, "type")].(map[string]any)
	texts, _ := media["caption"].([]any)
	var result strings.Builder
	for _, item := range texts {
		obj, _ := item.(map[string]any)
		if s, _ := obj["plain_text"].(string); s != "" {
			result.WriteString(s)
		} else if text, ok := obj["text"].(map[string]any); ok {
			s, _ := text["content"].(string)
			result.WriteString(s)
		}
	}
	return result.String()
}
func marker(kind string) Object {
	return Object{"object": "block", "type": "paragraph", "paragraph": Object{"rich_text": Text(description(kind)), "color": "gray"}}
}
func PageName(page Object) string {
	props, _ := page["properties"].(map[string]any)
	for _, p := range props {
		prop, _ := p.(map[string]any)
		titles, _ := prop["title"].([]any)
		var b strings.Builder
		for _, t := range titles {
			obj, _ := t.(map[string]any)
			b.WriteString(String(Object(obj), "plain_text"))
		}
		if b.Len() > 0 {
			return b.String()
		}
	}
	return "Stamp project"
}

// Create makes a dedicated subtree. It never adopts or wipes an existing page.
func (c *Client) Create(ctx context.Context, parent, name string) (Snapshot, error) {
	page, err := c.CreatePage(ctx, parent, name, []Object{projectIntro(name)}, "🔖")
	if err != nil {
		return Snapshot{}, err
	}
	s := Snapshot{PageID: String(page, "id"), Name: name, URL: String(page, "url")}
	current, err := c.CreatePage(ctx, s.PageID, "Published files", []Object{marker("renders v1")}, "📂")
	if err != nil {
		return s, err
	}
	s.CurrentID = String(current, "id")
	s.CatalogID, err = c.appendDetails(ctx, s.CurrentID, jsonBlock(catalog{Files: map[string]publishedFile{}, Folders: map[string]string{}}, "Stamp output catalog"))
	if err != nil {
		return s, err
	}
	history, err := c.CreatePage(ctx, s.PageID, "Version history", []Object{marker("revisions v1")}, "🕘")
	if err != nil {
		return s, err
	}
	s.HistoryID = String(history, "id")
	if _, err = c.Append(ctx, s.PageID, howToUse()); err != nil {
		return s, err
	}
	s.MetadataID, err = c.appendDetails(ctx, s.PageID, jsonBlock(Object{"stamp": 1, "current": s.CurrentID, "history": s.HistoryID, "catalog": s.CatalogID}, "Stamp project metadata"), projectFileTitle)
	if err != nil {
		return s, err
	}
	return c.Inspect(ctx, s.PageID)
}

func (c *Client) Inspect(ctx context.Context, id string) (Snapshot, error) {
	page, err := c.Page(ctx, id)
	if err != nil {
		return Snapshot{}, err
	}
	if trashed, _ := page["in_trash"].(bool); trashed {
		return Snapshot{}, errors.New("Notion project is in trash")
	}
	s := Snapshot{PageID: id, Name: PageName(page), URL: String(page, "url")}
	blocks, err := c.metadataChildren(ctx, id)
	if err != nil {
		return s, err
	}
	for _, b := range blocks {
		if isNamedToggle(b, projectFileTitle) {
			if s.ProjectFileID != "" {
				return s, errors.New("multiple Project File toggles")
			}
			s.ProjectFileID = String(b, "id")
		}
		if String(b, "type") == "file" && caption(b) == "Stamp current archive" {
			if s.ArchiveID != "" && s.ArchiveID != String(b, "id") {
				return s, errors.New("multiple Stamp archive blocks")
			}
			s.ArchiveID = String(b, "id")
		}
		if String(b, "type") != "code" || caption(b) != "Stamp project metadata" {
			continue
		}
		if s.HistoryID != "" {
			return s, errors.New("multiple Stamp metadata blocks")
		}
		code, _ := b["code"].(map[string]any)
		texts, _ := code["rich_text"].([]any)
		var raw strings.Builder
		for _, t := range texts {
			obj, _ := t.(map[string]any)
			raw.WriteString(String(Object(obj), "plain_text"))
		}
		var meta struct {
			Stamp   int    `json:"stamp"`
			Current string `json:"current"`
			History string `json:"history"`
			Catalog string `json:"catalog"`
			Archive string `json:"archive"`
		}
		if err := json.Unmarshal([]byte(raw.String()), &meta); err != nil || meta.Stamp != 1 {
			return s, errors.New("invalid Stamp Notion metadata")
		}
		s.CurrentID, s.HistoryID, s.CatalogID = meta.Current, meta.History, meta.Catalog
		s.MetadataID = String(b, "id")
		if meta.Archive != "" {
			if s.ArchiveID != "" && s.ArchiveID != meta.Archive {
				return s, errors.New("Stamp archive identity changed")
			}
			s.ArchiveID = meta.Archive
		}
	}
	if s.CurrentID == "" || s.HistoryID == "" || s.CatalogID == "" {
		return s, errors.New("selected Notion page is not a Stamp project")
	}
	for _, child := range []string{s.CurrentID, s.HistoryID} {
		if err := c.verifyParent(ctx, child, id); err != nil {
			return s, err
		}
	}
	if s.ArchiveID != "" {
		archive, err := c.Block(ctx, s.ArchiveID)
		if err != nil {
			return s, err
		}
		p, _ := archive["parent"].(map[string]any)
		validParent := p["page_id"] == s.PageID || (s.ProjectFileID != "" && p["block_id"] == s.ProjectFileID)
		if !validParent || caption(archive) != "Stamp current archive" {
			return s, errors.New("Stamp current archive was moved or changed")
		}
	}
	s.Revisions, err = c.readRevisions(ctx, s.HistoryID)
	if err != nil {
		return s, err
	}
	s.Heads, s.Lease, err = RevisionHeads(s.Revisions)
	return s, err
}

// RevisionHeads uses an append-only graph, because Notion provides no
// compare-and-swap primitive. Concurrent children remain visible as conflicts.
func RevisionHeads(revisions []Revision) ([]Revision, string, error) {
	nodes := map[string]Revision{}
	parents := map[string]bool{}
	for _, r := range revisions {
		if _, ok := nodes[r.ID]; ok {
			return nil, "", errors.New("duplicate Stamp revision identity")
		}
		nodes[r.ID] = r
	}
	for _, r := range revisions {
		for _, p := range r.Parents {
			if _, ok := nodes[p]; !ok {
				return nil, "", errors.New("Stamp revision history is incomplete or inaccessible")
			}
			if p == r.ID {
				return nil, "", errors.New("Stamp revision refers to itself")
			}
			parents[p] = true
		}
	}
	// Reject cycles, including disconnected cycles that would otherwise vanish.
	colors := map[string]int{}
	var visit func(string) error
	visit = func(id string) error {
		if colors[id] == 1 {
			return errors.New("cyclic Stamp revision history")
		}
		if colors[id] == 2 {
			return nil
		}
		colors[id] = 1
		for _, p := range nodes[id].Parents {
			if err := visit(p); err != nil {
				return err
			}
		}
		colors[id] = 2
		return nil
	}
	for id := range nodes {
		if err := visit(id); err != nil {
			return nil, "", err
		}
	}
	var heads []Revision
	for id, r := range nodes {
		if !parents[id] {
			heads = append(heads, r)
		}
	}
	sort.Slice(heads, func(i, j int) bool { return heads[i].ID < heads[j].ID })
	if len(heads) == 0 {
		return heads, "", nil
	}
	if len(heads) == 1 {
		return heads, heads[0].ID, nil
	}
	ids := make([]string, len(heads))
	for i, h := range heads {
		ids[i] = h.ID
	}
	return heads, "conflict-" + Digest([]byte(strings.Join(ids, "\n"))), nil
}
func (c *Client) Contents(ctx context.Context, s Snapshot) ([]byte, error) {
	if _, err := c.verifyCatalog(ctx, s); err != nil {
		return nil, err
	}
	if len(s.Heads) != 1 {
		return nil, fmt.Errorf("Notion has %d current revisions; review Version history and resolve with push --force-with-lease %s", len(s.Heads), s.Lease)
	}
	data, err := c.Download(ctx, s.Heads[0].BlockID)
	if err != nil {
		return nil, err
	}
	if Digest(data) != s.Heads[0].ID {
		return nil, errors.New("Notion archive does not match its revision hash")
	}
	return data, nil
}
func (c *Client) Commit(ctx context.Context, s Snapshot, data []byte) (Revision, error) {
	r := Revision{ID: Digest(data)}
	for _, h := range s.Heads {
		r.Parents = append(r.Parents, h.ID)
	}
	raw, _ := json.Marshal(r)
	if len(raw) > 1900 {
		return r, errors.New("too many conflicting revisions to resolve in one push")
	}
	upload, err := c.Upload(ctx, s.Name+".stamp.zip", "application/zip", data)
	if err != nil {
		return r, err
	}
	latest, err := c.Inspect(ctx, s.PageID)
	if err != nil {
		return r, err
	}
	if latest.Lease != s.Lease {
		return r, errors.New("Notion advanced during upload; pull and retry")
	}
	blocks := []Object{}
	if len(s.Revisions) > 0 {
		blocks = append(blocks, divider())
	}
	blocks = append(blocks, revisionQuote(data, time.Now().UTC()), FileBlock("file", upload, s.Name+".stamp", "stamp-revision:"+string(raw)))
	_, err = c.Append(ctx, s.HistoryID, blocks...)
	if err != nil {
		return r, err
	}
	latest, err = c.Inspect(ctx, s.PageID)
	if err != nil {
		return r, err
	}
	if latest.Lease != r.ID {
		return r, fmt.Errorf("concurrent Notion push detected; your archive is retained in Version history; review lease %s", latest.Lease)
	}
	if err := c.hideRevision(ctx, s.HistoryID, latest.Heads[0]); err != nil {
		return r, fmt.Errorf("archive retained but revision details could not be collapsed: %w", err)
	}
	block := FileBlock("file", upload, s.Name+".stamp", "Stamp current archive")
	if s.ArchiveID == "" {
		var result Object
		parent := s.PageID
		position := Object{"type": "after_block", "after_block": Object{"id": s.HistoryID}}
		if s.ProjectFileID != "" {
			parent = s.ProjectFileID
			position = Object{"type": "start"}
		}
		result, err = c.Call(ctx, "PATCH", "blocks/"+parent+"/children", Object{"children": []Object{block}, "position": position})
		if err == nil {
			results, _ := result["results"].([]any)
			for _, result := range results {
				created, _ := result.(map[string]any)
				block := Object(created)
				if String(block, "type") == "file" && caption(block) == "Stamp current archive" {
					if s.ArchiveID != "" {
						return r, errors.New("Notion returned duplicate current archives")
					}
					s.ArchiveID = String(block, "id")
				}
			}
			if s.ArchiveID == "" {
				err = errors.New("Notion returned no current archive block")
			}
		}
	} else {
		_, err = c.UpdateBlock(ctx, s.ArchiveID, Object{"file": block["file"]})
	}
	if err == nil {
		metadata := jsonBlock(Object{"stamp": 1, "current": s.CurrentID, "history": s.HistoryID, "catalog": s.CatalogID, "archive": s.ArchiveID}, "Stamp project metadata")
		_, err = c.UpdateBlock(ctx, s.MetadataID, Object{"code": metadata["code"]})
	}
	if err != nil {
		return r, fmt.Errorf("archive %s was retained, but the current attachment update failed; review and retry with --force-with-lease %s: %w", r.ID, r.ID, err)
	}
	return r, nil
}
func (c *Client) verifyParent(ctx context.Context, id, parent string) error {
	page, err := c.Page(ctx, id)
	if err != nil {
		return err
	}
	if trash, _ := page["in_trash"].(bool); trash {
		return errors.New("managed Notion page is in trash; restore it before syncing")
	}
	p, _ := page["parent"].(map[string]any)
	if p["page_id"] != parent {
		return errors.New("managed Notion page was moved outside its expected parent")
	}
	return nil
}

// Publish touches only the attachment identified by a Stamp caption. User
// notes and other attachments are left intact. Removed outputs are archived.
func (c *Client) Publish(ctx context.Context, s Snapshot, outputs []Output) error {
	inventory, err := c.verifyCatalog(ctx, s)
	if err != nil {
		return err
	}
	folderPaths := map[string]string{}
	for key, id := range inventory.Folders {
		folderPaths[id] = key
	}
	existing := map[string]publishedFile{}
	next := map[string]publishedFile{}
	folders := map[string]string{".": s.CurrentID}
	var walk func(string, string) error
	walk = func(id, prefix string) error {
		children, err := c.Children(ctx, id)
		if err != nil {
			return err
		}
		for _, b := range children {
			if String(b, "type") != "child_page" {
				continue
			}
			pageID := String(b, "id")
			blocks, err := c.Children(ctx, pageID)
			if err != nil {
				return err
			}
			managed := false
			for _, child := range blocks {
				cap := caption(child)
				if strings.HasPrefix(cap, "stamp-output:") {
					key := strings.TrimPrefix(cap, "stamp-output:")
					if path.Dir(key) != prefix {
						return errors.New("managed Notion output was moved")
					}
					if _, ok := existing[key]; ok {
						return fmt.Errorf("duplicate managed Notion output %q", key)
					}
					existing[key] = publishedFile{PageID: pageID, BlockID: String(child, "id"), Path: key}
					managed = true
				}
			}
			if !managed {
				cp, _ := b["child_page"].(map[string]any)
				name, _ := cp["title"].(string)
				// Only descend into folders carrying our explicit ownership marker.
				for _, child := range blocks {
					if isMarker(child, "folder v1") {
						key := folderPaths[pageID]
						if key == "" {
							key = path.Join(prefix, name)
						}
						if _, ok := folders[key]; ok {
							return fmt.Errorf("duplicate Notion folder %q", key)
						}
						folders[key] = pageID
						if err := walk(pageID, key); err != nil {
							return err
						}
						break
					}
				}
			}
		}
		return nil
	}
	if err := walk(s.CurrentID, "."); err != nil {
		return err
	}
	var folder func(string) (string, error)
	folder = func(key string) (string, error) {
		if id := folders[key]; id != "" {
			return id, nil
		}
		parent, err := folder(path.Dir(key))
		if err != nil {
			return "", err
		}
		p, err := c.CreatePage(ctx, parent, path.Base(key), []Object{marker("folder v1")})
		if err != nil {
			return "", err
		}
		id := String(p, "id")
		folders[key] = id
		return id, nil
	}
	for _, output := range outputs {
		if output.Path == "" || path.IsAbs(output.Path) || path.Clean(output.Path) != output.Path || strings.HasPrefix(output.Path, "../") {
			return errors.New("invalid rendered output path")
		}
		upload, err := c.Upload(ctx, path.Base(output.Path), output.MIME, output.Data)
		if err != nil {
			return err
		}
		kind := "file"
		if output.MIME == "application/pdf" {
			kind = "pdf"
		}
		block := FileBlock(kind, upload, path.Base(output.Path), "stamp-output:"+output.Path)
		if old, ok := existing[output.Path]; ok {
			if _, err = c.UpdateBlock(ctx, old.BlockID, Object{kind: block[kind]}); err != nil {
				return err
			}
			next[output.Path] = old
			delete(existing, output.Path)
		} else {
			parent, err := folder(path.Dir(output.Path))
			if err != nil {
				return err
			}
			page, err := c.CreatePage(ctx, parent, documentTitle(path.Base(output.Path)), []Object{block})
			if err != nil {
				return err
			}
			pageID := String(page, "id")
			blocks, err := c.Children(ctx, pageID)
			if err != nil {
				return err
			}
			for _, child := range blocks {
				if caption(child) == "stamp-output:"+output.Path {
					next[output.Path] = publishedFile{PageID: pageID, BlockID: String(child, "id"), Path: output.Path}
				}
			}
			if next[output.Path].BlockID == "" {
				return errors.New("new Notion output has no attachment")
			}
		}
	}
	// Remove references before archiving. A failed archive is rediscovered by
	// ownership marker on retry, while an inaccessible known page fails preflight.
	if err := c.writeCatalog(ctx, s, next, folders); err != nil {
		return err
	}
	for _, old := range existing {
		if err := c.TrashPage(ctx, old.PageID); err != nil {
			return err
		}
	}
	// Remove empty managed directories deepest first. Keep a directory that
	// contains notes or any unrecognized child, even if its last output was removed.
	directories := make([]string, 0, len(folders))
	for key := range folders {
		if key != "." {
			directories = append(directories, key)
		}
	}
	sort.Slice(directories, func(i, j int) bool { return strings.Count(directories[i], "/") > strings.Count(directories[j], "/") })
	for _, key := range directories {
		needed := false
		for file := range next {
			if strings.HasPrefix(file, key+"/") {
				needed = true
				break
			}
		}
		if needed {
			continue
		}
		blocks, err := c.Children(ctx, folders[key])
		if err != nil {
			return err
		}
		empty := true
		for _, block := range blocks {
			if !isMarker(block, "folder v1") {
				empty = false
				break
			}
		}
		if !empty {
			continue
		}
		id := folders[key]
		delete(folders, key)
		if err := c.writeCatalog(ctx, s, next, folders); err != nil {
			return err
		}
		if err := c.TrashPage(ctx, id); err != nil {
			return err
		}
	}
	return nil
}
func isMarker(block Object, value string) bool {
	p, _ := block["paragraph"].(map[string]any)
	texts, _ := p["rich_text"].([]any)
	if len(texts) != 1 {
		return false
	}
	t, _ := texts[0].(map[string]any)
	return t["plain_text"] == "Stamp managed: "+value || t["plain_text"] == description(value)
}
