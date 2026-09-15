// Package notion owns Notion's page, block and binary-file protocol. Tokens and
// temporary download URLs never form part of a project's portable metadata.
package notion

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jhleao/stamp/internal/diagnostic"
)

const Version = "2026-03-11"

type Object map[string]any

type Client struct {
	token     string
	base      string
	http      *http.Client
	MaxUpload int64
}

func New(ctx context.Context) (*Client, error) {
	token, err := tokenForSession(ctx)
	if err != nil {
		return nil, err
	}
	return newWithToken(ctx, token)
}

func newWithToken(ctx context.Context, token string) (*Client, error) {
	c := &Client{token: token, base: "https://api.notion.com/v1", http: &http.Client{Timeout: 2 * time.Minute}, MaxUpload: 5 << 20}
	me, err := c.Call(ctx, "GET", "users/me", nil)
	if err != nil {
		return nil, err
	}
	if bot, ok := me["bot"].(map[string]any); ok {
		if limits, ok := bot["workspace_limits"].(map[string]any); ok {
			if n, ok := limits["max_file_upload_size_in_bytes"].(float64); ok && n > 0 {
				c.MaxUpload = int64(n)
			}
		}
	}
	return c, nil
}

// Call retries only explicit throttling responses. A failed mutation may have
// succeeded remotely; retrying an ambiguous POST could duplicate pages.
func (c *Client) Call(ctx context.Context, method, endpoint string, body any) (Object, error) {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	return c.request(ctx, method, endpoint, "application/json", data)
}
func (c *Client) request(ctx context.Context, method, endpoint, contentType string, data []byte) (Object, error) {
	for attempt := 0; attempt < 5; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.base+"/"+endpoint, bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Notion-Version", Version)
		req.Header.Set("Content-Type", contentType)
		started := time.Now()
		res, err := c.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("Notion %s request failed; inspect remote state before retrying: %w", method, err)
		}
		diagnostic.Log("notion", "http", "method", method, "path", strings.Split(endpoint, "?")[0], "status", res.StatusCode, "duration_ms", time.Since(started).Milliseconds())
		contents, readErr := io.ReadAll(io.LimitReader(res.Body, 16<<20))
		res.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if res.StatusCode == 429 || res.StatusCode == 529 {
			seconds, _ := strconv.Atoi(res.Header.Get("Retry-After"))
			if seconds < 1 {
				seconds = 1 << attempt
			}
			timer := time.NewTimer(time.Duration(seconds) * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
			continue
		}
		var result Object
		if err := json.Unmarshal(contents, &result); err != nil {
			return nil, fmt.Errorf("Notion returned HTTP %d with an invalid response", res.StatusCode)
		}
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			hint := ""
			switch res.StatusCode {
			case 401:
				hint = "; token expired, revoked, or blocked by workspace policy"
			case 403:
				hint = "; check edit permission and token capabilities"
			case 404:
				hint = "; resource missing or inaccessible (Stamp will not recreate it automatically)"
			}
			// Error messages can echo request bodies; expose only the protocol code.
			return nil, fmt.Errorf("Notion %s %s: HTTP %d (%s)%s", method, strings.Split(endpoint, "?")[0], res.StatusCode, String(result, "code"), hint)
		}
		return result, nil
	}
	return nil, errors.New("Notion remained rate limited after five attempts")
}

func String(o Object, key string) string { s, _ := o[key].(string); return s }
func Text(value string) []Object         { return []Object{{"type": "text", "text": Object{"content": value}}} }
func Title(value string) Object {
	return Object{"title": Object{"type": "title", "title": Text(value)}}
}
func (c *Client) Page(ctx context.Context, id string) (Object, error) {
	return c.Call(ctx, "GET", "pages/"+id, nil)
}
func (c *Client) Block(ctx context.Context, id string) (Object, error) {
	return c.Call(ctx, "GET", "blocks/"+id, nil)
}
func (c *Client) Children(ctx context.Context, id string) ([]Object, error) {
	var all []Object
	endpoint := "blocks/" + id + "/children?page_size=100"
	for {
		result, err := c.Call(ctx, "GET", endpoint, nil)
		if err != nil {
			return nil, err
		}
		entries, _ := result["results"].([]any)
		for _, entry := range entries {
			if object, ok := entry.(map[string]any); ok {
				all = append(all, Object(object))
			}
		}
		if more, _ := result["has_more"].(bool); !more {
			return all, nil
		}
		cursor := String(result, "next_cursor")
		if cursor == "" {
			return nil, errors.New("Notion pagination returned no cursor")
		}
		endpoint = "blocks/" + id + "/children?page_size=100&start_cursor=" + url.QueryEscape(cursor)
	}
}
func (c *Client) CreatePage(ctx context.Context, parent, name string, children []Object, icons ...string) (Object, error) {
	body := Object{"properties": Title(name)}
	icon := "📁"
	for _, child := range children {
		if String(child, "type") == "pdf" {
			icon = "📄"
		}
		if String(child, "type") == "file" {
			icon = "📎"
		}
	}
	if len(icons) > 0 {
		icon = icons[0]
	}
	body["icon"] = Object{"type": "emoji", "emoji": icon}
	if parent != "" && parent != "root" {
		body["parent"] = Object{"page_id": parent}
	} else {
		body["parent"] = Object{"workspace": true}
	}
	if len(children) > 0 {
		body["children"] = children
	}
	return c.Call(ctx, "POST", "pages", body)
}
func (c *Client) Append(ctx context.Context, parent string, children ...Object) (Object, error) {
	return c.Call(ctx, "PATCH", "blocks/"+parent+"/children", Object{"children": children})
}
func (c *Client) UpdateBlock(ctx context.Context, id string, body Object) (Object, error) {
	// Media update schemas infer the source from file_upload; unlike creation,
	// they reject the nested type discriminator.
	for _, kind := range []string{"file", "pdf", "image", "video", "audio"} {
		if media, ok := body[kind].(Object); ok {
			delete(media, "type")
		}
	}
	return c.Call(ctx, "PATCH", "blocks/"+id, body)
}
func (c *Client) TrashPage(ctx context.Context, id string) error {
	_, err := c.Call(ctx, "PATCH", "pages/"+id, Object{"in_trash": true})
	return err
}

