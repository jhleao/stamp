package render

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"fmt"
	htmltemplate "html/template"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	goldhtml "github.com/yuin/goldmark/renderer/html"
	"gopkg.in/yaml.v3"
)

type Result struct {
	Source string `json:"source"`
	Output string `json:"output"`
}

type pageData struct {
	Title   string
	Meta    map[string]any
	Content htmltemplate.HTML
	CSS     htmltemplate.CSS
	BaseURL htmltemplate.URL
}

var markdown = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	goldmark.WithRendererOptions(goldhtml.WithUnsafe()),
)

//go:embed assets/paged.polyfill.min.js
var pagedJS string

func All(root string) ([]Result, error) {
	var sources []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if rel == ".stamp" || rel == "outputs" {
				return filepath.SkipDir
			}
			return nil
		}
		if kind(rel) != "" {
			sources = append(sources, rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(sources)
	if err := os.MkdirAll(filepath.Join(root, "outputs"), 0o755); err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(sources))
	var problems []string
	for _, source := range sources {
		output, err := One(root, source)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", source, err))
			continue
		}
		results = append(results, Result{Source: filepath.ToSlash(source), Output: filepath.ToSlash(output)})
	}
	if len(problems) > 0 {
		return results, fmt.Errorf("render failed:\n  %s", strings.Join(problems, "\n  "))
	}
	return results, nil
}

func One(root, source string) (string, error) {
	k := kind(source)
	if k == "" {
		return "", fmt.Errorf("unsupported source %s", source)
	}
	base := outputBase(source)
	switch k {
	case "page", "deck":
		output := outputPath(source, base+".pdf")
		return output, renderMarkdownPDF(root, source, output, k)
	case "doc":
		output := outputPath(source, base+".pdf")
		return output, renderDoc(root, source, output)
	case "fods":
		output := outputPath(source, base+".xlsx")
		return output, libreOffice(root, source, output, `xlsx:Calc MS Excel 2007 XML`)
	case "fodp":
		output := outputPath(source, base+".pdf")
		return output, libreOffice(root, source, output, "pdf")
	case "xlsx":
		output := outputPath(source, filepath.Base(source))
		return output, copyFile(filepath.Join(root, source), filepath.Join(root, output))
	}
	panic("unreachable")
}

func outputPath(source, name string) string {
	return filepath.Join("outputs", filepath.Dir(source), name)
}

func HTML(root, source string) ([]byte, error) {
	baseURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(root) + "/"}).String()
	return HTMLAt(root, source, baseURL)
}

func HTMLAt(root, source, baseURL string) ([]byte, error) {
	k := kind(source)
	if k != "page" && k != "deck" {
		return nil, fmt.Errorf("%s does not have an HTML preview", source)
	}
	data, err := os.ReadFile(filepath.Join(root, source))
	if err != nil {
		return nil, err
	}
	meta, body, err := frontMatter(data)
	if err != nil {
		return nil, err
	}
	var rendered bytes.Buffer
	if err := markdown.Convert(body, &rendered); err != nil {
		return nil, err
	}
	templateName := k + ".html.tmpl"
	cssName := k + ".css"
	templateBytes, err := os.ReadFile(filepath.Join(root, "theme", templateName))
	if err != nil {
		return nil, err
	}
	css, err := os.ReadFile(filepath.Join(root, "theme", cssName))
	if err != nil {
		return nil, err
	}
	tmpl, err := htmltemplate.New(templateName).Option("missingkey=error").Parse(string(templateBytes))
	if err != nil {
		return nil, err
	}
	title, _ := meta["title"].(string)
	view := pageData{Title: title, Meta: meta, Content: htmltemplate.HTML(rendered.String()), CSS: htmltemplate.CSS(css), BaseURL: htmltemplate.URL(baseURL)}
	var output bytes.Buffer
	if err := tmpl.Execute(&output, view); err != nil {
		return nil, err
	}
	result, err := inlineThemeFonts(root, output.Bytes())
	if err != nil {
		return nil, err
	}
	lower := strings.ToLower(string(result))
	for _, unsafe := range []string{"<script", "<iframe", "<object", "<embed"} {
		if strings.Contains(lower, unsafe) {
			return nil, fmt.Errorf("template output contains unsafe %s markup", unsafe)
		}
	}
	return result, nil
}

