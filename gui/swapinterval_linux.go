package gui

import "os"

// beforeGio turns off the GL drivers' vsync for a Wayland window, before Gio opens its display.
//
// Gio means to: it asks for swap interval 0 so that eglSwapBuffers never waits for a frame callback
// that may never come. It asks before any EGL context is current, so the request fails (NVIDIA
// answers EGL_BAD_SURFACE, Mesa EGL_BAD_CONTEXT) and the interval stays 1. A window on a hidden
// workspace gets no frame callbacks, so its second frame blocks in eglSwapBuffers; Gio answers the
// compositor's ping on that same goroutine, so the pong never goes out, and the compositor
// (Hyprland) reports the application as not responding until the window is shown again.
//
// The drivers' own switches give Gio the interval it asked for: __GL_SYNC_TO_VBLANK for NVIDIA,
// vblank_mode for Mesa. Gio still paces its frames with the compositor's callbacks. A value the
// user set is kept. X11 is left alone: there the interval is the only pacing.
//
// Remove this once Gio makes its context current before setting the interval.
func beforeGio() { swapIntervalZero(os.LookupEnv, os.Setenv) }

func swapIntervalZero(lookup func(string) (string, bool), set func(string, string) error) {
	if display, ok := lookup("WAYLAND_DISPLAY"); !ok || display == "" {
		return
	}
	for _, name := range []string{"__GL_SYNC_TO_VBLANK", "vblank_mode"} {
		if _, ok := lookup(name); !ok {
			_ = set(name, "0") // only fails on an invalid name
		}
	}
}
