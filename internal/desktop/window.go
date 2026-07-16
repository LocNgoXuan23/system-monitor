//go:build linux

// Package desktop provides the native Linux (GTK3 + WebKitGTK) shell for the
// system monitor desktop app. It is Linux-only and requires CGO.
package desktop

/*
#cgo pkg-config: gtk+-3.0 webkit2gtk-4.1
#include <gtk/gtk.h>
#include <webkit2/webkit2.h>
#include <stdlib.h>

extern void goWindowClosed(int width, int height);

// Capture the current size before the window is destroyed, then let the
// default handler proceed to "destroy".
static gboolean on_delete(GtkWidget *widget, GdkEvent *event, gpointer data) {
    int w = 0, h = 0;
    gtk_window_get_size(GTK_WINDOW(widget), &w, &h);
    goWindowClosed(w, h);
    return FALSE;
}

static void on_destroy(GtkWidget *widget, gpointer data) {
    gtk_main_quit();
}

// One-shot timer used only by tests to auto-close the window.
static gboolean auto_close(gpointer window) {
    gtk_window_close(GTK_WINDOW(window));
    return G_SOURCE_REMOVE;
}

static void run_window(const char *title, const char *url, int width, int height, int autoclose_ms) {
    gtk_init(0, NULL);
    GtkWidget *window = gtk_window_new(GTK_WINDOW_TOPLEVEL);
    gtk_window_set_title(GTK_WINDOW(window), title);
    gtk_window_set_default_size(GTK_WINDOW(window), width, height);
    // Resolve the taskbar/window icon from the installed hicolor theme.
    gtk_window_set_icon_name(GTK_WINDOW(window), "system-monitor");
    g_signal_connect(window, "delete-event", G_CALLBACK(on_delete), NULL);
    g_signal_connect(window, "destroy", G_CALLBACK(on_destroy), NULL);

    GtkWidget *webview = webkit_web_view_new();
    gtk_container_add(GTK_CONTAINER(window), webview);
    webkit_web_view_load_uri(WEBKIT_WEB_VIEW(webview), url);

    if (autoclose_ms > 0) {
        g_timeout_add(autoclose_ms, auto_close, window);
    }
    gtk_widget_show_all(window);
    gtk_main();
}
*/
import "C"

import (
	"runtime"
	"unsafe"
)

// WindowConfig configures the native window.
type WindowConfig struct {
	Title       string
	URL         string
	Width       int
	Height      int
	AutoCloseMS int            // >0 auto-closes after N ms (testing only)
	OnClose     func(w, h int) // called with the final window size as it closes
}

// activeOnClose is the callback for the single active window. The app runs one
// window at a time, so a package-level handle is sufficient.
var activeOnClose func(w, h int)

//export goWindowClosed
func goWindowClosed(width, height C.int) {
	if activeOnClose != nil {
		activeOnClose(int(width), int(height))
	}
}

// RunWindow opens the native window and blocks until it is closed. All GTK
// calls must stay on one OS thread, so the calling goroutine is locked to it.
func RunWindow(cfg WindowConfig) {
	runtime.LockOSThread()
	activeOnClose = cfg.OnClose
	ctitle := C.CString(cfg.Title)
	curl := C.CString(cfg.URL)
	defer C.free(unsafe.Pointer(ctitle))
	defer C.free(unsafe.Pointer(curl))
	C.run_window(ctitle, curl, C.int(cfg.Width), C.int(cfg.Height), C.int(cfg.AutoCloseMS))
	activeOnClose = nil
}
