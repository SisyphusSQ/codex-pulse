package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
)

type assetSpec struct {
	name            string
	width           int
	height          int
	monochrome      bool
	maxChannelDelta uint8
}

var sourceAssetSpecs = []assetSpec{
	{name: "codex-pulse-app-icon-1024.png", width: 1024, height: 1024},
	{name: "codex-pulse-app-icon-64.png", width: 64, height: 64},
	{name: "codex-pulse-app-icon-32.png", width: 32, height: 32},
	{name: "codex-pulse-app-icon-16.png", width: 16, height: 16},
	{name: "codex-pulse-tray-template-19.png", width: 19, height: 19, maxChannelDelta: 8},
	{name: "codex-pulse-tray-template-19@2x.png", width: 38, height: 38, maxChannelDelta: 8},
}

var bundleAssetSpecs = []assetSpec{
	{name: "codex-pulse-tray-template.png", width: 19, height: 19, monochrome: true},
	{name: "codex-pulse-tray-template@2x.png", width: 38, height: 38, monochrome: true},
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: go run ./build/darwin <source|bundle> <icons-dir> | render <source-dir> bin/.packaging/tray")
		os.Exit(64)
	}

	if os.Args[1] == "render" {
		if len(os.Args) != 4 || filepath.Clean(os.Args[3]) != filepath.Clean("bin/.packaging/tray") {
			fmt.Fprintln(os.Stderr, "icon_assets: render output is fixed to bin/.packaging/tray")
			os.Exit(64)
		}
		if err := renderTrayAssets(os.Args[2], os.Args[3]); err != nil {
			fmt.Fprintf(os.Stderr, "icon_assets: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("rendered 2 strict monochrome tray assets")
		return
	}
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: go run ./build/darwin <source|bundle> <icons-dir> | render <source-dir> bin/.packaging/tray")
		os.Exit(64)
	}

	var specs []assetSpec
	switch os.Args[1] {
	case "source":
		specs = sourceAssetSpecs
	case "bundle":
		specs = bundleAssetSpecs
	default:
		fmt.Fprintln(os.Stderr, "icon_assets: mode must be source or bundle")
		os.Exit(64)
	}

	if err := validateAssets(os.Args[2], specs); err != nil {
		fmt.Fprintf(os.Stderr, "icon_assets: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("validated %d %s icon assets\n", len(specs), os.Args[1])
}

func validateAssets(directory string, specs []assetSpec) error {
	for _, spec := range specs {
		if err := validateAsset(filepath.Join(directory, spec.name), spec); err != nil {
			return err
		}
	}
	return nil
}

func validateAsset(path string, spec assetSpec) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", spec.name, err)
	}
	defer file.Close()

	decoded, err := png.Decode(file)
	if err != nil {
		return fmt.Errorf("decode %s as PNG: %w", spec.name, err)
	}

	bounds := decoded.Bounds()
	if bounds.Dx() != spec.width || bounds.Dy() != spec.height {
		return fmt.Errorf("%s dimensions: got %dx%d, want %dx%d", spec.name, bounds.Dx(), bounds.Dy(), spec.width, spec.height)
	}

	var hasTransparent, hasVisible bool
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pixel := color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
			if pixel.A == 0 {
				hasTransparent = true
				continue
			}
			hasVisible = true
			if spec.monochrome && (pixel.R != pixel.G || pixel.G != pixel.B) {
				return fmt.Errorf("%s contains a colored visible pixel at %d,%d (rgba=%d,%d,%d,%d)", spec.name, x, y, pixel.R, pixel.G, pixel.B, pixel.A)
			}
			if spec.maxChannelDelta > 0 && channelDelta(pixel) > spec.maxChannelDelta {
				return fmt.Errorf("%s contains a non-neutral visible pixel at %d,%d", spec.name, x, y)
			}
		}
	}

	if !hasVisible {
		return fmt.Errorf("%s contains no visible pixels", spec.name)
	}
	if !hasTransparent {
		return fmt.Errorf("%s has no transparent pixels", spec.name)
	}
	return nil
}

func channelDelta(pixel color.NRGBA) uint8 {
	minimum := min(pixel.R, pixel.G, pixel.B)
	maximum := max(pixel.R, pixel.G, pixel.B)
	return maximum - minimum
}

func renderTrayAssets(sourceDirectory, outputDirectory string) error {
	if err := validateAssets(sourceDirectory, sourceAssetSpecs); err != nil {
		return err
	}
	if err := os.RemoveAll(outputDirectory); err != nil {
		return fmt.Errorf("clean tray output: %w", err)
	}
	if err := os.MkdirAll(outputDirectory, 0o755); err != nil {
		return fmt.Errorf("create tray output: %w", err)
	}

	mappings := []struct {
		source string
		output string
	}{
		{source: "codex-pulse-tray-template-19.png", output: "codex-pulse-tray-template.png"},
		{source: "codex-pulse-tray-template-19@2x.png", output: "codex-pulse-tray-template@2x.png"},
	}
	for _, mapping := range mappings {
		if err := renderMonochromePNG(filepath.Join(sourceDirectory, mapping.source), filepath.Join(outputDirectory, mapping.output)); err != nil {
			return err
		}
	}
	return validateAssets(outputDirectory, bundleAssetSpecs)
}

func renderMonochromePNG(sourcePath, outputPath string) error {
	file, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open tray source: %w", err)
	}
	decoded, err := png.Decode(file)
	closeErr := file.Close()
	if err != nil {
		return fmt.Errorf("decode tray source: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("close tray source: %w", closeErr)
	}

	bounds := decoded.Bounds()
	input := image.NewNRGBA(bounds)
	draw.Draw(input, bounds, decoded, bounds.Min, draw.Src)
	output := image.NewNRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pixel := input.NRGBAAt(x, y)
			luminance := uint8((299*uint32(pixel.R) + 587*uint32(pixel.G) + 114*uint32(pixel.B) + 500) / 1000)
			output.SetNRGBA(x, y, color.NRGBA{R: luminance, G: luminance, B: luminance, A: pixel.A})
		}
	}

	outputFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create tray output: %w", err)
	}
	if err := png.Encode(outputFile, output); err != nil {
		outputFile.Close()
		return fmt.Errorf("encode tray output: %w", err)
	}
	if err := outputFile.Close(); err != nil {
		return fmt.Errorf("close tray output: %w", err)
	}
	return nil
}
