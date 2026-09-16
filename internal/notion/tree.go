package notion

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"path"
	"sort"
	"strings"
)

func hasParent(block Object, id string) bool {
	parent, _ := block["parent"].(map[string]any)
	return parent["page_id"] == id || parent["block_id"] == id
}

func toggleTitle(block Object) string {
	toggle, _ := block["toggle"].(map[string]any)
	texts, _ := toggle["rich_text"].([]any)
	var title strings.Builder
	for _, t := range texts {
		item, _ := t.(map[string]any)
		title.WriteString(String(Object(item), "plain_text"))
	}
	return title.String()
}

func (c *Client) createToggle(ctx context.Context, parent, title string, children []Object, after ...string) (Object, error) {
	toggle := Object{"rich_text": Text(title)}
	if len(children) > 0 {
		toggle["children"] = children
	}
	body := Object{"children": []Object{{"object": "block", "type": "toggle", "toggle": toggle}}}
	if len(after) > 0 {
		body["position"] = Object{"type": "after_block", "after_block": Object{"id": after[0]}}
	}
	result, err := c.Call(ctx, "PATCH", "blocks/"+parent+"/children", body)
	if err != nil {
		return nil, err
	}
	blocks, _ := result["results"].([]any)
	if len(blocks) == 0 {
		return nil, errors.New("Notion returned no new tree toggle")
	}
	block, _ := blocks[0].(map[string]any)
	return Object(block), nil
}

func (c *Client) trashBlock(ctx context.Context, id string) error {
	_, err := c.UpdateBlock(ctx, id, Object{"in_trash": true})
	return err
}

// MigratePublishedTree changes only the presentation of existing hosted outputs.
// The canonical project archive and revision graph are never republished.
func (c *Client) MigratePublishedTree(ctx context.Context, s Snapshot) error {
	current, err := c.Block(ctx, s.CurrentID)
	if err != nil {
		return err
	}
	if String(current, "type") == "toggle" {
		return c.Preflight(ctx, s)
	}
	inventory, err := c.verifyCatalog(ctx, s)
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(inventory.Files))
	for key := range inventory.Files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	outputs := make([]Output, 0, len(keys))
	for _, key := range keys {
		data, err := c.Download(ctx, inventory.Files[key].BlockID)
		if err != nil {
			return err
		}
		contentType := mime.TypeByExtension(path.Ext(key))
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		outputs = append(outputs, Output{Path: key, MIME: contentType, Data: data})
	}
	return c.migrateTree(ctx, s, outputs)
}

// Build and verify the replacement before switching the project's pointer.
// On failure the original tree remains intact. Staging blocks are recoverable
// in Notion rather than deleting a possibly committed replacement on timeout.
func (c *Client) migrateTree(ctx context.Context, s Snapshot, outputs []Output) error {
	inventory, err := c.verifyCatalog(ctx, s)
	if err != nil {
		return err
	}
	if err = c.checkLegacyTree(ctx, s, inventory); err != nil {
		return err
	}
	staged := s
	current, err := c.createToggle(ctx, s.PageID, "📂 Published files", nil, s.CurrentID)
	if err != nil {
		return err
	}
	staged.CurrentID = String(current, "id")
	staged.CatalogID, err = c.appendDetails(ctx, staged.CurrentID, jsonBlock(catalog{Files: map[string]publishedFile{}, Folders: map[string]string{}}, "Stamp output catalog"))
	if err != nil {
		return err
	}
	if err = c.publishTree(ctx, staged, outputs); err != nil {
		return err
	}
	if err = c.Preflight(ctx, staged); err != nil {
		return err
	}
	latest, err := c.Inspect(ctx, s.PageID)
	if err != nil {
		return err
	}
	if latest.CurrentID != s.CurrentID || latest.CatalogID != s.CatalogID || latest.Lease != s.Lease {
		return errors.New("Notion project changed during tree migration; original presentation retained")
	}
	metadata := jsonBlock(Object{"stamp": 1, "current": staged.CurrentID, "history": latest.HistoryID, "catalog": staged.CatalogID, "archive": latest.ArchiveID}, "Stamp project metadata")
	if _, err = c.UpdateBlock(ctx, latest.MetadataID, Object{"code": metadata["code"]}); err != nil {
		return err
	}
	return c.TrashPage(ctx, s.CurrentID)
}

// Do not hide user-authored notes when retiring the old managed subtree.
func (c *Client) checkLegacyTree(ctx context.Context, s Snapshot, inventory catalog) error {
	allowed := map[string]bool{s.CatalogID: true}
	containers := []string{s.CurrentID}
	for _, id := range inventory.Folders {
		allowed[id] = true
		containers = append(containers, id)
	}
	for _, file := range inventory.Files {
		allowed[file.PageID] = true
		allowed[file.BlockID] = true
		containers = append(containers, file.PageID)
	}
	for _, id := range containers {
		blocks, err := c.Children(ctx, id)
		if err != nil {
			return err
		}
		for _, block := range blocks {
			if allowed[String(block, "id")] || isMarker(block, "folder v1") || isMarker(block, "renders v1") {
				continue
			}
			if isDetails(block) {
				children, err := c.Children(ctx, String(block, "id"))
				if err != nil {
					return err
				}
				if len(children) == 1 && String(children[0], "id") == s.CatalogID {
					continue
				}
			}
			return fmt.Errorf("published pages contain additional content at %s; preserve it before migrating to toggles", id)
		}
	}
	return nil
}
