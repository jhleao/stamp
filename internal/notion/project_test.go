package notion

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRevisionHistoryPreservesConcurrentPushes(t *testing.T) {
	history := []Revision{{ID: "base"}, {ID: "alice", Parents: []string{"base"}}, {ID: "bob", Parents: []string{"base"}}}
	heads, lease, err := RevisionHeads(history)
	if err != nil || len(heads) != 2 || !strings.HasPrefix(lease, "conflict-") {
		t.Fatalf("lost concurrent revision: %v %s %v", heads, lease, err)
	}
	reordered := []Revision{history[2], history[0], history[1]}
	_, sameLease, err := RevisionHeads(reordered)
	if err != nil || sameLease != lease {
		t.Fatal("lease depends on API ordering")
	}
	history = append(history, Revision{ID: "resolved", Parents: []string{"alice", "bob"}})
	heads, lease, err = RevisionHeads(history)
	if err != nil || len(heads) != 1 || lease != "resolved" {
		t.Fatalf("resolution failed: %v %s %v", heads, lease, err)
	}
}
func TestRevisionHistoryRejectsMissingAndCyclicParents(t *testing.T) {
	cases := [][]Revision{
		{{ID: "a", Parents: []string{"missing"}}},
		{{ID: "a"}, {ID: "a"}},
		{{ID: "a", Parents: []string{"a"}}},
		{{ID: "head"}, {ID: "a", Parents: []string{"b"}}, {ID: "b", Parents: []string{"a"}}},
	}
	for _, history := range cases {
		if _, _, err := RevisionHeads(history); err == nil {
			t.Fatalf("accepted corrupt history: %v", history)
		}
	}
}
func TestAmbiguousMutationIsNotRetriedAndErrorsDoNotExposeContents(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(500)
		json.NewEncoder(w).Encode(Object{"code": "internal_server_error", "message": "secret-token signed-url sensitive-content"})
	}))
	defer server.Close()
	c := &Client{token: "secret-token", base: server.URL, http: server.Client()}
	_, err := c.Call(context.Background(), "POST", "pages", Object{"title": "private"})
	if err == nil || calls != 1 || strings.Contains(err.Error(), "secret-token") || strings.Contains(err.Error(), "sensitive-content") {
		t.Fatalf("unsafe mutation failure: calls=%d err=%v", calls, err)
	}
}
func TestPaginationFailureDoesNotReturnPartialInventory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("start_cursor") != "" {
			w.WriteHeader(403)
			json.NewEncoder(w).Encode(Object{"code": "restricted_resource"})
			return
		}
		json.NewEncoder(w).Encode(Object{"results": []Object{{"id": "visible"}}, "has_more": true, "next_cursor": "next"})
	}))
	defer server.Close()
	c := &Client{base: server.URL, http: server.Client()}
	blocks, err := c.Children(context.Background(), "page")
	if err == nil || blocks != nil {
		t.Fatalf("partial inventory could cause destructive reconciliation: %v %v", blocks, err)
	}
}
