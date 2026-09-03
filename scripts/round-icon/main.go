package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

const defaultRadiusRatio = 0.18

func main() {
	input := flag.String("input", "", "source PNG (required)")
	output := flag.String("output", "internal/appicon/dsh-desktop-icon.png", "rounded PNG")
	radiusRatio := flag.Float64("radius", defaultRadiusRatio, "corner radius as a ratio of the shortest edge")
	flag.Parse()

	if *input == "" {
		fmt.Fprintln(os.Stderr, "-input is required")
		os.Exit(2)
	}
	if err := roundIcon(*input, *output, *radiusRatio); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func roundIcon(inputPath, outputPath string, radiusRatio float64) error {
	if radiusRatio <= 0 || radiusRatio > 0.5 {
		return fmt.Errorf("radius must be within (0, 0.5], got %.3f", radiusRatio)
	}

	input, err := os.Open(inputPath)
	if err != nil {
		return fmt.Errorf("open source icon: %w", err)
	}
	defer input.Close()

	source, err := png.Decode(input)
	if err != nil {
		return fmt.Errorf("decode source icon: %w", err)
	}
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width == 0 || height == 0 {
		return fmt.Errorf("source icon is empty")
	}
	radius := float64(min(width, height)) * radiusRatio
	result := image.NewNRGBA(image.Rect(0, 0, width, height))

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			sourceColour := color.NRGBAModel.Convert(source.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.NRGBA)
			coverage := roundedRectangleCoverage(x, y, width, height, radius)
			sourceColour.A = uint8(math.Round(float64(sourceColour.A) * coverage))
			result.SetNRGBA(x, y, sourceColour)
		}
	}

	if err = os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	output, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create rounded icon: %w", err)
	}
	defer output.Close()
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err = encoder.Encode(output, result); err != nil {
		return fmt.Errorf("encode rounded icon: %w", err)
	}
	return nil
}

func roundedRectangleCoverage(x, y, width, height int, radius float64) float64 {
	px := float64(x) + 0.5
	py := float64(y) + 0.5
	centerX := math.Max(radius, math.Min(px, float64(width)-radius))
	centerY := math.Max(radius, math.Min(py, float64(height)-radius))
	distance := math.Hypot(px-centerX, py-centerY)

	// A one-pixel transition gives a smooth antialiased boundary.
	coverage := radius + 0.5 - distance
	return math.Max(0, math.Min(1, coverage))
}
