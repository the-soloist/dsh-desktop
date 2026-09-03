//go:build darwin

package appicon

import _ "embed"

// ApplicationPNG contains the macOS application icon with a Dock-safe inset.
//
//go:embed dsh-desktop-icon-macos.png
var ApplicationPNG []byte
