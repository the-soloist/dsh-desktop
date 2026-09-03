package main

import "testing"

func TestRoundedRectangleCoverage(t *testing.T) {
	const (
		width  = 100
		height = 100
		radius = 20
	)
	if got := roundedRectangleCoverage(0, 0, width, height, radius); got != 0 {
		t.Fatalf("corner coverage = %v, want 0", got)
	}
	if got := roundedRectangleCoverage(width/2, 0, width, height, radius); got != 1 {
		t.Fatalf("top-centre coverage = %v, want 1", got)
	}
	if got := roundedRectangleCoverage(width/2, height/2, width, height, radius); got != 1 {
		t.Fatalf("centre coverage = %v, want 1", got)
	}
}
