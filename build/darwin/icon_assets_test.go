package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateAssetAcceptsTransparentMonochromeTemplate(t *testing.T) {
	path := writeTestPNG(t, 19, 19, color.NRGBA{R: 120, G: 120, B: 120, A: 255})

	err := validateAsset(path, assetSpec{name: filepath.Base(path), width: 19, height: 19, monochrome: true})
	if err != nil {
		t.Fatalf("validateAsset() error = %v", err)
	}
}

func TestValidateAssetRejectsColoredTemplate(t *testing.T) {
	path := writeTestPNG(t, 19, 19, color.NRGBA{R: 30, G: 80, B: 120, A: 255})

	err := validateAsset(path, assetSpec{name: filepath.Base(path), width: 19, height: 19, monochrome: true})
	if err == nil || !strings.Contains(err.Error(), "colored visible pixel") {
		t.Fatalf("validateAsset() error = %v, want colored pixel rejection", err)
	}
}

func TestValidateAssetAcceptsNearNeutralSource(t *testing.T) {
	path := writeTestPNG(t, 19, 19, color.NRGBA{R: 24, G: 24, B: 28, A: 255})

	err := validateAsset(path, assetSpec{name: filepath.Base(path), width: 19, height: 19, maxChannelDelta: 8})
	if err != nil {
		t.Fatalf("validateAsset() error = %v", err)
	}
}

func TestValidateAssetRejectsNonNeutralSource(t *testing.T) {
	path := writeTestPNG(t, 19, 19, color.NRGBA{R: 24, G: 24, B: 80, A: 255})

	err := validateAsset(path, assetSpec{name: filepath.Base(path), width: 19, height: 19, maxChannelDelta: 8})
	if err == nil || !strings.Contains(err.Error(), "non-neutral") {
		t.Fatalf("validateAsset() error = %v, want non-neutral rejection", err)
	}
}

func TestValidateAssetRejectsDimensionDrift(t *testing.T) {
	path := writeTestPNG(t, 18, 19, color.NRGBA{R: 0, G: 0, B: 0, A: 255})

	err := validateAsset(path, assetSpec{name: filepath.Base(path), width: 19, height: 19, monochrome: true})
	if err == nil || !strings.Contains(err.Error(), "dimensions") {
		t.Fatalf("validateAsset() error = %v, want dimension rejection", err)
	}
}

func TestValidateAssetRejectsFullyOpaqueImage(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "opaque.png")
	imageData := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			imageData.SetNRGBA(x, y, color.NRGBA{A: 255})
		}
	}
	writePNG(t, path, imageData)

	err := validateAsset(path, assetSpec{name: filepath.Base(path), width: 2, height: 2})
	if err == nil || !strings.Contains(err.Error(), "no transparent pixels") {
		t.Fatalf("validateAsset() error = %v, want alpha rejection", err)
	}
}

func TestRenderMonochromePNGPreservesAlphaAndRemovesTint(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.png")
	output := filepath.Join(directory, "output.png")
	imageData := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	imageData.SetNRGBA(0, 0, color.NRGBA{R: 30, G: 80, B: 120, A: 64})
	imageData.SetNRGBA(1, 0, color.NRGBA{A: 0})
	writePNG(t, source, imageData)

	if err := renderMonochromePNG(source, output); err != nil {
		t.Fatalf("renderMonochromePNG() error = %v", err)
	}
	file, err := os.Open(output)
	if err != nil {
		t.Fatalf("os.Open() error = %v", err)
	}
	decoded, err := png.Decode(file)
	file.Close()
	if err != nil {
		t.Fatalf("png.Decode() error = %v", err)
	}
	pixel := color.NRGBAModel.Convert(decoded.At(0, 0)).(color.NRGBA)
	if pixel.R != 70 || pixel.G != 70 || pixel.B != 70 || pixel.A != 64 {
		t.Fatalf("rendered pixel = %#v, want luminance 70 with alpha 64", pixel)
	}
}

func writeTestPNG(t *testing.T, width, height int, visible color.NRGBA) string {
	t.Helper()

	directory := t.TempDir()
	path := filepath.Join(directory, "asset.png")
	imageData := image.NewNRGBA(image.Rect(0, 0, width, height))
	imageData.SetNRGBA(width/2, height/2, visible)
	writePNG(t, path, imageData)
	return path
}

func writePNG(t *testing.T, path string, imageData image.Image) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("os.Create() error = %v", err)
	}
	if err := png.Encode(file, imageData); err != nil {
		file.Close()
		t.Fatalf("png.Encode() error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}
