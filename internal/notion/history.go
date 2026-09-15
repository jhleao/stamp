package notion

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

const revisionDetailsTitle = "Details"
const revisionMetadataCaption = "Stamp revision metadata"

type revisionRecord struct {
	Revision
	Archive string `json:"archive"`
}

func (c *Client) historyBlocks(ctx context.Context, history string) ([]Object, error) {
	blocks, err := c.Children(ctx, history)
	if err != nil {
		return nil, err
	}
	result := append([]Object(nil), blocks...)
	for _, block := range blocks {
		if String(block, "type") != "toggle" {
			continue
		}
		toggle, _ := block["toggle"].(map[string]any)
		texts, _ := toggle["rich_text"].([]any)
		if len(texts) != 1 {
			continue
		}
		text, _ := texts[0].(map[string]any)
		if text["plain_text"] != revisionDetailsTitle {
			continue
		}
		children, err := c.Children(ctx, String(block, "id"))
		if err != nil {
			return nil, err
		}
		for _, child := range children {
			if String(child, "type") == "code" && caption(child) == revisionMetadataCaption {
				result = append(result, child)
			}
		}
	}
	return result, nil
}

func (c *Client) readRevisions(ctx context.Context, history string) ([]Revision, error) {
	blocks, err := c.historyBlocks(ctx, history)
	if err != nil {
		return nil, err
	}
	return parseRevisions(blocks)
}

// A migration writes the hidden record before clearing the old caption. Accept
// both copies only when they describe exactly the same archive and ancestry.
func parseRevisions(blocks []Object) ([]Revision, error) {
	files := map[string]bool{}
	for _, block := range blocks {
		if String(block, "type") == "file" {
			files[String(block, "id")] = true
		}
	}
	byArchive := map[string]Revision{}
	var result []Revision
	for _, block := range blocks {
		var r Revision
		switch {
		case String(block, "type") == "file" && strings.HasPrefix(caption(block), "stamp-revision:"):
			if err := json.Unmarshal([]byte(strings.TrimPrefix(caption(block), "stamp-revision:")), &r); err != nil {
				return nil, errors.New("invalid Stamp revision record")
			}
			r.BlockID = String(block, "id")
		case String(block, "type") == "code" && caption(block) == revisionMetadataCaption:
			var record revisionRecord
			if err := json.Unmarshal([]byte(codeText(block)), &record); err != nil {
				return nil, errors.New("invalid Stamp revision metadata")
			}
			r = record.Revision
			r.BlockID = record.Archive
			if !files[r.BlockID] {
				return nil, errors.New("Stamp revision archive is missing or was moved")
			}
		default:
			continue
		}
		if _, err := hex.DecodeString(r.ID); err != nil || len(r.ID) != 64 {
			return nil, errors.New("invalid revision hash")
		}
		if old, found := byArchive[r.BlockID]; found {
			if old.ID != r.ID || !slices.Equal(old.Parents, r.Parents) {
				return nil, errors.New("conflicting Stamp revision metadata")
			}
			continue
		}
		byArchive[r.BlockID] = r
		result = append(result, r)
	}
	return result, nil
}

func (c *Client) hideRevision(ctx context.Context, history string, revision Revision) error {
	blocks, err := c.historyBlocks(ctx, history)
	if err != nil {
		return err
	}
	if _, err := parseRevisions(blocks); err != nil {
		return err
	}
	exists := false
	for _, block := range blocks {
		if String(block, "type") != "code" || caption(block) != revisionMetadataCaption {
			continue
		}
		var record revisionRecord
		if err := json.Unmarshal([]byte(codeText(block)), &record); err != nil {
			return err
		}
		if record.Archive == revision.BlockID {
			exists = true
		}
	}
	if !exists {
		child := jsonBlock(revisionRecord{Revision: revision, Archive: revision.BlockID}, revisionMetadataCaption)
		toggle := Object{"object": "block", "type": "toggle", "toggle": Object{"rich_text": Text(revisionDetailsTitle), "color": "gray", "children": []Object{child}}}
		_, err := c.Call(ctx, "PATCH", "blocks/"+history+"/children", Object{"children": []Object{toggle}, "position": Object{"type": "after_block", "after_block": Object{"id": revision.BlockID}}})
		if err != nil {
			return err
		}
	}
	_, err = c.UpdateBlock(ctx, revision.BlockID, Object{"file": Object{"caption": []Object{}}})
	return err
}
