package render

import (
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

func TestPrintLayersPreservePaintTextLinksAndPages(t *testing.T) {
	if _, err := findChrome(); err != nil {
		t.Skip(err)
	}
	for _, command := range []string{"pdftotext", "pdftoppm", "pdfinfo"} {
		if _, err := exec.LookPath(command); err != nil {
			t.Skip(err)
		}
	}
	for _, kind := range []string{"page", "deck"} {
		t.Run(kind, func(t *testing.T) { verifyPrintLayers(t, kind) })
	}
}

func verifyPrintLayers(t *testing.T, kind string) {
	t.Helper()
	root := t.TempDir()
	htmlPath := filepath.Join(root, "effects.html")
	html := `<!doctype html><style>
	@page { size: 400px 600px; margin: 0; }
	* { box-sizing: border-box; } body { margin: 0; font: 16px Arial; }
	.stamp-deck slide { position: relative; display: block; width: 400px; height: 600px; padding: 30px; break-after: page; background: #f4f0ea; }
	.card { padding: 20px; background: white; border-radius: 12px; box-shadow: 0 12px 24px #48284d60; }
	.gradient { background: linear-gradient(90deg, #db7369, #79ad9b); width: 100px; height: 40px; }
	.faded { opacity: .5; } .filtered { filter: blur(.3px); }
	.card::before { content: "Generated label"; display: block; }
	</style><body class="stamp-deck"><slide>
	<div class="card"><h1>Selectable heading</h1><a href="https://example.com/roadmap">Working link</a>
	<div class="gradient"></div><p class="faded">Faded illustration label</p>
	<svg style="position:absolute;left:50px;top:540px" width="60" height="30"><rect width="60" height="30" fill="#16804a"/><text x="4" y="20" fill="white">SVG</text></svg>
	</div></slide><slide><h2>Second page</h2><p>Plain text remains vector.</p></slide></body>`
	data := []byte(html)
	if kind == "page" {
		var err error
		data, err = withPagination(data)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(htmlPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	printer, err := newChromePrinter()
	if err != nil {
		t.Fatal(err)
	}
	defer printer.Close()
	pdfPath := filepath.Join(root, "effects.pdf")
	if err := printer.Print(htmlPath, pdfPath, kind); err != nil {
		t.Fatal(err)
	}
	text, err := exec.Command("pdftotext", pdfPath, "-").CombinedOutput()
	if err != nil {
		t.Fatalf("extract text: %s: %v", text, err)
	}
	for _, want := range []string{"Selectable heading", "Working link", "Generated label", "Second page", "Plain text remains vector."} {
		if !strings.Contains(string(text), want) {
			t.Fatalf("PDF lost selectable text %q: %s", want, text)
		}
	}
	info, err := exec.Command("pdfinfo", pdfPath).CombinedOutput()
	if err != nil || !strings.Contains(string(info), "Pages:           2") {
		t.Fatalf("pagination changed: %s: %v", info, err)
	}
	pdf, err := os.ReadFile(pdfPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(pdf), "/URI (https://example.com/roadmap)") {
		t.Fatal("PDF lost its link annotation")
	}
	if strings.Contains(string(pdf), "/SMask") {
		t.Fatal("flattened PDF still contains a soft transparency mask")
	}
	prefix := filepath.Join(root, "page")
	if output, err := exec.Command("pdftoppm", "-f", "1", "-singlefile", "-r", "96", "-png", pdfPath, prefix).CombinedOutput(); err != nil {
		t.Fatalf("render PDF: %s: %v", output, err)
	}
	file, err := os.Open(prefix + ".png")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	image, err := png.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := image.At(100, 550).RGBA()
	if r > g || b > g {
		t.Fatalf("SVG paint was lost: rgb(%d,%d,%d)", r>>8, g>>8, b>>8)
	}
	tab, closeTab := chromedp.NewContext(printer.browser)
	defer closeTab()
	var layers int
	if err := chromedp.Run(tab,
		emulation.SetEmulatedMedia().WithMedia("print"),
		chromedp.Navigate("file://"+htmlPath),
		chromedp.Poll(`document.fonts.status === "loaded" && (!window.PagedPolyfill || window.__stampPagedDone === true)`, nil),
		chromedp.ActionFunc(flattenPrintLayers),
		chromedp.Evaluate(`document.querySelectorAll("[data-stamp-print-page]").length`, &layers),
	); err != nil {
		t.Fatal(err)
	}
	if layers != 1 {
		t.Fatalf("expected only the effects page to be flattened, got %d", layers)
	}
}
