package drive

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"

	"github.com/jhleao/stamp/internal/diagnostic"
)

const defaultKeychainService = "sh.stamp.google-drive"

var (
	DefaultOAuthClientID     string
	DefaultOAuthClientSecret string
)

type CredentialSource string

const (
	CredentialDefault     CredentialSource = "Stamp default"
	CredentialInstalled   CredentialSource = "organization override"
	CredentialEnvironment CredentialSource = "environment override"
)

type CredentialInfo struct {
	Source   CredentialSource
	Path     string
	ClientID string
}

type Client struct {
	api      *drive.Service
	identity string
}

type Item struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Folder   bool              `json:"folder"`
	CanEdit  bool              `json:"canEdit"`
	Version  string            `json:"version,omitempty"`
	WebURL   string            `json:"webUrl,omitempty"`
	Parents  []string          `json:"parents,omitempty"`
	Props    map[string]string `json:"properties,omitempty"`
	revision string
}

// FileRef describes a canonical published file. ID is empty only while an
// older project is being upgraded to the output identity ledger.
type FileRef struct {
	Key  string `json:"key"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
}

// ResolveFiles verifies access to canonical published files and opens one
// constrained Picker when drive.file has not authorized them for this user.
func (c *Client) ResolveFiles(ctx context.Context, folderID string, refs []FileRef) (map[string]Item, error) {
	resolved := make(map[string]Item, len(refs))
	var missing []FileRef
	for _, ref := range refs {
		if ref.ID == "" {
			missing = append(missing, ref)
			continue
		}
		item, err := c.Get(ctx, ref.ID)
		if err != nil || !item.CanEdit {
			missing = append(missing, ref)
			continue
		}
		resolved[ref.Key] = item
	}
	if len(missing) == 0 {
		return resolved, nil
	}
	files, err := pickFiles(ctx, pickerRequest{
		title:    fmt.Sprintf("Select all %d published files, then click Select", len(missing)),
		prompt:   fmt.Sprintf("Select all %d published files. Stamp verifies every file before Push.", len(missing)),
		mime:     "application/pdf,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		parent:   folderID,
		required: missing,
	})
	if err != nil {
		return nil, err
	}
	for _, ref := range missing {
		var selected *pickedFile
		for i := range files {
			if (ref.ID != "" && files[i].ID == ref.ID) || (ref.ID == "" && files[i].Name == ref.Name) {
				selected = &files[i]
				break
			}
		}
		if selected == nil {
			return nil, fmt.Errorf("published file %q was not selected", ref.Name)
		}
		item, err := c.Get(ctx, selected.ID)
		if err != nil {
			return nil, fmt.Errorf("authorize %q: %w", ref.Name, err)
		}
		if !item.CanEdit {
			return nil, fmt.Errorf("published file %q is read-only; ask its owner for Editor access", ref.Name)
		}
		resolved[ref.Key] = item
	}
	return resolved, nil
}

type OAuthFile struct {
	Installed struct {
		ClientID     string   `json:"client_id"`
		ClientSecret string   `json:"client_secret"`
		AuthURI      string   `json:"auth_uri"`
		TokenURI     string   `json:"token_uri"`
		RedirectURIs []string `json:"redirect_uris"`
	} `json:"installed"`
}

func ConfigPath() string {
	if path := os.Getenv("STAMP_GOOGLE_OAUTH_CONFIG"); path != "" {
		return path
	}
	return OverridePath()
}

func OverridePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "Stamp", "google-oauth.json")
}

func InstallConfig(source string) (string, error) {
	data, err := os.ReadFile(source)
	if err != nil {
		return "", err
	}
	var file OAuthFile
	if err := json.Unmarshal(data, &file); err != nil {
		return "", fmt.Errorf("read OAuth JSON: %w", err)
	}
	if file.Installed.ClientID == "" {
		return "", errors.New("not a Google OAuth Desktop app JSON file")
	}
	destination := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(destination, data, 0o600); err != nil {
		return "", err
	}
	if err := os.Chmod(destination, 0o600); err != nil {
		return "", err
	}
	return destination, nil
}

func ResetConfig() (string, error) {
	path := OverridePath()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	_ = os.Remove(filepath.Dir(path))
	return path, nil
}

func Credentials() (CredentialInfo, error) {
	file, source, path, err := credentialFile()
	if err != nil {
		return CredentialInfo{}, err
	}
	return CredentialInfo{Source: source, Path: path, ClientID: file.Installed.ClientID}, nil
}

func Login(ctx context.Context) (string, error) {
	done := diagnostic.Start("drive", "login")
	var resultErr error
	defer func() { done(resultErr) }()
	config, clientID, err := oauthConfig()
	if err != nil {
		resultErr = err
		return "", err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		resultErr = err
		return "", err
	}
	defer listener.Close()
	config.RedirectURL = "http://" + listener.Addr().String() + "/oauth/callback"
	diagnostic.Log("drive", "login.callback_ready", "address", listener.Addr().String())
	state := randomToken()
	verifier := oauth2.GenerateVerifier()
	type loginResult struct {
		code string
		err  error
	}
	result := make(chan loginResult, 1)
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/callback" || r.URL.Query().Get("state") != state {
			http.Error(w, "Invalid Stamp login response.", http.StatusBadRequest)
			return
		}
		if oauthErr := r.URL.Query().Get("error"); oauthErr != "" {
			http.Error(w, oauthErr, http.StatusBadRequest)
			select {
			case result <- loginResult{err: fmt.Errorf("Google login was denied: %s", oauthErr)}:
			default:
			}
			return
		}
		select {
		case result <- loginResult{code: r.URL.Query().Get("code")}:
		default:
		}
		_, _ = io.WriteString(w, "Stamp is connected. You can close this tab.")
	})
	go server.Serve(listener)
	authURL := config.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce, oauth2.S256ChallengeOption(verifier))
	fmt.Fprintln(os.Stderr, "Continue in your browser:", authURL)
	if err := exec.Command("open", authURL).Start(); err != nil {
		resultErr = err
		return "", fmt.Errorf("open browser: %w", err)
	}
	var authCode string
	select {
	case login := <-result:
		if login.err != nil {
			resultErr = login.err
			return "", login.err
		}
		authCode = login.code
	case <-time.After(3 * time.Minute):
		resultErr = errors.New("Google login timed out")
		return "", errors.New("Google login did not complete; if Google says Stamp is being tested, add this account under Google Auth Platform > Audience > Test users, or make the app Internal")
	case <-ctx.Done():
		resultErr = ctx.Err()
		return "", ctx.Err()
	}
	_ = server.Shutdown(context.Background())
	token, err := config.Exchange(diagnostic.HTTPContext(ctx, "google-oauth"), authCode, oauth2.VerifierOption(verifier))
	if err != nil {
		resultErr = err
		return "", fmt.Errorf("exchange Google login: %w", err)
	}
	if err := saveToken(clientID, token); err != nil {
		resultErr = err
		return "", err
	}
	return "Google Drive connected", nil
}

func Logout() error {
	diagnostic.Log("drive", "logout.start")
	config, clientID, err := oauthConfig()
	if err != nil {
		return err
	}
	_ = config
	cmd := exec.Command("security", "delete-generic-password", "-s", keychainService(), "-a", clientID)
	if output, err := cmd.CombinedOutput(); err != nil && !strings.Contains(string(output), "could not be found") {
		return fmt.Errorf("keychain: %v: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func New(ctx context.Context) (*Client, error) {
	done := diagnostic.Start("drive", "client")
	config, clientID, err := oauthConfig()
	if err != nil {
		done(err)
		return nil, err
	}
	token, err := loadToken(clientID)
	if err != nil {
		done(err)
		return nil, err
	}
	httpClient := config.Client(diagnostic.HTTPContext(ctx, "google-drive"), token)
	api, err := drive.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		done(err)
		return nil, err
	}
	done(nil)
	return &Client{api: api}, nil
}

func (c *Client) Get(ctx context.Context, id string) (Item, error) {
	diagnostic.Log("drive", "files.get", "file_id", id)
	file, err := c.api.Files.Get(id).SupportsAllDrives(true).Fields(fileFields).Context(ctx).Do()
	if err != nil {
		return Item{}, err
	}
	return toItem(file), nil
}

func (c *Client) Rename(ctx context.Context, id, name string) (Item, error) {
	diagnostic.Log("drive", "files.rename", "file_id", id, "name", name)
	if strings.TrimSpace(name) == "" {
		return Item{}, errors.New("name is required")
	}
	file, err := c.api.Files.Update(id, &drive.File{Name: name}).SupportsAllDrives(true).Fields(fileFields).Context(ctx).Do()
	if err != nil {
		return Item{}, err
	}
	return toItem(file), nil
}

func (c *Client) Children(ctx context.Context, parentID string) ([]Item, error) {
	diagnostic.Log("drive", "files.children", "parent_id", parentID)
	return c.search(ctx, fmt.Sprintf("'%s' in parents and trashed=false", escape(parentID)))
}

func (c *Client) EnsureFolder(ctx context.Context, parentID, name string, props map[string]string) (Item, error) {
	diagnostic.Log("drive", "folders.ensure", "parent_id", parentID, "name", name)
	items, err := c.search(ctx, fmt.Sprintf("'%s' in parents and name='%s' and mimeType='%s' and trashed=false", escape(parentID), escape(name), driveFolder))
	if err != nil {
		return Item{}, err
	}
	if len(items) > 0 {
		diagnostic.Log("drive", "folders.found", "folder_id", items[0].ID, "name", items[0].Name)
		return items[0], nil
	}
	file, err := c.api.Files.Create(&drive.File{Name: name, MimeType: driveFolder, Parents: []string{parentID}, AppProperties: props}).
		SupportsAllDrives(true).Fields(fileFields).Context(ctx).Do()
	if err != nil {
		return Item{}, err
	}
	return toItem(file), nil
}

func (c *Client) CreateFile(ctx context.Context, parentID, name, mime string, contents io.Reader, props map[string]string) (Item, error) {
	diagnostic.Log("drive", "files.create", "parent_id", parentID, "name", name, "mime", mime)
	file, err := c.api.Files.Create(&drive.File{Name: name, MimeType: mime, Parents: []string{parentID}, AppProperties: props}).
		Media(contents).SupportsAllDrives(true).Fields(fileFields).Context(ctx).Do()
	if err != nil {
		return Item{}, err
	}
	return toItem(file), nil
}

func (c *Client) UpdateFile(ctx context.Context, id, mime string, contents io.Reader) (Item, error) {
	diagnostic.Log("drive", "files.update", "file_id", id, "mime", mime)
	file, err := c.api.Files.Update(id, &drive.File{MimeType: mime}).Media(contents).
		SupportsAllDrives(true).Fields(fileFields).Context(ctx).Do()
	if err != nil {
		return Item{}, err
	}
	return toItem(file), nil
}

func (c *Client) UpdateNamedFile(ctx context.Context, id, name, mime string, contents io.Reader) (Item, error) {
	diagnostic.Log("drive", "files.update_named", "file_id", id, "name", name, "mime", mime)
	file, err := c.api.Files.Update(id, &drive.File{Name: name, MimeType: mime}).Media(contents).
		SupportsAllDrives(true).Fields(fileFields).Context(ctx).Do()
	if err != nil {
		return Item{}, err
	}
	return toItem(file), nil
}

func (c *Client) Trash(ctx context.Context, id string) error {
	diagnostic.Log("drive", "files.trash", "file_id", id)
	_, err := c.api.Files.Update(id, &drive.File{Trashed: true}).SupportsAllDrives(true).Context(ctx).Do()
	return err
}

func (c *Client) Retain(ctx context.Context, item Item) error {
	diagnostic.Log("drive", "revisions.retain", "file_id", item.ID, "revision", item.revision)
	if item.revision == "" {
		return errors.New("Drive did not return a revision ID")
	}
	_, err := c.api.Revisions.Update(item.ID, item.revision, &drive.Revision{KeepForever: true}).Context(ctx).Do()
	return err
}

func (c *Client) Download(ctx context.Context, id string) ([]byte, error) {
	diagnostic.Log("drive", "files.download", "file_id", id)
	response, err := c.api.Files.Get(id).SupportsAllDrives(true).Context(ctx).Download()
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(io.LimitReader(response.Body, 513<<20))
	diagnostic.Log("drive", "files.downloaded", "file_id", id, "bytes", len(contents), "error", err)
	return contents, err
}

func (c *Client) FindChildByProperty(ctx context.Context, parentID, key, value string) (Item, bool, error) {
	diagnostic.Log("drive", "files.find_property", "parent_id", parentID, "property", key, "value", value)
	items, err := c.search(ctx, fmt.Sprintf("'%s' in parents and appProperties has { key='%s' and value='%s' } and trashed=false", escape(parentID), escape(key), escape(value)))
	if err != nil {
		return Item{}, false, err
	}
	if len(items) == 0 {
		diagnostic.Log("drive", "files.property_not_found", "parent_id", parentID, "property", key)
		return Item{}, false, nil
	}
	diagnostic.Log("drive", "files.property_found", "file_id", items[0].ID, "name", items[0].Name)
	return items[0], true, nil
}

func (c *Client) search(ctx context.Context, query string) ([]Item, error) {
	diagnostic.Log("drive", "files.search", "query", query)
	var result []Item
	pageToken := ""
	for {
		call := c.api.Files.List().Q(query).Spaces("drive").Corpora("allDrives").IncludeItemsFromAllDrives(true).SupportsAllDrives(true).
			Fields("nextPageToken,files(" + fileFields + ")").PageSize(1000).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		page, err := call.Do()
		if err != nil {
			return nil, err
		}
		for _, file := range page.Files {
			result = append(result, toItem(file))
		}
		pageToken = page.NextPageToken
		diagnostic.Log("drive", "files.search_page", "results", len(page.Files), "has_next_page", pageToken != "")
		if pageToken == "" {
			return result, nil
		}
	}
}

func ID(value string) string {
	if !strings.Contains(value, "/") {
		return value
	}
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`/folders/([^/?]+)`),
		regexp.MustCompile(`/d/([^/?]+)`),
		regexp.MustCompile(`[?&]id=([^&]+)`),
	}
	for _, pattern := range patterns {
		if match := pattern.FindStringSubmatch(value); len(match) == 2 {
			decoded, _ := url.QueryUnescape(match[1])
			return decoded
		}
	}
	return value
}

func oauthConfig() (*oauth2.Config, string, error) {
	file, _, _, err := credentialFile()
	if err != nil {
		return nil, "", err
	}
	installed := file.Installed
	if installed.ClientID == "" {
		return nil, "", errors.New("Google OAuth config needs a desktop client ID")
	}
	authURL, tokenURL := installed.AuthURI, installed.TokenURI
	if authURL == "" {
		authURL = "https://accounts.google.com/o/oauth2/auth"
	}
	if tokenURL == "" {
		tokenURL = "https://oauth2.googleapis.com/token"
	}
	config := &oauth2.Config{
		ClientID: installed.ClientID, ClientSecret: installed.ClientSecret,
		Endpoint: oauth2.Endpoint{AuthURL: authURL, TokenURL: tokenURL},
		Scopes:   []string{drive.DriveFileScope},
	}
	return config, installed.ClientID, nil
}

func credentialFile() (OAuthFile, CredentialSource, string, error) {
	if path := os.Getenv("STAMP_GOOGLE_OAUTH_CONFIG"); path != "" {
		file, err := readCredentialFile(path)
		return file, CredentialEnvironment, path, err
	}
	if path := OverridePath(); fileExists(path) {
		file, err := readCredentialFile(path)
		return file, CredentialInstalled, path, err
	}
	if DefaultOAuthClientID == "" || DefaultOAuthClientSecret == "" {
		return OAuthFile{}, "", "", errors.New("Google OAuth is not configured in this build; run stamp setup with an OAuth configuration")
	}
	var file OAuthFile
	file.Installed.ClientID = DefaultOAuthClientID
	file.Installed.ClientSecret = DefaultOAuthClientSecret
	file.Installed.AuthURI = "https://accounts.google.com/o/oauth2/auth"
	file.Installed.TokenURI = "https://oauth2.googleapis.com/token"
	return file, CredentialDefault, "built into stamp", nil
}

func readCredentialFile(path string) (OAuthFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return OAuthFile{}, fmt.Errorf("read Google OAuth config %s: %w", path, err)
	}
	var file OAuthFile
	if err := json.Unmarshal(data, &file); err != nil {
		return OAuthFile{}, fmt.Errorf("read Google OAuth config %s: %w", path, err)
	}
	if file.Installed.ClientID == "" {
		return OAuthFile{}, errors.New("Google OAuth config needs a desktop client ID")
	}
	return file, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func loadToken(account string) (*oauth2.Token, error) {
	diagnostic.Log("drive", "keychain.load", "account", account)
	output, err := exec.Command("security", "find-generic-password", "-s", keychainService(), "-a", account, "-w").Output()
	if err != nil {
		return nil, errors.New("not logged in; run stamp login")
	}
	var token oauth2.Token
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(output))), &token); err != nil {
		return nil, fmt.Errorf("read token from Keychain: %w", err)
	}
	return &token, nil
}

func saveToken(account string, token *oauth2.Token) error {
	diagnostic.Log("drive", "keychain.save", "account", account, "has_refresh_token", token.RefreshToken != "", "expiry", token.Expiry)
	data, err := json.Marshal(token)
	if err != nil {
		return err
	}
	output, err := exec.Command("security", "add-generic-password", "-U", "-s", keychainService(), "-a", account, "-w", string(data)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("save token in Keychain: %v: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func randomToken() string {
	data := make([]byte, 24)
	_, _ = rand.Read(data)
	return base64.RawURLEncoding.EncodeToString(data)
}

func keychainService() string {
	if service := strings.TrimSpace(os.Getenv("STAMP_GOOGLE_KEYCHAIN_SERVICE")); service != "" {
		return service
	}
	return defaultKeychainService
}

func escape(value string) string {
	return strings.ReplaceAll(value, "'", "\\'")
}

func toItem(file *drive.File) Item {
	version := file.HeadRevisionId
	if version == "" {
		version = strconv.FormatInt(file.Version, 10)
	}
	canEdit := file.Capabilities != nil && file.Capabilities.CanEdit
	return Item{ID: file.Id, Name: file.Name, Folder: file.MimeType == driveFolder, CanEdit: canEdit, Version: version, WebURL: file.WebViewLink, Parents: file.Parents, Props: file.AppProperties, revision: file.HeadRevisionId}
}

const (
	driveFolder = "application/vnd.google-apps.folder"
	fileFields  = "id,name,mimeType,version,headRevisionId,webViewLink,parents,appProperties,md5Checksum,capabilities(canEdit)"
)