func inlineThemeFonts(root string, html []byte) ([]byte, error) {
	for _, name := range []string{"dmsans.ttf", "dmsans-italic.ttf"} {
		marker := []byte(`theme/fonts/` + name)
		if !bytes.Contains(html, marker) {
			continue
		}
		font, err := os.ReadFile(filepath.Join(root, "theme", "fonts", name))
		if err != nil {
			return nil, fmt.Errorf("theme font %s: %w", name, err)
		}
		dataURL := []byte("data:font/ttf;base64," + base64.StdEncoding.EncodeToString(font))
		html = bytes.ReplaceAll(html, marker, dataURL)
	}
	return html, nil
}

// BrowserPreview renders office formats to a cached PDF for Studio.
func BrowserPreview(root, source string) (string, error) {
	k := kind(source)
	previewDir := filepath.Join(root, ".stamp", "preview", "office")
	if err := os.MkdirAll(previewDir, 0o755); err != nil {
		return "", err
	}
	output := filepath.Join(previewDir, strings.ReplaceAll(filepath.ToSlash(source), "/", "-")+".pdf")
	switch k {
	case "fods", "fodp", "xlsx":
		return output, libreOfficeAbsolute(filepath.Join(root, source), output, "pdf")
	case "doc":
		return output, renderDoc(root, source, filepath.ToSlash(strings.TrimPrefix(output, root+string(filepath.Separator))))
	default:
		return "", fmt.Errorf("%s does not need an office preview", source)
	}
}

func renderMarkdownPDF(root, source, output, kind string) error {
	html, err := HTML(root, source)
	if err != nil {
		return err
	}
	if kind == "page" {
		html, err = withPagination(html)
		if err != nil {
			return err
		}
	}
	htmlDir := filepath.Join(root, ".stamp", "preview")
	if err := os.MkdirAll(htmlDir, 0o755); err != nil {
		return err
	}
	htmlPath := filepath.Join(htmlDir, strings.ReplaceAll(filepath.ToSlash(source), "/", "-")+".html")
	if err := os.WriteFile(htmlPath, html, 0o644); err != nil {
		return err
	}
	outputPath := filepath.Join(root, output)
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	chrome, err := findChrome()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chrome),
		chromedp.Flag("allow-file-access-from-files", true),
	)
	allocator, stopChrome := chromedp.NewExecAllocator(ctx, opts...)
	defer stopChrome()
	browser, closeBrowser := chromedp.NewContext(allocator)
	defer closeBrowser()

	var pdf []byte
	var paginationError string
	actions := []chromedp.Action{
		chromedp.Navigate((&url.URL{Scheme: "file", Path: filepath.ToSlash(htmlPath)}).String()),
		chromedp.Poll(`document.fonts.status === "loaded"`, nil, chromedp.WithPollingTimeout(60*time.Second)),
	}
	if kind == "page" {
		actions = append(actions,
			chromedp.Poll(`window.__stampPagedDone === true`, nil, chromedp.WithPollingTimeout(60*time.Second)),
			chromedp.Evaluate(`window.__stampPagedError || ""`, &paginationError),
		)
	}
	actions = append(actions,
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			pdf, _, err = page.PrintToPDF().
				WithPrintBackground(true).
				WithScale(1).
				WithMarginTop(0).
				WithMarginBottom(0).
				WithMarginLeft(0).
				WithMarginRight(0).
				WithPreferCSSPageSize(true).
				Do(ctx)
			return err
		}),
	)
	err = chromedp.Run(browser, actions...)
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("Chromium timed out")
	}
	if err != nil {
		return fmt.Errorf("Chromium: %w", err)
	}
	if paginationError != "" {
		return fmt.Errorf("Paged.js: %s", paginationError)
	}
	return os.WriteFile(outputPath, pdf, 0o644)
}

func withPagination(html []byte) ([]byte, error) {
	closingBody := []byte("</body>")
	index := bytes.LastIndex(html, closingBody)
	if index < 0 {
		return nil, fmt.Errorf("page template has no closing body tag")
	}
	scripts := `<script>window.PagedConfig={auto:false};</script><script>` + pagedJS + `</script>` +
		`<script>window.__stampPagedDone=false;window.__stampPagedError="";` +
		`window.PagedPolyfill.preview().then(function(){window.__stampPagedDone=true;})` +
		`.catch(function(e){window.__stampPagedError=String(e&&e.message||e);window.__stampPagedDone=true;});</script>`
	result := make([]byte, 0, len(html)+len(scripts))
	result = append(result, html[:index]...)
	result = append(result, scripts...)
	result = append(result, html[index:]...)
	return result, nil
}

