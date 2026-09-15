package notion

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMultipartUploadPreservesBytesAndMediaType(t *testing.T) {
	data := bytes.Repeat([]byte("zip-test-payload"), (21<<20)/16+1)
	// Ensure the fixture crosses the documented single-part threshold.
	data = data[:21<<20]
	var received bytes.Buffer
	parts := 0
	completed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/file_uploads":
			var body Object
			json.NewDecoder(r.Body).Decode(&body)
			if body["mode"] != "multi_part" || body["number_of_parts"] != float64(3) {
				t.Errorf("invalid multipart declaration: %v", body)
			}
			json.NewEncoder(w).Encode(Object{"id": "upload"})
		case "/file_uploads/upload/send":
			reader, err := r.MultipartReader()
			if err != nil {
				t.Error(err)
				return
			}
			for {
				part, err := reader.NextPart()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Error(err)
					return
				}
				body, _ := io.ReadAll(part)
				switch part.FormName() {
				case "file":
					if part.Header.Get("Content-Type") != "application/zip" {
						t.Error("file media type changed")
					}
					received.Write(body)
				case "part_number":
					parts++
					if string(body) != strconv.Itoa(parts) {
						t.Error("parts reordered")
					}
				}
			}
			json.NewEncoder(w).Encode(Object{"id": "upload"})
		case "/file_uploads/upload/complete":
			completed = true
			json.NewEncoder(w).Encode(Object{"id": "upload"})
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
	}))
	defer server.Close()
	c := &Client{base: server.URL, http: server.Client(), MaxUpload: 30 << 20}
	_, err := c.Upload(context.Background(), "project.stamp.zip", "application/zip", data)
	if err != nil || !completed || parts != 3 || !bytes.Equal(data, received.Bytes()) {
		t.Fatalf("multipart data integrity failed: parts=%d complete=%v err=%v", parts, completed, err)
	}
}
func TestRateLimitWaitHonorsCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(429)
		io.WriteString(w, `{"code":"rate_limited"}`)
	}))
	defer server.Close()
	c := &Client{base: server.URL, http: server.Client()}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := c.Call(ctx, "GET", "users/me", nil)
	if err != context.DeadlineExceeded {
		t.Fatalf("rate limit was not cancellable: %v", err)
	}
}
func TestUpdateMediaUsesUpdateSchema(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if _, exists := body["pdf"]["type"]; exists {
			t.Error("creation discriminator sent to update endpoint")
		}
		if body["pdf"]["file_upload"].(map[string]any)["id"] != "upload" {
			t.Error("attachment identity lost")
		}
		io.WriteString(w, `{"id":"block"}`)
	}))
	defer server.Close()
	c := &Client{base: server.URL, http: server.Client()}
	block := FileBlock("pdf", "upload", "test.pdf", "stamp-output:test.pdf")
	if _, err := c.UpdateBlock(context.Background(), "block", Object{"pdf": block["pdf"]}); err != nil {
		t.Fatal(err)
	}
}
func TestNotionPageReferences(t *testing.T) {
	id := "12345678-1234-1234-1234-123456789abc"
	for _, value := range []string{id, strings.ReplaceAll(id, "-", ""), "https://app.notion.com/p/Project-" + strings.ReplaceAll(id, "-", "") + "?v=ignored"} {
		actual, err := ID(value)
		if err != nil || actual != id {
			t.Errorf("%q: %q %v", value, actual, err)
		}
	}
	for _, value := range []string{"https://evil.example/" + id, "not-a-page", "https://notion.so/invalid"} {
		if _, err := ID(value); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
}
