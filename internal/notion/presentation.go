package notion

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path"
	"strings"
	"time"
	"unicode"
)

const detailsTitle = "Stamp details"
const projectFileTitle = "Project File"

func description(kind string) string {
	switch kind {
	case "project v1":
		return "A shared collection of documents, presentations, and spreadsheets. Browse the published files below, or download the project to work in Stamp."
	case "renders v1":
		return "The latest published files, organized just like your Stamp project. Open a document to read or download it."
	case "revisions v1":
		return "Previous project versions, kept for recovery. Each archive contains a complete Stamp workspace."
	case "folder v1":
		return "Published files in this folder."
	}
	return "Stamp managed: " + kind
}

func documentTitle(filename string) string {
	value := strings.TrimSuffix(filename, path.Ext(filename))
	value = strings.ReplaceAll(strings.ReplaceAll(value, "-", " "), "_", " ")
	runes := []rune(value)
	if len(runes) > 0 {
		runes[0] = unicode.ToUpper(runes[0])
	}
	return string(runes)
}

func (c *Client) pageIcon(ctx context.Context, id, icon string) error {
	page, err := c.Page(ctx, id)
	if err != nil {
		return err
	}
	if page["icon"] != nil {
		return nil
	} // Defaults never replace a person's choice.
	_, err = c.Call(ctx, "PATCH", "pages/"+id, Object{"icon": Object{"type": "emoji", "emoji": icon}})
	return err
}

func isDetails(block Object) bool { return isNamedToggle(block, detailsTitle) }

func isNamedToggle(block Object, title string) bool {
	if String(block, "type") != "toggle" {
		return false
	}
	toggle, _ := block["toggle"].(map[string]any)
	texts, _ := toggle["rich_text"].([]any)
	if len(texts) != 1 {
		return false
	}
	text, _ := texts[0].(map[string]any)
	return text["plain_text"] == title
}

// metadataChildren reads only our explicitly named details sections. It does
// not interpret arbitrary user toggles or descend into unrelated pages.
func (c *Client) metadataChildren(ctx context.Context, page string) ([]Object, error) {
	blocks, err := c.Children(ctx, page)
	if err != nil {
		return nil, err
	}
	result := append([]Object(nil), blocks...)
	for _, block := range blocks {
		if isDetails(block) || isNamedToggle(block, projectFileTitle) {
			children, err := c.Children(ctx, String(block, "id"))
			if err != nil {
				return nil, err
			}
			result = append(result, children...)
		}
	}
	return result, nil
}

func (c *Client) wrapMetadata(ctx context.Context, page string, block Object) (string, error) {
	p, _ := block["parent"].(map[string]any)
	if p["block_id"] != "" && p["block_id"] != nil {
		return String(block, "id"), nil
	}
	child := jsonBlock(map[string]any{}, caption(block))
	child["code"].(Object)["rich_text"] = jsonText(codeText(block))
	return c.appendDetails(ctx, page, child)
}

func (c *Client) appendDetails(ctx context.Context, page string, child Object, titles ...string) (string, error) {
	title := detailsTitle
	if len(titles) > 0 {
		title = titles[0]
	}
	result, err := c.Append(ctx, page, Object{"object": "block", "type": "toggle", "toggle": Object{
		"rich_text": Text(title), "color": "gray", "children": []Object{child},
	}})
	if err != nil {
		return "", err
	}
	blocks, _ := result["results"].([]any)
	if len(blocks) != 1 {
		return "", errors.New("Notion returned no details section")
	}
	toggle, _ := blocks[0].(map[string]any)
	children, err := c.Children(ctx, String(Object(toggle), "id"))
	if err != nil {
		return "", err
	}
	if len(children) != 1 {
		return "", errors.New("Notion returned no metadata inside details")
	}
	return String(children[0], "id"), nil
}

