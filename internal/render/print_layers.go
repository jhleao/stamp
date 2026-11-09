package render

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

//go:embed assets/print-layers.js
var printLayersJS string

type printLayerBounds struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

func flattenPrintLayers(ctx context.Context) error {
	var bounds []printLayerBounds
	if err := chromedp.Evaluate(printLayersJS, &bounds).Do(ctx); err != nil {
		return fmt.Errorf("prepare print backgrounds: %w", err)
	}
	if len(bounds) == 0 {
		return nil
	}
	images := make([]string, 0, len(bounds))
	for _, box := range bounds {
		if box.Width <= 0 || box.Height <= 0 {
			return fmt.Errorf("invalid print background size %.2f × %.2f", box.Width, box.Height)
		}
		data, err := page.CaptureScreenshot().WithFormat(page.CaptureScreenshotFormatPng).
			WithCaptureBeyondViewport(true).
			WithClip(&page.Viewport{X: box.X, Y: box.Y, Width: box.Width, Height: box.Height, Scale: 300.0 / 96.0}).Do(ctx)
		if err != nil {
			return fmt.Errorf("capture print background: %w", err)
		}
		opaque, err := opaquePrintPNG(data)
		if err != nil {
			return err
		}
		images = append(images, base64.StdEncoding.EncodeToString(opaque))
	}
	encoded, err := json.Marshal(images)
	if err != nil {
		return err
	}
	if err := chromedp.Evaluate(`window.__stampPrintLayers.finish(`+string(encoded)+`)`, nil).Do(ctx); err != nil {
		return fmt.Errorf("compose print layers: %w", err)
	}
	return chromedp.Poll(`Array.from(document.querySelectorAll("img[data-stamp-print-background]")).every(image => image.complete && image.naturalWidth > 0)`, nil).Do(ctx)
}

func opaquePrintPNG(data []byte) ([]byte, error) {
	source, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode print background: %w", err)
	}
	opaque := image.NewRGBA(source.Bounds())
	draw.Draw(opaque, opaque.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(opaque, opaque.Bounds(), source, source.Bounds().Min, draw.Over)
	var output bytes.Buffer
	if err := png.Encode(&output, opaque); err != nil {
		return nil, fmt.Errorf("encode print background: %w", err)
	}
	return output.Bytes(), nil
}
