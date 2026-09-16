package notion

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTreeMigrationRefusesToHideAdditionalContent(t *testing.T) {
	for _, extra := range []Object{
		{"id": "note", "type": "paragraph", "paragraph": Object{"rich_text": Text("A teammate's notes")}},
		{"id": "other-page", "type": "child_page", "child_page": Object{"title": "Research"}},
	} {
		t.Run(String(extra, "type"), func(t *testing.T) {
			mutations := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					mutations++
					w.WriteHeader(500)
					return
				}
				json.NewEncoder(w).Encode(Object{"results": []Object{{"id": "catalog"}, extra}, "has_more": false})
			}))
			defer server.Close()
			client := &Client{base: server.URL, http: server.Client()}
			err := client.checkLegacyTree(context.Background(), Snapshot{CurrentID: "current", CatalogID: "catalog"}, catalog{Files: map[string]publishedFile{}, Folders: map[string]string{}})
			if err == nil || mutations != 0 {
				t.Fatalf("user content must block migration without writes: %v, mutations=%d", err, mutations)
			}
		})
	}
}