// Style upgrades the first bare layout while preserving page, PDF, and archive
// identities. Subsequent pushes don't restyle existing pages or overwrite icons.
func (c *Client) Style(ctx context.Context, s Snapshot) (Snapshot, error) {
	if err := c.Preflight(ctx, s); err != nil {
		return s, err
	}
	for _, item := range []struct{ id, kind, icon string }{{s.PageID, "project v1", "🔖"}, {s.CurrentID, "renders v1", "📂"}, {s.HistoryID, "revisions v1", "🕘"}} {
		if err := c.pageIcon(ctx, item.id, item.icon); err != nil {
			return s, err
		}
		children, err := c.Children(ctx, item.id)
		if err != nil {
			return s, err
		}
		for _, child := range children {
			if isMarker(child, item.kind) {
				if _, err = c.UpdateBlock(ctx, String(child, "id"), Object{"paragraph": marker(item.kind)["paragraph"]}); err != nil {
					return s, err
				}
			}
		}
	}
	for id, title := range map[string]string{s.CurrentID: "Published files", s.HistoryID: "Version history"} {
		page, err := c.Page(ctx, id)
		if err != nil {
			return s, err
		}
		if old := PageName(page); old == "Current" || old == "Stamp revisions" {
			if _, err = c.Call(ctx, "PATCH", "pages/"+id, Object{"properties": Title(title)}); err != nil {
				return s, err
			}
		}
	}
	inventory, err := c.readCatalog(ctx, s)
	if err != nil {
		return s, err
	}
	for _, id := range inventory.Folders {
		if err = c.pageIcon(ctx, id, "📁"); err != nil {
			return s, err
		}
		blocks, err := c.Children(ctx, id)
		if err != nil {
			return s, err
		}
		for _, b := range blocks {
			if isMarker(b, "folder v1") {
				if _, err = c.UpdateBlock(ctx, String(b, "id"), Object{"paragraph": marker("folder v1")["paragraph"]}); err != nil {
					return s, err
				}
			}
		}
	}
	for key, file := range inventory.Files {
		icon := "📄"
		if strings.HasSuffix(key, ".xlsx") {
			icon = "📊"
		}
		if err = c.pageIcon(ctx, file.PageID, icon); err != nil {
			return s, err
		}
		page, err := c.Page(ctx, file.PageID)
		if err != nil {
			return s, err
		}
		if PageName(page) == path.Base(key) {
			if _, err = c.Call(ctx, "PATCH", "pages/"+file.PageID, Object{"properties": Title(documentTitle(path.Base(key)))}); err != nil {
				return s, err
			}
		}
	}
	oldCatalog, err := c.Block(ctx, s.CatalogID)
	if err != nil {
		return s, err
	}
	newCatalog, err := c.wrapMetadata(ctx, s.CurrentID, oldCatalog)
	if err != nil {
		return s, err
	}
	data := jsonBlock(Object{"stamp": 1, "current": s.CurrentID, "history": s.HistoryID, "catalog": newCatalog, "archive": s.ArchiveID}, "Stamp project metadata")
	if _, err = c.UpdateBlock(ctx, s.MetadataID, Object{"code": data["code"]}); err != nil {
		return s, err
	}
	// Retrieve the updated representation before placing it under the toggle.
	oldMeta, err := c.Block(ctx, s.MetadataID)
	if err != nil {
		return s, err
	}
	newMeta, err := c.wrapMetadata(ctx, s.PageID, oldMeta)
	if err != nil {
		return s, err
	}
	for _, pair := range [][2]string{{s.MetadataID, newMeta}, {s.CatalogID, newCatalog}} {
		if pair[0] != pair[1] {
			if _, err = c.UpdateBlock(ctx, pair[0], Object{"in_trash": true}); err != nil {
				return s, err
			}
		}
	}
	if err := c.StyleHistory(ctx, s); err != nil {
		return s, err
	}
	return c.Inspect(ctx, s.PageID)
}

func divider() Object {
	return Object{"object": "block", "type": "divider", "divider": Object{}}
}

// Display metadata comes from the archive itself, so history shows the original
// version date even when an older archive is published again.
func revisionQuote(data []byte, fallback time.Time) Object {
	var info struct {
		Message   string `json:"message"`
		CreatedAt string `json:"createdAt"`
	}
	if archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data))); err == nil {
		for _, file := range archive.File {
			if file.Name != ".stamp/version.json" {
				continue
			}
			if reader, err := file.Open(); err == nil {
				_ = json.NewDecoder(io.LimitReader(reader, 64*1024)).Decode(&info)
				reader.Close()
			}
			break
		}
	}
	title := strings.TrimSpace(info.Message)
	if title == "" {
		title = "Project version"
	}
	// Bound user-authored messages to Notion's per-rich-text limit.
	if runes := []rune(title); len(runes) > 1800 {
		title = string(runes[:1800]) + "…"
	}
	date := fallback
	if parsed, err := time.Parse(time.RFC3339Nano, info.CreatedAt); err == nil {
		date = parsed
	}
	titleText := Text("\nMessage: " + title)
	titleText[0]["annotations"] = Object{"color": "gray"}
	dateText := Text(date.UTC().Format("2 January 2006 · 15:04 UTC"))
	dateText[0]["annotations"] = Object{"bold": true}
	return Object{"object": "block", "type": "quote", "quote": Object{"rich_text": append(dateText, titleText...)}}
}

// StyleHistory inserts presentation blocks without replacing immutable archives.
func (c *Client) StyleHistory(ctx context.Context, s Snapshot) error {
	blocks, err := c.Children(ctx, s.HistoryID)
	if err != nil {
		return err
	}
	known := map[string]Revision{}
	for _, revision := range s.Revisions {
		known[revision.BlockID] = revision
	}
	count := 0
	for i, block := range blocks {
		revision, managed := known[String(block, "id")]
		if !managed {
			continue
		}
		count++
		data, err := c.Download(ctx, String(block, "id"))
		if err != nil {
			return err
		}
		created, err := time.Parse(time.RFC3339Nano, String(block, "created_time"))
		if err != nil {
			return err
		}
		if i > 0 && String(blocks[i-1], "type") == "quote" {
			quote := revisionQuote(data, created)
			if _, err := c.UpdateBlock(ctx, String(blocks[i-1], "id"), Object{"quote": quote["quote"]}); err != nil {
				return err
			}
			if err := c.hideRevision(ctx, s.HistoryID, revision); err != nil {
				return err
			}
			continue
		}
		children := []Object{}
		if count > 1 {
			children = append(children, divider())
		}
		children = append(children, revisionQuote(data, created))
		position := Object{"type": "start"}
		if i > 0 {
			position = Object{"type": "after_block", "after_block": Object{"id": String(blocks[i-1], "id")}}
		}
		if _, err := c.Call(ctx, "PATCH", "blocks/"+s.HistoryID+"/children", Object{"children": children, "position": position}); err != nil {
			return err
		}
		if err := c.hideRevision(ctx, s.HistoryID, revision); err != nil {
			return err
		}
	}
	return nil
}

func howToUse() Object {
	return Object{"object": "block", "type": "toggle", "toggle": Object{
		"rich_text": Text("How to use this"), "color": "gray",
		"children": []Object{{"object": "block", "type": "paragraph", "paragraph": Object{
			"rich_text": []Object{{"type": "text", "text": Object{"content": "Stamp on GitHub", "link": Object{"url": "https://github.com/jhleao/stamp"}}}},
		}}},
	}}
}
