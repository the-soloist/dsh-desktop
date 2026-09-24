//go:build darwin && !server

package desktop

/*
#cgo CFLAGS: -mmacosx-version-min=11.0 -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework WebKit
#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>

static WKWebView *dshFindWebView(NSView *view) {
    if ([view isKindOfClass:[WKWebView class]]) return (WKWebView *)view;
    for (NSView *child in view.subviews) {
        WKWebView *webview = dshFindWebView(child);
        if (webview) return webview;
    }
    return nil;
}

static bool dshSetPageZoom(void *window, double factor) {
    WKWebView *webview = dshFindWebView([(NSWindow *)window contentView]);
    if (!webview) return false;
    // Trackpad gestures own viewport magnification; keyboard shortcuts own
    // layout zoom. Changing element sizes must not reset the user's pinch.
    webview.allowsMagnification = YES;
    webview.pageZoom = factor;
    return true;
}
*/
import "C"

import "github.com/wailsapp/wails/v3/pkg/application"

const minimumPageZoom = 50

func setNativePageZoom(window *application.WebviewWindow, factor float64) bool {
	return application.InvokeSyncWithResult(func() bool {
		handle := window.NativeWindow()
		return handle != nil && bool(C.dshSetPageZoom(handle, C.double(factor)))
	})
}

func bindNativeZoomMenu(app *application.App, zoom *pageZoom) {
	// App-menu accelerators take precedence over window key bindings on macOS.
	// Keep menu/keyboard zoom on pageZoom, independently of trackpad magnification.
	menu := application.DefaultApplicationMenu()
	for role, direction := range map[application.Role]int{
		application.ZoomIn: 1, application.ZoomOut: -1, application.ResetZoom: 0,
	} {
		menu.FindByRole(role).OnClick(func(*application.Context) { zoom.change(direction) })
	}
	menu.FindByRole(application.ResetZoom).RemoveAccelerator()
	app.Menu.Set(menu)
}
