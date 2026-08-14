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
)

const keychainService = "sh.stamp.google-drive"

const (
	defaultClientID     = "REMOVED_GOOGLE_OAUTH_CLIENT_ID"
	defaultClientSecret = "REMOVED_GOOGLE_OAUTH_CLIENT_SECRET"
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
	Scope    string
}

type Client struct {
	api      *drive.Service
	identity string
}

type Item struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Folder   bool              `json:"folder"`
	Version  string            `json:"version,omitempty"`
	WebURL   string            `json:"webUrl,omitempty"`
	Parents  []string          `json:"parents,omitempty"`
	Props    map[string]string `json:"properties,omitempty"`
	revision string
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
	return CredentialInfo{Source: source, Path: path, ClientID: file.Installed.ClientID, Scope: drive.DriveFileScope}, nil
}

func Login(ctx context.Context) (string, error) {
	config, clientID, err := oauthConfig()
	if err != nil {
		return "", err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer listener.Close()
	config.RedirectURL = "http://" + listener.Addr().String() + "/oauth/callback"
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
	if err := exec.Command("open", authURL).Start(); err != nil {
		return "", fmt.Errorf("open browser: %w", err)
	}
	var authCode string
	select {
	case login := <-result:
		if login.err != nil {
			return "", login.err
		}
		authCode = login.code
	case <-time.After(3 * time.Minute):
		return "", errors.New("Google login did not complete; if Google says Stamp is being tested, add this account under Google Auth Platform > Audience > Test users, or make the app Internal")
	case <-ctx.Done():
		return "", ctx.Err()
	}
	_ = server.Shutdown(context.Background())
	token, err := config.Exchange(ctx, authCode, oauth2.VerifierOption(verifier))
	if err != nil {
		return "", fmt.Errorf("exchange Google login: %w", err)
	}
	if err := saveToken(clientID, token); err != nil {
		return "", err
	}
	return "Google Drive connected", nil
}

func Logout() error {
	config, clientID, err := oauthConfig()
	if err != nil {
		return err
	}
	_ = config
	cmd := exec.Command("security", "delete-generic-password", "-s", keychainService, "-a", clientID)
	if output, err := cmd.CombinedOutput(); err != nil && !strings.Contains(string(output), "could not be found") {
		return fmt.Errorf("keychain: %v: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func New(ctx context.Context) (*Client, error) {
	config, clientID, err := oauthConfig()
	if err != nil {
		return nil, err
	}
	token, err := loadToken(clientID)
	if err != nil {
		return nil, err
	}
	httpClient := config.Client(ctx, token)
	api, err := drive.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, err
	}
	return &Client{api: api}, nil
}

func (c *Client) Get(ctx context.Context, id string) (Item, error) {
	file, err := c.api.Files.Get(id).SupportsAllDrives(true).Fields(fileFields).Context(ctx).Do()
	if err != nil {
		return Item{}, err
	}
	return toItem(file), nil
}

func (c *Client) Rename(ctx context.Context, id, name string) (Item, error) {
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
	return c.search(ctx, fmt.Sprintf("'%s' in parents and trashed=false", escape(parentID)))
}

func (c *Client) EnsureFolder(ctx context.Context, parentID, name string, props map[string]string) (Item, error) {
	items, err := c.search(ctx, fmt.Sprintf("'%s' in parents and name='%s' and mimeType='%s' and trashed=false", escape(parentID), escape(name), driveFolder))
	if err != nil {
		return Item{}, err
	}
	if len(items) > 0 {
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
	file, err := c.api.Files.Create(&drive.File{Name: name, MimeType: mime, Parents: []string{parentID}, AppProperties: props}).
		Media(contents).SupportsAllDrives(true).Fields(fileFields).Context(ctx).Do()
	if err != nil {
		return Item{}, err
	}
	return toItem(file), nil
}

func (c *Client) UpdateFile(ctx context.Context, id, mime string, contents io.Reader) (Item, error) {
	file, err := c.api.Files.Update(id, &drive.File{MimeType: mime}).Media(contents).
		SupportsAllDrives(true).Fields(fileFields).Context(ctx).Do()
	if err != nil {
		return Item{}, err
	}
	return toItem(file), nil
}

func (c *Client) UpdateNamedFile(ctx context.Context, id, name, mime string, contents io.Reader) (Item, error) {
	file, err := c.api.Files.Update(id, &drive.File{Name: name, MimeType: mime}).Media(contents).
		SupportsAllDrives(true).Fields(fileFields).Context(ctx).Do()
	if err != nil {
		return Item{}, err
	}
	return toItem(file), nil
}

func (c *Client) Trash(ctx context.Context, id string) error {
	_, err := c.api.Files.Update(id, &drive.File{Trashed: true}).SupportsAllDrives(true).Context(ctx).Do()
	return err
}

func (c *Client) Retain(ctx context.Context, item Item) error {
	if item.revision == "" {
		return errors.New("Drive did not return a revision ID")
	}
	_, err := c.api.Revisions.Update(item.ID, item.revision, &drive.Revision{KeepForever: true}).Context(ctx).Do()
	return err
}

func (c *Client) Download(ctx context.Context, id string) ([]byte, error) {
	response, err := c.api.Files.Get(id).SupportsAllDrives(true).Context(ctx).Download()
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	return io.ReadAll(io.LimitReader(response.Body, 513<<20))
}

func (c *Client) FindChildByProperty(ctx context.Context, parentID, key, value string) (Item, bool, error) {
	items, err := c.search(ctx, fmt.Sprintf("'%s' in parents and appProperties has { key='%s' and value='%s' } and trashed=false", escape(parentID), escape(key), escape(value)))
	if err != nil {
		return Item{}, false, err
	}
	if len(items) == 0 {
		return Item{}, false, nil
	}
	return items[0], true, nil
}

func (c *Client) search(ctx context.Context, query string) ([]Item, error) {
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
	var file OAuthFile
	file.Installed.ClientID = defaultClientID
	file.Installed.ClientSecret = defaultClientSecret
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
	output, err := exec.Command("security", "find-generic-password", "-s", keychainService, "-a", account, "-w").Output()
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
	data, err := json.Marshal(token)
	if err != nil {
		return err
	}
	output, err := exec.Command("security", "add-generic-password", "-U", "-s", keychainService, "-a", account, "-w", string(data)).CombinedOutput()
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

func escape(value string) string {
	return strings.ReplaceAll(value, "'", "\\'")
}

func toItem(file *drive.File) Item {
	version := file.HeadRevisionId
	if version == "" {
		version = strconv.FormatInt(file.Version, 10)
	}
	return Item{ID: file.Id, Name: file.Name, Folder: file.MimeType == driveFolder, Version: version, WebURL: file.WebViewLink, Parents: file.Parents, Props: file.AppProperties, revision: file.HeadRevisionId}
}

const (
	driveFolder = "application/vnd.google-apps.folder"
	fileFields  = "id,name,mimeType,version,headRevisionId,webViewLink,parents,appProperties,md5Checksum"
)
