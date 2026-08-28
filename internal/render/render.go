package render

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	htmltemplate "html/template"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	goldhtml "github.com/yuin/goldmark/renderer/html"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"gopkg.in/yaml.v3"

	"github.com/jhleao/stamp/internal/diagnostic"
	"github.com/jhleao/stamp/internal/theme"
)

type Result struct {
	Source string `json:"source"`
	Output string `json:"output"`
}

type ProgressFunc func(completed, total int, source string)

type pageData struct {
	Title   string
	Format  string
	Meta    map[string]any
	Content htmltemplate.HTML
	CSS     htmltemplate.CSS
	BaseURL htmltemplate.URL
}

type componentData struct {
	Props   map[string]string
	Content string
	Meta    map[string]any
	Format  string
}

const maxComponentDepth = 32

var markdown = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	goldmark.WithRendererOptions(goldhtml.WithUnsafe()),
)

//go:embed assets/paged.polyfill.min.js
var pagedJS string

func All(root string) ([]Result, error) {
	return AllWithProgress(root, nil)
}

func AllWithProgress(root string, progress ProgressFunc) ([]Result, error) {
	diagnostic.Log("render", "all.start", "root", root)
	if err := theme.CompileIfNeeded(context.Background(), root); err != nil {
		diagnostic.Log("render", "theme.error", "error", err)
		return nil, err
	}
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
	diagnostic.Log("render", "sources", "count", len(sources))
	if err := os.MkdirAll(filepath.Join(root, "outputs"), 0o755); err != nil {
		return nil, err
	}
	if err := pruneOutputs(root, sources); err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(sources))
	var problems []string
	var printer *chromePrinter
	defer func() {
		if printer != nil {
			printer.Close()
		}
	}()
	for index, source := range sources {
		if progress != nil {
			progress(index, len(sources), filepath.ToSlash(source))
		}
		started := time.Now()
		diagnostic.Log("render", "source.start", "source", source, "kind", kind(source))
		if k := kind(source); (k == "page" || k == "deck") && printer == nil {
			printer, err = newChromePrinter()
			if err != nil {
				return results, err
			}
		}
		output, err := renderOne(root, source, printer)
		if err != nil {
			diagnostic.Log("render", "source.error", "source", source, "duration", time.Since(started).Round(time.Millisecond), "error", err)
			problems = append(problems, fmt.Sprintf("%s: %v", source, err))
			continue
		}
		diagnostic.Log("render", "source.complete", "source", source, "output", output, "duration", time.Since(started).Round(time.Millisecond))
		results = append(results, Result{Source: filepath.ToSlash(source), Output: filepath.ToSlash(output)})
	}
	if progress != nil {
		progress(len(sources), len(sources), "")
	}
	if len(problems) > 0 {
		return results, fmt.Errorf("render failed:\n  %s", strings.Join(problems, "\n  "))
	}
	diagnostic.Log("render", "all.complete", "outputs", len(results))
	return results, nil
}

