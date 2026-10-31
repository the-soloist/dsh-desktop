//go:build darwin && !server

package desktop

/*
#cgo CFLAGS: -mmacosx-version-min=11.0 -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

static void dshRestoreWindowSize(void *handle, int width, int height) {
    NSWindow *window = (NSWindow *)handle;
    NSRect frame = window.frame;
    frame.origin.y += frame.size.height - height;
    frame.size = NSMakeSize(width, height);
    [window setFrame:frame display:YES animate:NO];
}
*/
import "C"

import "github.com/wailsapp/wails/v3/pkg/application"

// Wails beta.16 creates an NSWindow from a content rect (minus one pixel), but
// Size returns its outer frame. Restore that saved frame after native creation
// so titlebar height is not added on every launch.
func restoreNormalWindowSize(window *application.WebviewWindow, width, height int) {
	application.InvokeSync(func() {
		if handle := window.NativeWindow(); handle != nil {
			C.dshRestoreWindowSize(handle, C.int(width), C.int(height))
		}
	})
}
