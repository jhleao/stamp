package render

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestPruneOutputsRemovesArtifactsForDeletedSources(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{
		"documents/kept.page.md",
		"outputs/documents/kept.pdf",
		"outputs/documents/removed.pdf",
		"outputs/empty/removed.pdf",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := pruneOutputs(root, []string{"documents/kept.page.md"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "outputs", "documents", "kept.pdf")); err != nil {
		t.Fatalf("current output was removed: %v", err)
	}
	for _, name := range []string{"outputs/documents/removed.pdf", "outputs/empty/removed.pdf", "outputs/empty"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(name))); !os.IsNotExist(err) {
			t.Fatalf("stale output remains at %s: %v", name, err)
		}
	}
}

func TestRenderComponents(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "theme", "components")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, contents string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("MetricCard.tsx", `export default function MetricCard({ props, children, meta }) { return <figure className="metric"><strong>{props.value}</strong><figcaption>{children}</figcaption><small>{meta.period}</small></figure>; }`)
	write("Panel.tsx", `export default function Panel({ props, children }) { return <section><MetricCard value={props.value}>{children}</MetricCard></section>; }`)

	got, err := renderComponents(root, `<Panel value="$4.2M"><em>Up 18%</em></Panel>`, map[string]any{"period": "Q3"}, "page")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`class="metric"`, `$4.2M`, `<em>Up 18%</em>`, `<small>Q3</small>`} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered component missing %q:\n%s", want, got)
		}
	}
}

func TestRenderComponentsExposeFormatThroughNesting(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "theme", "components")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, contents string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("CardFrame.tsx", `export default function CardFrame({ children, format }) { return <article data-format={format}><Panel>{children}</Panel></article>; }`)
	write("Panel.tsx", `export default function Panel({ children, format }) { return <section className={format === "deck" ? "slide" : "sheet"}>{children}</section>; }`)

	got, err := renderComponents(root, `<CardFrame>Context-aware</CardFrame>`, nil, "deck")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`data-format="deck"`, `class="slide"`, `Context-aware`} {
		if !strings.Contains(got, want) {
			t.Fatalf("nested component missing format result %q:\n%s", want, got)
		}
	}
}

func TestRenderComponentsRejectsEventAttributes(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "theme", "components")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Card.tsx"), []byte(`export default function Card({ children }) { return <div>{children}</div>; }`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := renderComponents(root, `<Card onclick="bad()">No</Card>`, nil, "page")
	if err == nil || !strings.Contains(err.Error(), "event attribute") {
		t.Fatalf("expected event attribute error, got %v", err)
	}
}

func TestRenderComponentsLeavesUnknownTags(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "theme", "components"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := renderComponents(root, `<slide><h1>Hello</h1></slide>`, nil, "deck")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `<slide><h1>Hello</h1></slide>`) {
		t.Fatalf("unknown tag changed: %s", got)
	}
}

func TestRenderSelfClosingComponent(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "theme", "components")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "PageBreak.tsx"), []byte(`export default function PageBreak() { return <hr className="page-break" />; }`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := renderComponents(root, `<p>Before</p><PageBreak /><p>After</p>`, nil, "page")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `<hr class="page-break"/><p>After</p>`) {
		t.Fatalf("self-closing component consumed its sibling: %s", got)
	}
}

func TestComponentCanRenderDataDrivenSVG(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "theme", "components")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	view := `export default function BarChart({ props, meta }) { return <svg viewBox="0 0 100 40" aria-label={props.label}>{meta.bars.map(width => <rect width={width} height="8" />)}</svg>; }`
	if err := os.WriteFile(filepath.Join(dir, "BarChart.tsx"), []byte(view), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := renderComponents(root, `<BarChart label="Revenue trend" />`, map[string]any{"bars": []any{25, 50, 75}}, "page")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(got, "<rect") != 3 || !strings.Contains(got, `aria-label="Revenue trend"`) {
		t.Fatalf("data-driven SVG did not render: %s", got)
	}
}

func TestComponentHTMLAtRendersOnlySelectedComponent(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "theme", "components")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Callout.tsx"), []byte(`export default function Callout({ props, children }) { return <aside className="callout"><b>{props.label}</b>{children}</aside>; }`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "theme", "page.css"), []byte(`.callout{padding:2rem}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ComponentHTMLAt(root, "Callout", "/files/")
	if err != nil {
		t.Fatal(err)
	}
	html := string(got)
	for _, want := range []string{`class="stamp-component-preview"`, `class="callout"`, `Example`, `Component preview`} {
		if !strings.Contains(html, want) {
			t.Fatalf("component preview missing %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, "page.html.tmpl") {
		t.Fatalf("component preview unexpectedly used a document shell: %s", html)
	}
}

func TestComponentHTMLAtUsesComponentFormat(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "theme", "components")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Card.tsx"), []byte(`export default function Card({ children, format }) { return <div data-format={format}>{children}</div>; }`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "theme", "page.css"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ComponentHTMLAt(root, "Card", "/files/")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `data-format="component"`) {
		t.Fatalf("isolated preview did not expose component format: %s", got)
	}
}

func TestComponentHTMLAtKeepsLocalFontFiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "theme", "components")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Card.tsx"), []byte(`export default function Card({ children }) { return <div>{children}</div>; }`), 0o644); err != nil {
		t.Fatal(err)
	}
	css := `@font-face{font-family:Example;src:url(theme/fonts/example.ttf);font-weight:400}`
	if err := os.WriteFile(filepath.Join(root, "theme", "page.css"), []byte(css), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ComponentHTMLAt(root, "Card", "/files/")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `url(theme/fonts/example.ttf)`) || strings.Contains(string(got), `data:font`) {
		t.Fatalf("component preview should let the browser load the original local font: %s", got)
	}
}

