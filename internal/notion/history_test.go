package notion

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRevisionMetadataMigrationPreservesIdentityAndRejectsDamage(t *testing.T) {
	revision := Revision{ID: strings.Repeat("a", 64), Parents: []string{}, BlockID: "archive"}
	file := func(captionText string) Object {
		return Object{"id": "archive", "type": "file", "file": map[string]any{"caption": []any{map[string]any{"plain_text": captionText}}}}
	}
	record := func(r Revision, archive string) Object {
		raw, _ := json.Marshal(revisionRecord{Revision: r, Archive: archive})
		return Object{"type": "code", "code": map[string]any{"caption": []any{map[string]any{"plain_text": revisionMetadataCaption}}, "rich_text": []any{map[string]any{"plain_text": string(raw)}}}}
	}
	raw, _ := json.Marshal(revision)
	legacy := file("stamp-revision:" + string(raw))
	hidden := record(revision, "archive")
	for _, blocks := range [][]Object{{legacy}, {legacy, hidden}, {file(""), hidden}, {file(""), hidden, hidden}} {
		got, err := parseRevisions(blocks)
		if err != nil || len(got) != 1 || got[0].ID != revision.ID || got[0].BlockID != "archive" {
			t.Fatalf("migration lost identity: %+v, %v", got, err)
		}
	}
	changed := revision
	changed.Parents = []string{strings.Repeat("b", 64)}
	for _, blocks := range [][]Object{{hidden}, {file(""), record(revision, "moved")}, {legacy, record(changed, "archive")}} {
		if _, err := parseRevisions(blocks); err == nil {
			t.Fatal("accepted missing archive or inconsistent migration record")
		}
	}
}
