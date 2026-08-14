package notion

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

const (
	apiBase         = "https://api.notion.com/v1"
	apiVersion      = "2026-03-11"
	keychainService = "sh.stamp.notion"
	keychainAccount = "default"
)

type Client struct {
	token string
	http  *http.Client
}

type Page struct {
	ID             string         `json:"id"`
	URL            string         `json:"url"`
	LastEditedTime string         `json:"last_edited_time"`
	Properties     map[string]any `json:"properties"`
}

type Block struct {
	ID             string         `json:"id"`
	Type           string         `json:"type"`
	LastEditedTime string         `json:"last_edited_time"`
	Data           map[string]any `json:"-"`
	raw            map[string]any
}

type Upload struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	UploadURL string `json:"upload_url"`
}

func SaveToken(token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("Notion token is empty")
	}
	cmd := exec.Command("security", "add-generic-password", "-U", "-a", keychainAccount, "-s", keychainService, "-w", token)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("store Notion token in Keychain: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func Logout() error {
	cmd := exec.Command("security", "delete-generic-password", "-a", keychainAccount, "-s", keychainService)
	if out, err := cmd.CombinedOutput(); err != nil && !strings.Contains(string(out), "could not be found") {
		return fmt.Errorf("remove Notion token: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func Token() (string, error) {
	out, err := exec.Command("security", "find-generic-password", "-w", "-a", keychainAccount, "-s", keychainService).Output()
	if err != nil {
		return "", errors.New("Notion is not connected; run stamp notion login")
	}
	return strings.TrimSpace(string(out)), nil
}

func New() (*Client, error) {
	token, err := Token()
	if err != nil {
		return nil, err
	}
	return &Client{token: token, http: &http.Client{Timeout: 60 * time.Second}}, nil
}

func (c *Client) Me(ctx context.Context) (map[string]any, error) {
	var result map[string]any
	err := c.do(ctx, http.MethodGet, "/users/me", nil, &result)
	return result, err
}

func (c *Client) SearchPages(ctx context.Context) ([]Page, error) {
	var response struct {
		Results []Page `json:"results"`
	}
	body := map[string]any{"filter": map[string]string{"property": "object", "value": "page"}, "page_size": 100}
	if err := c.do(ctx, http.MethodPost, "/search", body, &response); err != nil {
		return nil, err
	}
	return response.Results, nil
}

func (c *Client) GetPage(ctx context.Context, id string) (Page, error) {
	var page Page
	err := c.do(ctx, http.MethodGet, "/pages/"+ID(id), nil, &page)
	return page, err
}

func (c *Client) CreatePage(ctx context.Context, parentID, title string) (Page, error) {
	return c.CreatePageMarkdown(ctx, parentID, title, "")
}

func (c *Client) CreatePageMarkdown(ctx context.Context, parentID, title, markdown string) (Page, error) {
	return c.CreatePageMarkdownIcon(ctx, parentID, title, markdown, "")
}

func (c *Client) CreatePageMarkdownIcon(ctx context.Context, parentID, title, markdown, icon string) (Page, error) {
	body := map[string]any{"properties": map[string]any{"title": map[string]any{"type": "title", "title": richText(title)}}}
	if strings.TrimSpace(parentID) != "" {
		body["parent"] = map[string]string{"type": "page_id", "page_id": ID(parentID)}
	} else {
		body["parent"] = map[string]any{"type": "workspace", "workspace": true}
	}
	if markdown != "" {
		body["markdown"] = markdown
	}
	if icon != "" {
		body["icon"] = map[string]any{"type": "emoji", "emoji": icon}
	}
	var page Page
	err := c.do(ctx, http.MethodPost, "/pages", body, &page)
	return page, err
}

type Markdown struct {
	Markdown  string   `json:"markdown"`
	Truncated bool     `json:"truncated"`
	Unknown   []string `json:"unknown_block_ids"`
}

func (c *Client) Markdown(ctx context.Context, pageID string) (Markdown, error) {
	var result Markdown
	err := c.do(ctx, http.MethodGet, "/pages/"+ID(pageID)+"/markdown", nil, &result)
	if err == nil && (result.Truncated || len(result.Unknown) > 0) {
		return result, errors.New("Notion page contains truncated or unsupported content; refusing a lossy pull")
	}
	return result, err
}

func (c *Client) ReplaceMarkdown(ctx context.Context, pageID, markdown string) (Markdown, error) {
	var result Markdown
	body := map[string]any{"type": "replace_content", "replace_content": map[string]any{"new_str": markdown}}
	err := c.do(ctx, http.MethodPatch, "/pages/"+ID(pageID)+"/markdown", body, &result)
	return result, err
}

func (c *Client) Children(ctx context.Context, blockID string) ([]Block, error) {
	var response struct {
		Results []map[string]any `json:"results"`
		HasMore bool             `json:"has_more"`
		Cursor  string           `json:"next_cursor"`
	}
	path := "/blocks/" + ID(blockID) + "/children?page_size=100"
	var blocks []Block
	for {
		response.Results = nil
		if err := c.do(ctx, http.MethodGet, path, nil, &response); err != nil {
			return nil, err
		}
		for _, raw := range response.Results {
			block := Block{raw: raw}
			block.ID, _ = raw["id"].(string)
			block.Type, _ = raw["type"].(string)
			block.LastEditedTime, _ = raw["last_edited_time"].(string)
			block.Data, _ = raw[block.Type].(map[string]any)
			blocks = append(blocks, block)
		}
		if !response.HasMore {
			return blocks, nil
		}
		path = "/blocks/" + ID(blockID) + "/children?page_size=100&start_cursor=" + url.QueryEscape(response.Cursor)
	}
}

func (c *Client) Append(ctx context.Context, blockID string, children []map[string]any) error {
	for len(children) > 0 {
		n := 100
		if len(children) < n {
			n = len(children)
		}
		if err := c.do(ctx, http.MethodPatch, "/blocks/"+ID(blockID)+"/children", map[string]any{"children": children[:n]}, nil); err != nil {
			return err
		}
		children = children[n:]
	}
	return nil
}

func (c *Client) DeleteBlock(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/blocks/"+ID(id), nil, nil)
}

func (c *Client) UpdateCode(ctx context.Context, id, text string) error {
	return c.do(ctx, http.MethodPatch, "/blocks/"+ID(id), map[string]any{
		"code": map[string]any{"rich_text": richText(text), "language": "markdown"},
	}, nil)
}

func (c *Client) Upload(ctx context.Context, name, contentType string, data []byte) (Upload, error) {
	var upload Upload
	if err := c.do(ctx, http.MethodPost, "/file_uploads", map[string]any{"mode": "single_part", "filename": name, "content_type": contentType}, &upload); err != nil {
		return upload, err
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, strings.ReplaceAll(name, `"`, "")))
	header.Set("Content-Type", contentType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return upload, err
	}
	if _, err := part.Write(data); err != nil {
		return upload, err
	}
	if err := writer.Close(); err != nil {
		return upload, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+"/file_uploads/"+upload.ID+"/send", &body)
	if err != nil {
		return upload, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Notion-Version", apiVersion)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if err := c.perform(req, &upload); err != nil {
		return upload, err
	}
	return upload, nil
}

func (c *Client) Download(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	response, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		return nil, fmt.Errorf("download Notion file: %s", response.Status)
	}
	return io.ReadAll(response.Body)
}