func renderDoc(root, source, output string) error {
	pandoc, err := exec.LookPath("pandoc")
	if err != nil {
		return fmt.Errorf("pandoc is not installed")
	}
	temp, err := os.MkdirTemp("", "stamp-doc-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	docx := filepath.Join(temp, outputBase(source)+".docx")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	combined, err := exec.CommandContext(ctx, pandoc, filepath.Join(root, source), "-o", docx).CombinedOutput()
	if err != nil {
		return fmt.Errorf("pandoc: %v: %s", err, strings.TrimSpace(string(combined)))
	}
	return libreOfficeAbsolute(docx, filepath.Join(root, output), "pdf")
}

func libreOffice(root, source, output, filter string) error {
	return libreOfficeAbsolute(filepath.Join(root, source), filepath.Join(root, output), filter)
}

func libreOfficeAbsolute(source, output, filter string) error {
	soffice, err := exec.LookPath("soffice")
	if err != nil {
		return fmt.Errorf("LibreOffice is not installed")
	}
	temp, err := os.MkdirTemp("", "stamp-office-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	profile := filepath.Join(temp, "profile")
	out := filepath.Join(temp, "out")
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	args := []string{"--headless", "--nologo", "--nodefault", "--norestore", "-env:UserInstallation=file://" + profile, "--convert-to", filter, "--outdir", out, source}
	combined, err := exec.CommandContext(ctx, soffice, args...).CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("LibreOffice timed out")
	}
	if err != nil {
		return fmt.Errorf("LibreOffice: %v: %s", err, strings.TrimSpace(string(combined)))
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		return err
	}
	if len(entries) != 1 {
		return fmt.Errorf("LibreOffice produced %d files: %s", len(entries), strings.TrimSpace(string(combined)))
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	return copyFile(filepath.Join(out, entries[0].Name()), output)
}

func frontMatter(source []byte) (map[string]any, []byte, error) {
	meta := map[string]any{}
	if !bytes.HasPrefix(source, []byte("---\n")) {
		return meta, source, nil
	}
	end := bytes.Index(source[4:], []byte("\n---\n"))
	if end < 0 {
		return nil, nil, fmt.Errorf("front matter is not closed")
	}
	if err := yaml.Unmarshal(source[4:4+end], &meta); err != nil {
		return nil, nil, fmt.Errorf("front matter: %w", err)
	}
	return meta, source[4+end+5:], nil
}

func kind(path string) string {
	lower := strings.ToLower(filepath.ToSlash(path))
	switch {
	case strings.HasSuffix(lower, ".page.md"):
		return "page"
	case strings.HasSuffix(lower, ".deck.md"):
		return "deck"
	case strings.HasSuffix(lower, ".doc.md"):
		return "doc"
	case strings.HasSuffix(lower, ".fods"):
		return "fods"
	case strings.HasSuffix(lower, ".fodp"):
		return "fodp"
	case strings.HasSuffix(lower, ".xlsx"):
		return "xlsx"
	default:
		return ""
	}
}

func outputBase(path string) string {
	name := filepath.Base(path)
	for _, suffix := range []string{".page.md", ".deck.md", ".doc.md", ".fods", ".fodp", ".xlsx"} {
		if strings.HasSuffix(strings.ToLower(name), suffix) {
			return name[:len(name)-len(suffix)]
		}
	}
	return strings.TrimSuffix(name, filepath.Ext(name))
}

func findChrome() (string, error) {
	if configured := os.Getenv("STAMP_CHROME"); configured != "" {
		return configured, nil
	}
	known := []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
	}
	for _, candidate := range known {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("Chrome or Chromium is not installed (or set STAMP_CHROME)")
}

// ChromePath reports the browser Stamp will use for page and deck PDFs.
func ChromePath() (string, error) {
	return findChrome()
}

func copyFile(source, destination string) error {
	if filepath.Clean(source) == filepath.Clean(destination) {
		return nil
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	output, err := os.Create(destination)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
