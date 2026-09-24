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
    // Wails beta.16 uses magnification, which scales the viewport without
    // reflowing 100vh/fixed elements. Use WebKit's layout zoom instead.
    webview.allowsMagnification = NO;
    webview.magnification = 1.0;
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
	// Replace their callbacks too, so the old magnification path is never used.
	menu := application.DefaultApplicationMenu()
	for role, direction := range map[application.Role]int{
		application.ZoomIn: 1, application.ZoomOut: -1, application.ResetZoom: 0,
	} {
		menu.FindByRole(role).OnClick(func(*application.Context) { zoom.change(direction) })
	}
	menu.FindByRole(application.ResetZoom).RemoveAccelerator()
	app.Menu.Set(menu)
}