func TestComponentCatalogReadsOptionalMetadata(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "theme", "components")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sources := map[string]string{
		"Card.tsx": `export const metadata = {
			description: "A decisive summary card.",
			usage: "Use once per major conclusion."
		};
		export default function Card() { return <article />; }`,
		"Divider.tsx": `export default function Divider() { return <hr />; }`,
	}
	for name, source := range sources {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	catalog, err := ComponentCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog) != 2 || catalog[0] != (ComponentInfo{Name: "Card", Description: "A decisive summary card.", Usage: "Use once per major conclusion."}) || catalog[1] != (ComponentInfo{Name: "Divider"}) {
		t.Fatalf("unexpected component catalog: %#v", catalog)
	}
}

func TestComponentPropNamesDiscoversAccessAndDestructuring(t *testing.T) {
	source := []byte(`
		const { eyebrow, title: heading, subtitle = "Fallback", ...rest } = props;
		return <div>{props.count}{props?.compact}{props["aria-label"]}</div>;
	`)
	want := []string{"aria-label", "compact", "count", "eyebrow", "subtitle", "title"}
	if got := componentPropNames(source); !slices.Equal(got, want) {
		t.Fatalf("componentPropNames() = %v, want %v", got, want)
	}
}

func TestTSXComponentRendersPropsChildrenAndConditions(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "theme", "components")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	source := `export default function Card({ props, children, format }) {
		return <article className={format === "page" ? "sheet" : "other"}>
			{props.label && <strong>{props.label}</strong>}{children}
		</article>;
	}`
	if err := os.WriteFile(filepath.Join(dir, "Card.tsx"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := renderComponents(root, `<Card label="Signal"><em>Useful</em></Card>`, map[string]any{}, "page")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`class="sheet"`, `<strong>Signal</strong>`, `<em>Useful</em>`} {
		if !strings.Contains(got, want) {
			t.Fatalf("TSX output missing %q: %s", want, got)
		}
	}
}

func TestComponentCompilationCacheTracksSourceChanges(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "theme", "components")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "Card.tsx")
	write := func(label string) {
		t.Helper()
		source := `export default function Card() { return <div>` + label + `</div>; }`
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("First")
	first, err := renderComponents(root, `<Card />`, nil, "page")
	if err != nil {
		t.Fatal(err)
	}
	write("Second")
	second, err := renderComponents(root, `<Card />`, nil, "page")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first, "First") || !strings.Contains(second, "Second") {
		t.Fatalf("component cache returned stale output: first=%q second=%q", first, second)
	}
}

func TestComponentsRequireTSX(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "theme", "components")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Card.html.tmpl"), []byte(`<div>old format</div>`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := renderComponents(root, `<Card />`, nil, "page")
	if err == nil || !strings.Contains(err.Error(), "rename it to .tsx") {
		t.Fatalf("expected a useful TSX migration error, got %v", err)
	}
}

func BenchmarkRenderComponents(b *testing.B) {
	root := b.TempDir()
	dir := filepath.Join(root, "theme", "components")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		b.Fatal(err)
	}
	source := `export default function Card({ props, children }) {
		return <article className="card"><strong>{props.label}</strong>{children}</article>;
	}`
	if err := os.WriteFile(filepath.Join(dir, "Card.tsx"), []byte(source), 0o644); err != nil {
		b.Fatal(err)
	}
	document := strings.Repeat(`<Card label="Signal"><p>Useful context.</p></Card>`, 40)
	b.ResetTimer()
	for range b.N {
		if _, err := renderComponents(root, document, nil, "page"); err != nil {
			b.Fatal(err)
		}
	}
}

func TestValidateOutputRejectsExecutableAndExternalResources(t *testing.T) {
	base := "file:///safe/project/"
	unsafe := []string{
		`<html><body><img src="https://example.com/tracker.png"></body></html>`,
		`<html><body><img src="/etc/passwd"></body></html>`,
		`<html><body><p onclick="steal()">Click</p></body></html>`,
		`<html><head><style>@import "https://example.com/theme.css"</style></head></html>`,
		`<html><head><base href="file:///etc/"></head></html>`,
	}
	for _, source := range unsafe {
		if err := validateOutput([]byte(source), base); err == nil {
			t.Errorf("expected unsafe output to fail: %s", source)
		}
	}
	if err := validateOutput([]byte(`<html><head><base href="file:///safe/project/"></head><body><img src="assets/chart.png"><a href="https://example.com/report">Source</a></body></html>`), base); err != nil {
		t.Fatalf("safe local resources and ordinary links should pass: %v", err)
	}
}

func TestMarkdownOutputNamesDisambiguatePageAndDeck(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "theme", "examples")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "guide.deck.md"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, collision := markdownOutputBase(root, "theme/examples/guide.page.md", "guide", "page"); got != "guide-page" || !collision {
		t.Fatalf("page output base = %q, want guide-page", got)
	}
	if got, collision := markdownOutputBase(root, "documents/report.page.md", "report", "page"); got != "report" || collision {
		t.Fatalf("ordinary output base = %q, want report", got)
	}
}

func TestRemoveComponentIndentationPreservesMarkdownIndentation(t *testing.T) {
	source := `<Slide>
  # Result
  <Columns>
    <Card>
      - First
        - Nested
    </Card>
  </Columns>
</Slide>`
	want := `<Slide>
# Result
<Columns>
<Card>
- First
  - Nested
</Card>
</Columns>
</Slide>`
	if got := string(removeComponentIndentation([]byte(source))); got != want {
		t.Fatalf("component indentation was not normalized:\n%s\nwant:\n%s", got, want)
	}
}
