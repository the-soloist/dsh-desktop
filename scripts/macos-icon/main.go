package main

import (
	"flag"
	"fmt"
	"image/png"
	"os"
	"path/filepath"

	"github.com/jackmordaunt/icns/v2"
)

func main() {
	inputPath := flag.String("input", "internal/appicon/dsh-desktop-icon.png", "source PNG")
	outputPath := flag.String("output", "dist/intermediate/macos/DshDesktop.icns", "ICNS output")
	flag.Parse()

	if err := generateICNS(*inputPath, *outputPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generateICNS(inputPath, outputPath string) error {
	input, err := os.Open(inputPath)
	if err != nil {
		return fmt.Errorf("open icon: %w", err)
	}
	image, err := png.Decode(input)
	closeErr := input.Close()
	if err != nil {
		return fmt.Errorf("decode icon: %w", err)
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	output, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create ICNS: %w", err)
	}
	if err = icns.Encode(output, image); err != nil {
		_ = output.Close()
		return fmt.Errorf("encode ICNS: %w", err)
	}
	return output.Close()
}