// pruneOutputs removes derived files whose source no longer exists. Without
// this reconciliation, a deleted document leaves an old PDF behind and push
// reasonably mistakes that PDF for a current Drive mirror.
func pruneOutputs(root string, sources []string) error {
	expected := make(map[string]bool, len(sources))
	for _, source := range sources {
		base := outputBase(source)
		switch kind(source) {
		case "page", "deck":
			base, _ = markdownOutputBase(root, source, base, kind(source))
			expected[filepath.Clean(outputPath(source, base+".pdf"))] = true
		case "doc", "fodp":
			expected[filepath.Clean(outputPath(source, base+".pdf"))] = true
		case "fods":
			expected[filepath.Clean(outputPath(source, base+".xlsx"))] = true
		case "xlsx":
			expected[filepath.Clean(outputPath(source, filepath.Base(source)))] = true
		}
	}
	outputs := filepath.Join(root, "outputs")
	var directories []string
	if err := filepath.WalkDir(outputs, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != outputs {
				directories = append(directories, path)
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if !expected[filepath.Clean(rel)] {
			return os.Remove(path)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("reconcile outputs: %w", err)
	}
	for index := len(directories) - 1; index >= 0; index-- {
		if err := os.Remove(directories[index]); err != nil && !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTEMPTY) {
			return fmt.Errorf("remove empty output directory: %w", err)
		}
	}
	return nil
}

func renderOne(root, source string, printer *chromePrinter) (string, error) {
	k := kind(source)
	if k == "" {
		return "", fmt.Errorf("unsupported source %s", source)
	}
	base := outputBase(source)
	switch k {
	case "page", "deck":
		var collision bool
		base, collision = markdownOutputBase(root, source, base, k)
		if collision {
			ambiguous := filepath.Join(root, outputPath(source, outputBase(source)+".pdf"))
			if err := os.Remove(ambiguous); err != nil && !errors.Is(err, os.ErrNotExist) {
				return "", fmt.Errorf("remove ambiguous output: %w", err)
			}
		}
		output := outputPath(source, base+".pdf")
		if printer == nil {
			var err error
			printer, err = newChromePrinter()
			if err != nil {
				return "", err
			}
			defer printer.Close()
		}
		return output, renderMarkdownPDF(root, source, output, k, printer)
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

func markdownOutputBase(root, source, base, format string) (string, bool) {
	otherFormat := "page"
	if format == "page" {
		otherFormat = "deck"
	}
	sibling := filepath.Join(root, filepath.Dir(source), base+"."+otherFormat+".md")
	if _, err := os.Stat(sibling); err == nil {
		return base + "-" + format, true
	}
	return base, false
}

func outputPath(source, name string) string {
	return filepath.Join("outputs", filepath.Dir(source), name)
}

var sourceComponentTag = regexp.MustCompile(`</?[A-Z][A-Za-z0-9.-]*(?:\s[^<>]*?)?/?>`)

// removeComponentIndentation lets authors format nested Stamp components as a
// readable tree without Markdown interpreting four structural spaces as code.
// Any indentation beyond the component depth is preserved for Markdown lists
// and fenced code.
func removeComponentIndentation(source []byte) []byte {
	lines := strings.Split(string(source), "\n")
	depth := 0
	fenced := false
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		leadingClose := 0
		if !fenced && strings.HasPrefix(trimmed, "</") {
			leadingClose = 1
		}
		lineDepth := max(0, depth-leadingClose)
		prefix := strings.Repeat(" ", lineDepth*2)
		lines[index] = strings.TrimPrefix(line, prefix)

		if strings.HasPrefix(trimmed, "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		for _, tag := range sourceComponentTag.FindAllString(trimmed, -1) {
			if strings.HasPrefix(tag, "</") {
				depth = max(0, depth-1)
			} else if !strings.HasSuffix(tag, "/>") {
				depth++
			}
		}
	}
	return []byte(strings.Join(lines, "\n"))
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
	body = removeComponentIndentation(body)
	var rendered bytes.Buffer
	if err := markdown.Convert(body, &rendered); err != nil {
		return nil, err
	}
	content, err := renderComponents(root, rendered.String(), meta, k)
	if err != nil {
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
	view := pageData{Title: title, Format: k, Meta: meta, Content: htmltemplate.HTML(content), CSS: htmltemplate.CSS(css), BaseURL: htmltemplate.URL(baseURL)}
	var output bytes.Buffer
	if err := tmpl.Execute(&output, view); err != nil {
		return nil, err
	}
	result, err := inlineThemeFonts(root, output.Bytes())
	if err != nil {
		return nil, err
	}
	return result, validateOutput(result, baseURL)
}

// ComponentHTMLAt renders one theme component with representative content.
// It deliberately bypasses the page/deck shell so Studio can inspect the
// component itself without the visual noise of a complete example document.
func ComponentHTMLAt(root, name, baseURL string) ([]byte, error) {
	return ComponentHTMLAtWith(root, name, baseURL, nil)
}

// ComponentHTMLAtWith renders a component preview with optional prop overrides
// supplied by Studio's small preview control strip.
func ComponentHTMLAtWith(root, name, baseURL string, overrides map[string]string) ([]byte, error) {
	if !validComponentName(name) {
		return nil, fmt.Errorf("invalid component name %q", name)
	}
	templatePath := componentPath(root, name)
	templateBytes, err := os.ReadFile(templatePath)
	if err != nil {
		return nil, err
	}
	props := componentPreviewProps(templateBytes)
	for key, value := range overrides {
		if componentPropName.MatchString(key) {
			props[key] = value
		}
	}
	var attributes strings.Builder
	for _, key := range sortedKeys(props) {
		attributes.WriteByte(' ')
		attributes.WriteString(key)
		attributes.WriteString(`="`)
		attributes.WriteString(htmltemplate.HTMLEscapeString(props[key]))
		attributes.WriteByte('"')
	}
	content := componentPreviewContent(name)
	source := "<" + name + attributes.String() + ">" + content + "</" + name + ">"
	meta := map[string]any{
		"title": "Component preview", "subtitle": "Shown in isolation", "category": "Example",
		"author": "Stamp", "date": "Today", "audience": "Your team", "filed": "Preview",
	}
	rendered, err := renderComponents(root, source, meta, "component")
	if err != nil {
		return nil, err
	}
	css, err := os.ReadFile(filepath.Join(root, "theme", "page.css"))
	if err != nil {
		return nil, err
	}
	result := []byte(`<!doctype html><html><head><meta charset="utf-8"><base href="` + htmltemplate.HTMLEscapeString(baseURL) +
		`"><meta name="viewport" content="width=device-width,initial-scale=1"><style>` + string(css) +
		`</style><style>html,body{min-height:100%;margin:0}.stamp-component-preview{box-sizing:border-box;min-height:100vh;padding:0;display:flex;flex-direction:column;justify-content:center}.stamp-component-preview>*{width:100%;margin:0}</style></head><body><main class="stamp-component-preview">` + rendered + `</main></body></html>`)
	return result, validateOutput(result, baseURL)
}

func componentPath(root, name string) string {
	return filepath.Join(root, "theme", "components", name+".tsx")
}

var remoteCSS = regexp.MustCompile(`(?i)(@import\s|url\(\s*['"]?\s*(?:https?:|file:|//))`)

func validateOutput(data []byte, baseURL string) error {
	document, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("validate rendered HTML: %w", err)
	}
	var visit func(*html.Node) error
	visit = func(node *html.Node) error {
		if node.Type == html.ElementNode {
			tag := strings.ToLower(node.Data)
			switch tag {
			case "script", "iframe", "object", "embed", "link":
				return fmt.Errorf("template output contains unsafe <%s> markup", tag)
			case "style":
				if node.FirstChild != nil && remoteCSS.MatchString(node.FirstChild.Data) {
					return errors.New("template CSS may not load remote or absolute resources")
				}
			case "meta":
				for _, attr := range node.Attr {
					if strings.EqualFold(attr.Key, "http-equiv") && strings.EqualFold(attr.Val, "refresh") {
						return errors.New("template output may not redirect")
					}
				}
			case "base":
				for _, attr := range node.Attr {
					if strings.EqualFold(attr.Key, "href") && attr.Val != baseURL {
						return errors.New("template may not change the project base URL")
					}
				}
			}
			for _, attr := range node.Attr {
				key, value := strings.ToLower(attr.Key), strings.TrimSpace(attr.Val)
				if strings.HasPrefix(key, "on") {
					return fmt.Errorf("template output may not use event attribute %s", attr.Key)
				}
				if key == "style" && remoteCSS.MatchString(value) {
					return errors.New("template styles may not load remote or absolute resources")
				}
				if key == "src" || key == "poster" {
					lower := strings.ToLower(value)
					if strings.HasPrefix(value, "/") || (strings.Contains(lower, ":") && !strings.HasPrefix(lower, "data:")) {
						return fmt.Errorf("template resource %s must be local to the project or an embedded data URL", value)
					}
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if err := visit(child); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(document)
}

var componentPropPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\bprops\s*\??\.\s*([A-Za-z][A-Za-z0-9_]*)`),
	regexp.MustCompile(`\bprops\s*\??\[\s*["']([A-Za-z][A-Za-z0-9_-]*)["']\s*\]`),
}
var componentPropDestructurePattern = regexp.MustCompile(`(?s)\b(?:const|let|var)\s*\{([^{}]*)\}\s*(?::[^=;\n]+)?=\s*props\b`)
var componentPropName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

func componentPropNames(template []byte) []string {
	names := map[string]bool{}
	for _, pattern := range componentPropPatterns {
		for _, match := range pattern.FindAllSubmatch(template, -1) {
			names[string(match[1])] = true
		}
	}
	for _, match := range componentPropDestructurePattern.FindAllSubmatch(template, -1) {
		for _, member := range bytes.Split(match[1], []byte(",")) {
			key := strings.TrimSpace(strings.SplitN(string(member), ":", 2)[0])
			key = strings.TrimSpace(strings.SplitN(key, "=", 2)[0])
			if componentPropName.MatchString(key) {
				names[key] = true
			}
		}
	}
	values := make(map[string]string, len(names))
	for name := range names {
		values[name] = ""
	}
	return sortedKeys(values)
}

func componentPreviewProps(template []byte) map[string]string {
	values := map[string]string{}
	for _, key := range componentPropNames(template) {
		switch strings.ToLower(key) {
		case "value", "metric", "score", "rating":
			values[key] = "94%"
		case "title", "heading":
			values[key] = "A clear component title"
		case "lead":
			values[key] = "The outcome is speed without surrendering auditability."
		case "label", "kicker", "eyebrow", "category":
			values[key] = "Example"
		case "index", "number", "no":
			values[key] = "01"
		case "cols":
			values[key] = "3"
		case "full", "compact", "inverse", "featured":
			values[key] = ""
		case "ratio", "divider":
			values[key] = ""
		case "cite", "source":
			values[key] = "Source note"
		case "image", "src", "url":
			values[key] = "data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCAxMjAwIDcwMCI+PHJlY3QgZmlsbD0iI2U3ZTVlNCIgd2lkdGg9IjEyMDAiIGhlaWdodD0iNzAwIi8+PHBhdGggZD0iTTAgNTYwIDM4MCAyNTBsMjIwIDIwMCAxNjAtMTIwIDQ0MCAyOTB2MTMwSDB6IiBmaWxsPSIjY2JjN2MzIi8+PC9zdmc+"
		default:
			values[key] = "Supporting detail"
		}
	}
	return values
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func componentPreviewContent(name string) string {
	switch name {
	case "Columns":
		return `<div><strong>First idea</strong><p>A concise supporting thought.</p></div><div><strong>Second idea</strong><p>Another useful point.</p></div><div><strong>Third idea</strong><p>A final useful point.</p></div>`
	case "Band":
		return `<h2>Proof where it counts</h2>`
	case "Figures":
		return `<div><strong>62%</strong><p>First signal</p></div><div><strong>38%</strong><p>Second signal</p></div>`
	case "Cover", "SectionCover":
		return `<h1>Make the important thing clear.</h1><p>A short, useful line of context.</p>`
	case "Customers":
		return `<span>Northstar</span><span>Fieldwork</span><span>Daylight</span>`
	default:
		return `<h3>Component preview</h3><p>Representative content shows spacing, type, color, and behavior.</p>`
	}
}

// renderComponents expands custom tags backed by theme/components/<PascalCaseName>.tsx.
// Unknown tags remain ordinary HTML, so a theme can still use lightweight CSS-only
// primitives. Components receive props, children, document meta, and format
// ("page", "deck", or "component" for an isolated preview).
func renderComponents(root, source string, meta map[string]any, format string) (string, error) {
	program, err := loadComponentProgram(root)
	if err != nil {
		return "", err
	}
	if len(program.components) == 0 {
		return source, nil
	}
	renderer, err := program.newRenderer()
	if err != nil {
		return "", err
	}
	source = program.closeSelfClosingTags(source)
	context := &html.Node{Type: html.ElementNode, DataAtom: atom.Main, Data: "main"}
	nodes, err := html.ParseFragment(strings.NewReader(source), context)
	if err != nil {
		return "", fmt.Errorf("parse rendered Markdown: %w", err)
	}
	var output strings.Builder
	for _, node := range nodes {
		expanded, expandErr := expandComponentNode(node, renderer, meta, format, 0)
		if expandErr != nil {
			return "", expandErr
		}
		output.WriteString(expanded)
	}
	return output.String(), nil
}

func expandComponentNode(node *html.Node, renderer *componentRenderer, meta map[string]any, format string, depth int) (string, error) {
	if depth > maxComponentDepth {
		return "", fmt.Errorf("components exceed maximum nesting depth of %d", maxComponentDepth)
	}
	if node.Type == html.ElementNode {
		if renderer.has(node.Data) {
			props := map[string]string{}
			for _, attr := range node.Attr {
				if strings.HasPrefix(strings.ToLower(attr.Key), "on") {
					return "", fmt.Errorf("component <%s> may not use event attribute %s", node.Data, attr.Key)
				}
				props[attr.Key] = attr.Val
			}
			var content strings.Builder
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				expanded, err := expandComponentNode(child, renderer, meta, format, depth+1)
				if err != nil {
					return "", err
				}
				content.WriteString(expanded)
			}
			view := componentData{Props: props, Content: content.String(), Meta: meta, Format: format}
			rendered, err := renderer.renderComponent(node.Data, view)
			if err != nil {
				return "", fmt.Errorf("component <%s>: %w", node.Data, err)
			}
			return expandComponentHTML(rendered, renderer, meta, format, depth+1)
		}
	}
	clone := *node
	clone.FirstChild, clone.LastChild = nil, nil
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		expanded, err := expandComponentNode(child, renderer, meta, format, depth)
		if err != nil {
			return "", err
		}
		fragments, err := html.ParseFragment(strings.NewReader(expanded), &clone)
		if err != nil {
			return "", err
		}
		for _, fragment := range fragments {
			clone.AppendChild(fragment)
		}
	}
	var output bytes.Buffer
	if err := html.Render(&output, &clone); err != nil {
		return "", err
	}
	return output.String(), nil
}

func expandComponentHTML(source string, renderer *componentRenderer, meta map[string]any, format string, depth int) (string, error) {
	source = renderer.program.closeSelfClosingTags(source)
	context := &html.Node{Type: html.ElementNode, DataAtom: atom.Main, Data: "main"}
	nodes, err := html.ParseFragment(strings.NewReader(source), context)
	if err != nil {
		return "", err
	}
	var output strings.Builder
	for _, node := range nodes {
		expanded, err := expandComponentNode(node, renderer, meta, format, depth)
		if err != nil {
			return "", err
		}
		output.WriteString(expanded)
	}
	return output.String(), nil
}

func (program *componentProgram) closeSelfClosingTags(source string) string {
	for _, component := range program.components {
		source = component.selfClosing.ReplaceAllString(source, component.explicitClosing)
	}
	return source
}

func validComponentName(name string) bool {
	if name == "" || name[0] < 'A' || name[0] > 'Z' {
		return false
	}
	for _, char := range name {
		if (char < 'A' || char > 'Z') && (char < 'a' || char > 'z') && (char < '0' || char > '9') {
			return false
		}
	}
	return true
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

// BrowserPreview renders a source to the PDF Studio displays. Markdown sources
// deliberately use the same renderer and output path as export: keeping a
// separate HTML preview here makes pagination, fonts, and page geometry drift.
func BrowserPreview(root, source string) (string, error) {
	k := kind(source)
	if k == "page" || k == "deck" {
		if err := theme.CompileIfNeeded(context.Background(), root); err != nil {
			return "", err
		}
		output, err := renderOne(root, source, nil)
		if err != nil {
			return "", err
		}
		return filepath.Join(root, output), nil
	}
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

func renderMarkdownPDF(root, source, output, kind string, printer *chromePrinter) error {
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
	return printer.Print(htmlPath, outputPath, kind)
}

type chromePrinter struct {
	browser        context.Context
	closeBrowser   context.CancelFunc
	closeAllocator context.CancelFunc
}

func newChromePrinter() (*chromePrinter, error) {
	chrome, err := findChrome()
	if err != nil {
		return nil, err
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chrome),
		chromedp.Flag("allow-file-access-from-files", true),
	)
	allocator, closeAllocator := chromedp.NewExecAllocator(context.Background(), opts...)
	browser, closeBrowser := chromedp.NewContext(allocator)
	if err := chromedp.Run(browser); err != nil {
		closeBrowser()
		closeAllocator()
		return nil, fmt.Errorf("start Chromium: %w", err)
	}
	return &chromePrinter{browser: browser, closeBrowser: closeBrowser, closeAllocator: closeAllocator}, nil
}

func (printer *chromePrinter) Close() {
	printer.closeBrowser()
	printer.closeAllocator()
}

func (printer *chromePrinter) Print(htmlPath, outputPath, kind string) error {
	tab, closeTab := chromedp.NewContext(printer.browser)
	defer closeTab()
	ctx, cancel := context.WithTimeout(tab, 60*time.Second)
	defer cancel()
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
	err := chromedp.Run(ctx, actions...)
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