func (c *Client) do(ctx context.Context, method, path string, body any, result any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, apiBase+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Notion-Version", apiVersion)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.perform(req, result)
}

func (c *Client) perform(req *http.Request, result any) error {
	for attempt := 0; attempt < 4; attempt++ {
		response, err := c.http.Do(req)
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		if readErr != nil {
			return readErr
		}
		if response.StatusCode == http.StatusTooManyRequests && attempt < 3 {
			delay := time.Second
			if value := response.Header.Get("Retry-After"); value != "" {
				if parsed, err := time.ParseDuration(value + "s"); err == nil {
					delay = parsed
				}
			}
			time.Sleep(delay)
			continue
		}
		if response.StatusCode/100 != 2 {
			var problem struct {
				Message string `json:"message"`
			}
			_ = json.Unmarshal(data, &problem)
			if problem.Message == "" {
				problem.Message = response.Status
			}
			return fmt.Errorf("Notion: %s", problem.Message)
		}
		if result != nil && len(data) > 0 {
			return json.Unmarshal(data, result)
		}
		return nil
	}
	return errors.New("Notion request retry limit reached")
}

func richText(value string) []map[string]any {
	return []map[string]any{{"type": "text", "text": map[string]any{"content": value}}}
}

func ID(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimSuffix(strings.Split(value, "?")[0], "/")
	if index := strings.LastIndex(value, "-"); index >= 0 && len(value)-index-1 >= 32 {
		value = value[index+1:]
	} else if index := strings.LastIndex(value, "/"); index >= 0 {
		value = value[index+1:]
	}
	value = strings.ReplaceAll(value, "-", "")
	if len(value) == 32 {
		return value[:8] + "-" + value[8:12] + "-" + value[12:16] + "-" + value[16:20] + "-" + value[20:]
	}
	return value
}

func Text(block Block) string {
	items, _ := block.Data["rich_text"].([]any)
	var text strings.Builder
	for _, item := range items {
		part, _ := item.(map[string]any)
		plain, _ := part["plain_text"].(string)
		text.WriteString(plain)
	}
	return text.String()
}

func FileURL(block Block) string {
	file, _ := block.Data["file"].(map[string]any)
	value, _ := file["url"].(string)
	return value
}

func FileCaption(block Block) string {
	items, _ := block.Data["caption"].([]any)
	var text strings.Builder
	for _, item := range items {
		part, _ := item.(map[string]any)
		plain, _ := part["plain_text"].(string)
		text.WriteString(plain)
	}
	return text.String()
}

func ChildTitle(block Block) string {
	if block.Type != "child_page" {
		return ""
	}
	title, _ := block.Data["title"].(string)
	return title
}

func Paragraph(text string) map[string]any {
	return map[string]any{"object": "block", "type": "paragraph", "paragraph": map[string]any{"rich_text": richText(text)}}
}

func Heading(level int, text string) map[string]any {
	kind := fmt.Sprintf("heading_%d", level)
	return map[string]any{"object": "block", "type": kind, kind: map[string]any{"rich_text": richText(text)}}
}

func Callout(text, icon string) map[string]any {
	return map[string]any{"object": "block", "type": "callout", "callout": map[string]any{
		"rich_text": richText(text), "icon": map[string]any{"type": "emoji", "emoji": icon},
	}}
}

func Divider() map[string]any {
	return map[string]any{"object": "block", "type": "divider", "divider": map[string]any{}}
}

func Code(text, language string) map[string]any {
	return map[string]any{"object": "block", "type": "code", "code": map[string]any{"rich_text": richText(text), "language": language}}
}

func FileBlock(uploadID, caption string) map[string]any {
	return map[string]any{"object": "block", "type": "file", "file": map[string]any{"type": "file_upload", "file_upload": map[string]string{"id": uploadID}, "caption": richText(caption)}}
}
