package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"

	"github.com/jackmordaunt/icns/v2"
	dshdesktop "github.com/the-soloist/dsh-desktop"
	"golang.org/x/image/draw"
)

const (
	macOSIconCanvasSize  = 1024
	macOSIconArtworkSize = 824
)

func main() {
	metadata, err := dshdesktop.CurrentMetadata()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	inputPath := flag.String("input", "internal/appicon/dsh-desktop-icon.png", "source PNG")
	outputPath := flag.String("output", filepath.Join("dist", "intermediate", "macos", metadata.DisplayName+".icns"), "ICNS output")
	pngOutputPath := flag.String("png-output", "", "optional 1024x1024 macOS PNG output")
	flag.Parse()

	if err := generateMacOSIcons(*inputPath, *pngOutputPath, *outputPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generateMacOSIcons(inputPath, pngOutputPath, icnsOutputPath string) error {
	input, err := os.Open(inputPath)
	if err != nil {
		return fmt.Errorf("open icon: %w", err)
	}
	source, err := png.Decode(input)
	closeErr := input.Close()
	if err != nil {
		return fmt.Errorf("decode icon: %w", err)
	}
	if closeErr != nil {
		return closeErr
	}

	macOSIcon := prepareMacOSIcon(source)
	if pngOutputPath != "" {
		if err = writePNG(pngOutputPath, macOSIcon); err != nil {
			return err
		}
	}
	return writeICNS(icnsOutputPath, macOSIcon)
}

func prepareMacOSIcon(source image.Image) *image.NRGBA {
	canvas := image.NewNRGBA(image.Rect(0, 0, macOSIconCanvasSize, macOSIconCanvasSize))
	destination := macOSArtworkBounds(source.Bounds())
	draw.CatmullRom.Scale(canvas, destination, source, source.Bounds(), draw.Over, nil)
	return canvas
}

func macOSArtworkBounds(source image.Rectangle) image.Rectangle {
	width, height := source.Dx(), source.Dy()
	if width <= 0 || height <= 0 {
		return image.Rectangle{}
	}
	scale := math.Min(
		float64(macOSIconArtworkSize)/float64(width),
		float64(macOSIconArtworkSize)/float64(height),
	)
	scaledWidth := int(math.Round(float64(width) * scale))
	scaledHeight := int(math.Round(float64(height) * scale))
	left := (macOSIconCanvasSize - scaledWidth) / 2
	top := (macOSIconCanvasSize - scaledHeight) / 2
	return image.Rect(left, top, left+scaledWidth, top+scaledHeight)
}

func writePNG(outputPath string, icon image.Image) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create PNG directory: %w", err)
	}
	output, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create PNG: %w", err)
	}
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err = encoder.Encode(output, icon); err != nil {
		_ = output.Close()
		return fmt.Errorf("encode PNG: %w", err)
	}
	return output.Close()
}

func writeICNS(outputPath string, icon image.Image) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	output, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create ICNS: %w", err)
	}
	if err = icns.Encode(output, icon); err != nil {
		_ = output.Close()
		return fmt.Errorf("encode ICNS: %w", err)
	}
	return output.Close()
}
