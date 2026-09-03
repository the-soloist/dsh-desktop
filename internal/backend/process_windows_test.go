//go:build windows

package backend

import "testing"

func TestQuoteCommandPromptArgument(t *testing.T) {
	if got, want := quoteCommandPromptArgument(`C:\Program Files\bunx.cmd`), `"C:\Program Files\bunx.cmd"`; got != want {
		t.Fatalf("quoted path = %q, want %q", got, want)
	}
	if got, want := quoteCommandPromptArgument(`100%`), `"100%%"`; got != want {
		t.Fatalf("quoted percent = %q, want %q", got, want)
	}
}
