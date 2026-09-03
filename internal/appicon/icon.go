// Package appicon exposes the generated application icon to native UI code.
package appicon

import _ "embed"

// PNG contains the rounded application icon.
//
//go:embed dsh-desktop-icon.png
var PNG []byte
