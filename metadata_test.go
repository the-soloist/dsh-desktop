package dshdesktop

import "testing"

func TestCurrentMetadata(t *testing.T) {
	metadata, err := CurrentMetadata()
	if err != nil {
		t.Fatalf("CurrentMetadata() error = %v", err)
	}
	if metadata.DisplayName != "DSH Desktop" {
		t.Fatalf("DisplayName = %q", metadata.DisplayName)
	}
	if metadata.DSHURL != "http://127.0.0.1:3080" {
		t.Fatalf("DSHURL = %q", metadata.DSHURL)
	}
}
