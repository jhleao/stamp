package notion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
)

type catalog struct {
	Files   map[string]publishedFile `json:"files"`
	Folders map[string]string        `json:"folders"`
}

func jsonBlock(value any, caption string) Object {
	data, _ := json.Marshal(value)
	return Object{"object": "block", "type": "code", "code": Object{"language": "json", "rich_text": jsonText(string(data)), "caption": Text(caption)}}
}
func jsonText(value string) []Object {
	var result []Object
	// JSON is ASCII for our IDs and escaping may contain Unicode paths; chunk by
	// rune count to preserve UTF-8 while staying below Notion's text limit.
	runes := []rune(value)
	for len(runes) > 0 {
		n := 1800
		if len(runes) < n {
			n = len(runes)
		}
		result = append(result, Text(string(runes[:n]))...)
		runes = runes[n:]
	}
	return result
}
func codeText(block Object) string {
	code, _ := block["code"].(map[string]any)
	texts, _ := code["rich_text"].([]any)
	var raw strings.Builder
	for _, text := range texts {
		o, _ := text.(map[string]any)
		raw.WriteString(String(Object(o), "plain_text"))
	}
	return raw.String()
}
func (c *Client) readCatalog(ctx context.Context, s Snapshot) (catalog, error) {
	block, err := c.Block(ctx, s.CatalogID)
	if err != nil {
		return catalog{}, err
	}
	parent, _ := block["parent"].(map[string]any)
	validParent := hasParent(block, s.CurrentID)
	if toggleID, ok := parent["block_id"].(string); ok {
		toggle, err := c.Block(ctx, toggleID)
		if err != nil {
			return catalog{}, err
		}
		validParent = validParent || (hasParent(toggle, s.CurrentID) && isDetails(toggle))
	}
	if !validParent || caption(block) != "Stamp output catalog" {
		return catalog{}, errors.New("Stamp output catalog was moved or changed")
	}
	var result catalog
	if err = json.Unmarshal([]byte(codeText(block)), &result); err != nil {
		return result, fmt.Errorf("invalid Stamp output catalog: %w", err)
	}
	if result.Files == nil || result.Folders == nil {
		return result, errors.New("incomplete Stamp output catalog")
	}
	return result, nil
}
func (c *Client) verifyCatalog(ctx context.Context, s Snapshot) (catalog, error) {
	result, err := c.readCatalog(ctx, s)
	if err != nil {
		return result, err
	}
	parentFor := func(key string) (string, error) {
		if key == "." {
			return s.CurrentID, nil
		}
		id := result.Folders[key]
		if id == "" {
			return "", errors.New("Stamp output catalog has a missing parent")
		}
		return id, nil
	}
	for key, id := range result.Folders {
		if key == "." || path.Clean(key) != key || strings.HasPrefix(key, "../") || path.IsAbs(key) {
			return result, errors.New("invalid Stamp folder path")
		}
		parent, err := parentFor(path.Dir(key))
		if err != nil {
			return result, err
		}
		if err = c.verifyParent(ctx, id, parent); err != nil {
			return result, err
		}
	}
	for key, file := range result.Files {
		parent, err := parentFor(path.Dir(key))
		if err != nil {
			return result, err
		}
		if err = c.verifyParent(ctx, file.PageID, parent); err != nil {
			return result, err
		}
		block, err := c.Block(ctx, file.BlockID)
		if err != nil {
			return result, err
		}
		if !hasParent(block, file.PageID) || caption(block) != "stamp-output:"+key {
			return result, errors.New("managed Notion attachment was moved or changed")
		}
	}
	return result, nil
}
func (c *Client) writeCatalog(ctx context.Context, s Snapshot, files map[string]publishedFile, folders map[string]string) error {
	ownedFolders := map[string]string{}
	for key, id := range folders {
		if key != "." {
			ownedFolders[key] = id
		}
	}
	block := jsonBlock(catalog{Files: files, Folders: ownedFolders}, "Stamp output catalog")
	code := block["code"].(Object)
	if len(code["rich_text"].([]Object)) > 100 {
		return errors.New("Notion output catalog exceeds 100 text chunks")
	}
	_, err := c.UpdateBlock(ctx, s.CatalogID, Object{"code": code})
	return err
}

func (c *Client) Preflight(ctx context.Context, s Snapshot) error {
	_, err := c.verifyCatalog(ctx, s)
	return err
}