func FileBlock(kind, upload, name, caption string) Object {
	media := Object{"type": "file_upload", "file_upload": Object{"id": upload}, "caption": Text(caption)}
	if kind == "file" {
		media["name"] = name
	}
	return Object{"object": "block", "type": kind, kind: media}
}

func (c *Client) Upload(ctx context.Context, name, contentType string, data []byte) (string, error) {
	if int64(len(data)) > c.MaxUpload {
		return "", fmt.Errorf("%s exceeds this Notion workspace's upload limit (%d bytes)", name, c.MaxUpload)
	}
	const chunk = 10 << 20
	body := Object{"filename": name, "content_type": contentType, "mode": "single_part"}
	if len(data) > 20<<20 {
		body["mode"] = "multi_part"
		body["number_of_parts"] = (len(data) + chunk - 1) / chunk
	}
	upload, err := c.Call(ctx, "POST", "file_uploads", body)
	if err != nil {
		return "", err
	}
	id := String(upload, "id")
	if id == "" {
		return "", errors.New("Notion upload returned no ID")
	}
	partSize := len(data)
	if partSize == 0 {
		partSize = 1
	}
	if body["mode"] == "multi_part" {
		partSize = chunk
	}
	for offset, part := 0, 1; offset < len(data) || part == 1; offset, part = offset+partSize, part+1 {
		end := offset + partSize
		if end > len(data) {
			end = len(data)
		}
		var buffer bytes.Buffer
		writer := multipart.NewWriter(&buffer)
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": "file", "filename": name}))
		header.Set("Content-Type", contentType)
		file, err := writer.CreatePart(header)
		if err != nil {
			return "", err
		}
		if _, err = file.Write(data[offset:end]); err != nil {
			return "", err
		}
		if body["mode"] == "multi_part" {
			if err = writer.WriteField("part_number", strconv.Itoa(part)); err != nil {
				return "", err
			}
		}
		if err = writer.Close(); err != nil {
			return "", err
		}
		if _, err = c.request(ctx, "POST", "file_uploads/"+id+"/send", writer.FormDataContentType(), buffer.Bytes()); err != nil {
			return "", err
		}
	}
	if body["mode"] == "multi_part" {
		if _, err = c.Call(ctx, "POST", "file_uploads/"+id+"/complete", Object{}); err != nil {
			return "", err
		}
	}
	return id, nil
}

// Download always refreshes the signed URL and sends no API credentials to it.
func (c *Client) Download(ctx context.Context, blockID string) ([]byte, error) {
	block, err := c.Block(ctx, blockID)
	if err != nil {
		return nil, err
	}
	media, _ := block[String(block, "type")].(map[string]any)
	file, _ := media["file"].(map[string]any)
	raw, _ := file["url"].(string)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, errors.New("Notion block has no hosted download URL")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if err != nil {
		return nil, err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, errors.New("Notion file download failed")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("Notion file download returned HTTP %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, c.MaxUpload+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > c.MaxUpload {
		return nil, errors.New("Notion download exceeds workspace file limit")
	}
	return data, nil
}

var pageID = regexp.MustCompile(`(?i)[0-9a-f]{32}$`)

func ID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "://") {
		u, err := url.Parse(value)
		if err != nil {
			return "", err
		}
		if u.Scheme != "https" || !(u.Hostname() == "notion.so" || strings.HasSuffix(u.Hostname(), ".notion.so") || u.Hostname() == "notion.com" || strings.HasSuffix(u.Hostname(), ".notion.com")) {
			return "", errors.New("expected a Notion page URL")
		}
		value = strings.TrimRight(u.Path, "/")
	}
	compact := strings.ReplaceAll(value, "-", "")
	id := pageID.FindString(compact)
	if id == "" {
		return "", errors.New("expected a Notion page ID or URL")
	}
	return strings.ToLower(id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]), nil
}
