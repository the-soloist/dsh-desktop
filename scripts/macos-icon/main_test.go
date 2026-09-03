package main

import (
	"image"
	"image/color"
	"testing"
)

func TestPrepareMacOSIconUsesStandardCanvasAndSafeArea(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 100, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			source.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}

	icon := prepareMacOSIcon(source)
	if got, want := icon.Bounds(), image.Rect(0, 0, 1024, 1024); got != want {
		t.Fatalf("icon bounds = %v, want %v", got, want)
	}
	if alpha := icon.NRGBAAt(99, 512).A; alpha != 0 {
		t.Fatalf("safe-area alpha = %d, want 0", alpha)
	}
	if alpha := icon.NRGBAAt(100, 512).A; alpha == 0 {
		t.Fatal("artwork does not start at the expected safe-area boundary")
	}
	if alpha := icon.NRGBAAt(923, 512).A; alpha == 0 {
		t.Fatal("artwork does not reach the expected safe-area boundary")
	}
	if alpha := icon.NRGBAAt(924, 512).A; alpha != 0 {
		t.Fatalf("safe-area alpha = %d, want 0", alpha)
	}
}

func TestMacOSArtworkBoundsPreservesAspectRatio(t *testing.T) {
	got := macOSArtworkBounds(image.Rect(0, 0, 200, 100))
	want := image.Rect(100, 306, 924, 718)
	if got != want {
		t.Fatalf("artwork bounds = %v, want %v", got, want)
	}
}
