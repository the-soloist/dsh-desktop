//go:build darwin && !server && zoomtest

package desktop

/*
#cgo CFLAGS: -x objective-c
#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>

// Wails' native test window exposes its WKWebView through this property.
@protocol DSHZoomTestWindow
@property (readonly) WKWebView *webView;
@end

static bool dshTestAllowsMagnification(void *window) {
    return [(id<DSHZoomTestWindow>)window webView].allowsMagnification;
}
*/
import "C"

import "github.com/wailsapp/wails/v3/pkg/application"

func testNativeMagnificationEnabled(window *application.WebviewWindow) bool {
	return application.InvokeSyncWithResult(func() bool {
		handle := window.NativeWindow()
		return handle != nil && bool(C.dshTestAllowsMagnification(handle))
	})
}
