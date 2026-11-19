# Main window: open at a chosen size and position without a visible jump

**Status:** implementation plan (not yet applied)
**Scope:** the main `Frequency Browser` window only. The Settings and Import
windows already use `Hidden: true` + `Show()` and centre themselves while open.

## The problem

`main.go` currently creates the main window with `Width: 1024`, `Height: 768`,
and no `Hidden` flag. Wails maps the window as soon as it is created; the window
manager picks a place (usually the centre of the current monitor), and any later
`SetPosition` / `SetSize` is a **second** visible change. The result is the
window appearing somewhere for a frame or two and then jumping to its real
place.

The fix is not "move it faster". It is to make sure the window is **never
mapped until its size and position are already correct**:

1. create it hidden;
2. give it its final size while it is still hidden;
3. put its final position into the window's pre-map geometry hints;
4. map it (or map it transparent, correct the frame offset, then reveal it);
5. show it to the user only at the end.

The reference implementation of this technique is the user's `renamer`
project, specifically:

- `/home/ken/src/renamer/main.go` — hidden window + saved rectangle in the
  creation options
- `/home/ken/src/renamer/app.go` — `ShowMain`, `showMainWindowFallback`,
  `placeMainWindow`, `focusMainWindow`
- `/home/ken/src/renamer/window_place_linux.go` — the GTK4/X11 pre-map
  placement C code
- `/home/ken/src/renamer/window_place_other.go` — non-X11 stub

That project is GTK4, the same stack this app builds against. The C file can be
copied almost verbatim.

## What "hidden until positioned" means here

The window exists (GTK widget is realised) but is **not mapped**. Size is set
with `gtk_window_set_default_size` (Wails' `SetSize`), so the first map is
already the right size. Position is written into `WM_NORMAL_HINTS`
(`USPosition | PPosition`) and `XMoveWindow` before `XMapWindow`, so a
reparenting window manager maps the frame at the requested place instead of
choosing one. The window is mapped with `_NET_WM_WINDOW_OPACITY = 0` so the
one-frame frame-decoration correction is invisible, then made opaque.

This is why a plain `Show()` followed by `SetPosition()` cannot be made
jump-free: the user sees the first map.

## Repo facts the plan relies on

- Wails module: `github.com/wailsapp/wails/v3 v3.0.0-beta.20`.
- The binary links GTK4 / WebKitGTK 6.0 (verified with
  `ldd build/bin/frequency-browser` → `libgtk-4.so.1`,
  `libwebkitgtk-6.0.so.4`). The C file's build tag `!gtk3` is therefore active.
- The main window is created from `events.Linux.ApplicationStartup`
  (`main.go`). The app is effectively Linux-only at present; the non-Linux
  stub is still needed so the package builds if that changes.
- Wails registers its own `events.Linux.ApplicationStartup` listener to cache
  the screen list. Application event listeners run in **separate goroutines**,
  so `application.Get().Screen.GetPrimary()` may still be `nil` inside our
  startup callback. Do **not** compute a screen-derived position there; do it
  in `ShowMain`, which runs after the page has loaded.
- `WebviewWindowOptions.InitialPosition` defaults to `WindowCentered` (the zero
  value), and `X`/`Y` are ignored unless it is set to `WindowXY`. This is a
  common cause of "my X/Y did nothing".
- `WebviewWindow.NativeWindow()` returns the GTK widget pointer and
  `application.InvokeSync` runs a function on the thread that owns GTK/Xlib.
- `wails3 build` regenerates bindings and runs `go mod tidy`.

## Implementation

### 1. Add `window_placement.go`

This file owns the target rectangle and the show sequence. Keeping it out of
`main.go` makes the window-creation code readable.

```go
package main

import (
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// mainWindowPlace is the size and content position the main window opens at.
// It is filled in by resolveMainWindowPlace just before the window is shown,
// not when it is created: Wails caches the screens from the same startup event
// this program creates its window from, and the two handlers race.
var mainWindowPlace struct {
	X, Y, Width, Height int
}

// mainShown makes ShowMain act once, whichever of the page and the fallback
// timer reaches it first.
var mainShown atomic.Bool

// resolveMainWindowPlace fills mainWindowPlace with the size and position the
// main window should have. This is the single place to change if the window
// should open somewhere else.
func resolveMainWindowPlace() {
	const (
		width  = 1024
		height = 768
	)
	mainWindowPlace.Width = width
	mainWindowPlace.Height = height

	screen := application.Get().Screen.GetPrimary()
	if screen == nil {
		// No screen information yet: let the window manager place the window.
		// The fallback path below still corrects it once it is on screen.
		mainWindowPlace.X, mainWindowPlace.Y = 0, 0
		return
	}

	// WorkArea is in CSS/DIP pixels, the same units as WebviewWindowOptions
	// and SetSize. PhysicalWorkArea would be wrong on a scaled display.
	area := screen.WorkArea
	mainWindowPlace.X = area.X + (area.Width-width)/2
	mainWindowPlace.Y = area.Y + (area.Height-height)/2
}

// ShowMain shows the main window once its size and place are settled. The
// window is created hidden, so it is never mapped at the window manager's
// choice and then moved where it belongs.
//
// It is called by the page when the first frame is ready, and by a timer as a
// backstop. mainShown makes the second call a no-op. It is exported because
// the page calls it through the generated bindings.
func (a *App) ShowMain() {
	if mainWindow == nil || shutdownInProgress.Load() || allowMainWindowClose.Load() {
		return
	}
	if mainShown.Swap(true) {
		return
	}

	resolveMainWindowPlace()

	// Apply the size while the window is still hidden. Wails maps SetSize to
	// gtk_window_set_default_size, so the first map is already the right size.
	mainWindow.SetSize(mainWindowPlace.Width, mainWindowPlace.Height)

	x, y := mainWindowPlace.X, mainWindowPlace.Y

	// On X11 this maps the window at x,y with the place already in its hints,
	// then reveals it. False means this display cannot be positioned that way
	// (Wayland, or another platform), so show it and move it instead.
	if showWindowAt(mainWindow, x, y) {
		go verifyMainWindowPlacement(x, y)
		return
	}
	showMainWindowFallback(x, y)
}

// verifyMainWindowPlacement checks that the window really ended up at x,y once
// it is on screen, and moves it again if the window manager adjusted it.
func verifyMainWindowPlacement(x, y int) {
	time.Sleep(150 * time.Millisecond)
	if mainWindow == nil {
		return
	}
	if gotX, gotY := mainWindow.Position(); gotX != x || gotY != y {
		placeMainWindow(x, y)
	}
	focusMainWindow()
}

// showMainWindowFallback is for displays that do not let a program choose where
// a new window is mapped (Wayland does not). The manager places the window
// where it likes, and a move asked for before it has done that is discarded, so
// the window is minimised for the moment it places it, moved, then restored.
func showMainWindowFallback(x, y int) {
	mainWindow.Show()
	mainWindow.Minimise()

	go func() {
		time.Sleep(80 * time.Millisecond) // the window manager has placed it by now
		for attempt := 0; attempt < 2; attempt++ {
			if placeMainWindow(x, y) {
				break
			}
		}
		if mainWindow == nil {
			return
		}
		mainWindow.UnMinimise()

		time.Sleep(150 * time.Millisecond) // and the window is back
		if gotX, gotY := mainWindow.Position(); gotX != x || gotY != y {
			placeMainWindow(x, y)
		}
		focusMainWindow()
	}()
}

// placeMainWindow puts the window's content at x,y on the screen. The answer is
// false when the window manager did not take the move at all, so the caller can
// ask again.
//
// A reparenting window manager moves the window's frame when it is asked to move
// the window, while the position read back is the content's: the title bar is
// left between the two, and the content ends up lower than it was asked for. The
// move is read back and made again by the difference, so the content lands where
// it was asked for. Only a difference the size of a decoration is taken for one;
// a larger one is the window manager placing the window itself — a spot that no
// longer fits on the screen, say — and is left alone.
func placeMainWindow(x, y int) bool {
	if mainWindow == nil {
		return false
	}

	beforeX, beforeY := mainWindow.Position()
	if beforeX == x && beforeY == y {
		return true
	}

	mainWindow.SetPosition(x, y)

	// The move goes through the window manager, so it is not in effect the
	// moment the call returns.
	gotX, gotY := beforeX, beforeY
	for attempt := 0; attempt < 30 && gotX == beforeX && gotY == beforeY; attempt++ {
		time.Sleep(10 * time.Millisecond)
		gotX, gotY = mainWindow.Position()
	}

	dx, dy := gotX-x, gotY-y
	const decoration = 100
	if (dx != 0 || dy != 0) &&
		dx >= -decoration && dx <= decoration && dy >= -decoration && dy <= decoration {
		mainWindow.SetPosition(x-dx, y-dy)
		return true
	}
	if gotX == x && gotY == y {
		return true
	}
	if gotX == beforeX && gotY == beforeY {
		return false // the window manager did not take the move
	}

	return true // the manager placed it where it would; nothing more to do
}

// focusMainWindow asks for the main window to be raised until it has the focus.
// Coming back and being raised are two steps to the window manager, and a raise
// asked for while the window is still coming back is lost.
func focusMainWindow() {
	for attempt := 0; attempt < 10 && mainWindow != nil && !mainWindow.IsFocused(); attempt++ {
		mainWindow.Focus()
		time.Sleep(50 * time.Millisecond)
	}
}
```

> `resolveMainWindowPlace` centres a 1024×768 window in the primary work area.
> To use a fixed rectangle instead, replace its body with the four constants
> and still call it from `ShowMain`; do not compute it in the startup callback.

### 2. Change the main window creation in `main.go`

Add `"time"` to the imports, then replace the window-creation block with this:

```go
app.Event.OnApplicationEvent(events.Linux.ApplicationStartup, func(*application.ApplicationEvent) {
	mainWindow = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "Frequency Browser",
		Width:  1024,
		Height: 768,
		// Never map the window until ShowMain has placed it. A window shown
		// here is seen at the window manager's choice and then moved.
		Hidden: true,
		// Dark first frame, so there is no white flash before the page paints.
		BackgroundColour: application.NewRGB(27, 38, 54),
		URL:              "/",
		// Render the application menu as a hamburger button in this
		// window's native header bar instead of a traditional menu bar.
		// The style is needed because the default menu-bar style cannot
		// display plain top-level action items (GTK drops them with a
		// warning).
		UseApplicationMenu: true,
		Linux: application.LinuxWindow{
			MenuStyle: application.LinuxMenuStylePrimaryMenu,
		},
	})
	// Auxiliary windows must be closed before the main window, and an
	// unsaved Settings window gets a say. The hook can cancel the close,
	// so the orderly shutdown happens in requestShutdown instead.
	mainWindow.RegisterHook(events.Common.WindowClosing, onMainWindowClosing)

	// Backstop: a page that never gets as far as calling ShowMain must not
	// leave the application with no window on screen.
	time.AfterFunc(3*time.Second, appService.ShowMain)
})
```

Notes:

- Do **not** set `InitialPosition: application.WindowXY` here unless `X`/`Y` are
  actually known at creation time. If they are, `InitialPosition` **must** be
  set, because the zero value (`WindowCentered`) makes Wails ignore `X`/`Y`.
  For a screen-derived position, leave it unset and let `ShowMain` do the work.
- Keep `Width`/`Height` in the options as a sane default for the hidden widget;
  `ShowMain` re-applies the resolved size with `SetSize`.
- `appService` is assigned before `application.New`, so the method value is safe
  to capture here.

### 3. Add `window_place_linux.go` (GTK4/X11 pre-map placement)

Create the file with the exact contents in **Appendix A**. It is copied from
`/home/ken/src/renamer/window_place_linux.go`; the build tag is:

```go
//go:build linux && cgo && !gtk3 && !android && !server
```

What it does, in order:

1. `gtk_widget_realize` so the widget has a `GdkSurface` and X11 window ID.
2. Return `0` on Wayland (`!GDK_IS_X11_SURFACE`) so the caller uses the
   show-then-move fallback.
3. Write `USPosition | PPosition` and `x,y` into `WM_NORMAL_HINTS`.
4. Set `StaticGravity`, `XMoveWindow(x, y)`.
5. Set `_NET_WM_WINDOW_OPACITY = 0`, then `XMapWindow`.
6. Wait for `_NET_FRAME_EXTENTS` (up to ~20 ms) and move by the frame's
   left/top so the **content** lands at `x,y`.
7. `gtk_window_present`, re-apply the position hints and gravity (GDK rewrites
   them when it presents), then set opacity to `0xffffffff`.

Every GTK/Xlib call is made inside `application.InvokeSync`, on the thread that
owns them.

### 4. Add `window_place_other.go`

This is the exact complement of the Linux file. It is a safety net for builds
that exclude the GTK4/X11 file (GTK3, non-Linux, no cgo, `server`), not a
supported configuration of this app: Wails beta.20 itself does not build on
Linux without cgo, and the existing `SetTransientFor` calls already prevent the
`gtk3`/`server` builds. Keep the stub so the file set is complete and the
package is not broken if the build moves platforms later.

```go
//go:build !linux || !cgo || gtk3 || android || server

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// showWindowAt reports that this display cannot place a window before it is
// mapped, so ShowMain uses the show-then-move fallback.
func showWindowAt(_ *application.WebviewWindow, _, _ int) bool {
	return false
}
```

### 5. Call `ShowMain` from the page when the first frame is ready

In `frontend/src/App.svelte`, add `ShowMain` to the binding import:

```js
  import {
    ImportEiBi,
    // ...
    GetFrequencyRange,
    ShowMain,
  } from "../bindings/frequency-browser/app";
```

Then change the end of the `onMount` async IIFE. The current code ends with:

```js
      await updateStatus();
    })();
```

Replace it with:

```js
      await updateStatus();
    })().finally(() => {
      // The window was created hidden. Show it only now, with the grid built,
      // so the first frame is the finished window and not an empty one.
      ShowMain()
        .then(() => {
          // The grid was laid out while the window had no mapped surface, so
          // fitColumns may have used the wrong width. Recalculate once the
          // window is really on screen.
          requestAnimationFrame(() => handleResize());
        })
        .catch((err) => console.error("ShowMain failed:", err));
    });
```

Why here: the async block loads the saved lists and builds Tabulator before it
settles. Showing after that means the first painted frame is populated. The
3-second timer in `main.go` is the backstop if a load hangs or the page throws.
The `.finally` runs on both success and failure, so an import/radio error never
leaves an invisible application.

If the grid still measures oddly on the first frame, wait for fonts before
showing:

```js
    })().finally(async () => {
      try {
        await document.fonts.ready;
        await ShowMain();
        requestAnimationFrame(() => handleResize());
      } catch (err) {
        console.error("ShowMain failed:", err);
      }
    });
```

### 6. Regenerate bindings and build

```sh
cd /home/ken/src/frequency-browser
task generate:bindings        # the project's exact binding command
wails3 build
```

`wails3 build` already runs the `generate:bindings` task, so running it once is
enough. Confirm `frontend/bindings/frequency-browser/app.ts` exports
`ShowMain` (the task uses `-ts -i`).

## Ordering and threading rules

1. **Create hidden.** `Hidden: true` must be in the creation options. Calling
   `Hide()` after creation is too late — the window was already mapped.
2. **Size before map.** `SetSize` while hidden; Wails uses
   `gtk_window_set_default_size`, which is applied on the first map.
3. **Position in the pre-map hints.** On X11, `SetPosition` before mapping is
   not reliable: GDK rewrites `WM_NORMAL_HINTS` when it presents the window and
   drops the position. That is why `showWindowAt` sends the map request
   itself and re-applies the hints after `gtk_window_present`.
4. **GTK/Xlib only on the owning thread.** Always go through
   `application.InvokeSync` in the Go wrapper.
5. **Show once.** `mainShown` is an atomic swap, so the page and the backstop
   timer cannot race.
6. **Do not show during shutdown.** `ShowMain` checks `shutdownInProgress` and
   `allowMainWindowClose` (both already exist in `shutdown.go`).

## Gotchas

- **`InitialPosition` zero value is `WindowCentered`.** If you set `X`/`Y` in
  the options without `InitialPosition: application.WindowXY`, they are ignored.
- **Screen list is not ready in the startup callback.** Wails caches screens
  from the same event, in another goroutine. Resolve the work area in
  `ShowMain`, not in `OnApplicationEvent`.
- **Wayland cannot be positioned by the client.** `showWindowAt` returns `0`;
  the fallback lets the compositor place the window, then makes a best-effort
  move and restore. Do not promise pixel-exact placement on Wayland.
- **Opacity needs a compositor.** Without one, `_NET_WM_WINDOW_OPACITY` is
  ignored. The window is still mapped at the right place (the hints and
  `XMoveWindow` do that); only the brief frame-offset correction may be
  visible. Do not move the opacity call before `XMapWindow` if you change the
  code — the transparency is what hides the correction.
- **Frame extents may arrive late.** The loop waits up to ~20 ms. If
  `_NET_FRAME_EXTENTS` never appears (some minimal WMs), `left`/`top` stay 0
  and the content lands one title bar low. The `verifyMainWindowPlacement`
  check catches that and `placeMainWindow` corrects it.
- **Do not call `Show()` before `showWindowAt`.** That defeats the entire
  technique and reintroduces the jump.
- **The native header bar.** This app uses `UseApplicationMenu` with
  `LinuxMenuStylePrimaryMenu`, which the reference project does not. After the
  first build, verify the hamburger menu still renders and opens. If the manual
  map interferes with the header bar, force the fallback path (return `false`
  from `showWindowAt`) rather than removing the hidden-window part; the
  fallback still avoids most of the jump.
- **Tabulator `fitColumns` while hidden.** It may measure a zero-width
  container. Call `handleResize()` (which redraws the table) after the window
  is shown.

## Verification

1. **Build and test.**
   ```sh
   wails3 build
   go test ./...
   ```
   `wails3 build` exercises the GTK4/cgo file. Do **not** use
   `CGO_ENABLED=0` as a check: Wails beta.20 does not build on Linux without
   cgo. The stub in section 4 is only for the file set's completeness.
2. **First frame.** Run `wails3 dev` (or the built binary) and record the
   screen at 60 fps. Step through the recording: the first frame in which the
   window is visible must already be at the target rectangle. There must be no
   frame showing the window at the centre or at `0,0`.
3. **Read back the geometry.** Temporarily log
   `mainWindow.Position()` and `mainWindow.Size()` after `ShowMain`; they must
   equal the target. With `xdotool`:
   ```sh
   xdotool search --name "Frequency Browser" getwindowgeometry
   ```
   The reported position is the frame's; compare the content offset against
   the title-bar border.
4. **Multi-monitor.** Test on a secondary monitor with a non-zero/negative
   `WorkArea.X`/`Y`. The window must land on the intended monitor, not the
   primary one.
5. **HiDPI.** Set a 1.5× or 2× scale. The window must be the requested
   CSS-pixel size, not the physical-pixel size (use `WorkArea`, not
   `PhysicalWorkArea`).
6. **Fallback path.** Temporarily make `showWindowAt` return `false`. The
   window must still end up at the target, with no crash and no hidden window.
7. **Wayland session.** Run under Wayland. The app must not crash; the
   compositor may choose the initial place, then the fallback moves it. Record
   the result and document it as best-effort.
8. **Page failure.** Stop the asset server or break the page load. After 3
   seconds the backstop timer must show the window anyway.
9. **Shutdown.** Close the app before the page calls `ShowMain` (or during a
   slow load). The shutdown prompt and auxiliary-window ordering must still
   work, and `ShowMain` must not resurrect a closing window.

## Acceptance criteria

- The first visible frame of the main window is already at the configured
  size and content position; no intermediate visible position.
- No white flash: the first frame uses the existing
  `application.NewRGB(27, 38, 54)` background.
- The window opens at 1024×768 centred in the primary work area, or at the
  fixed rectangle chosen in `resolveMainWindowPlace`.
- `ShowMain` is idempotent and safe to call from both the page and the
  backstop timer.
- The supported GTK4/cgo build is unchanged; `window_place_other.go` is selected
  only when the Linux cgo file is excluded, so future platform builds do not
  fail on a missing `showWindowAt`.
- Closing the app before or after `ShowMain` follows the existing shutdown
  sequence: unsaved-Settings prompt, auxiliary windows first, main window last.

## Optional: restore the last position and size

If the window should reopen where it was left, instead of a fixed rectangle:

1. Add a `WindowState{Width, Height, X, Y}` JSON file (see
   `/home/ken/src/renamer/config.go` for `loadWindowState` /
   `saveWindowState`).
2. Load it at startup and keep it package-level.
3. In `resolveMainWindowPlace`, prefer the saved rectangle when it is valid
   (`Width > 0`, `Height > 0`, and it still intersects a screen's `WorkArea`);
   otherwise fall back to centring.
4. Save it from the existing `onMainWindowClosing` path **before** the window
   is destroyed, while `Position()` and `Size()` still report the real values.
   In this app that means after `confirmUnsavedSettings()` succeeds and before
   `mainWindow.Close()`.

The show sequence does not change: the saved rectangle simply becomes the
target passed to `ShowMain`.

## Appendix A: `window_place_linux.go`

Copy this file verbatim from the reference implementation:

```
/home/ken/src/renamer/window_place_linux.go
```

For completeness, its contents are:

```go
//go:build linux && cgo && !gtk3 && !android && !server

package main

/*
#cgo linux pkg-config: gtk4
#cgo linux LDFLAGS: -lX11

#include <string.h>
#include <unistd.h>
#include <gtk/gtk.h>
#include <gdk/x11/gdkx.h>
#include <X11/Xlib.h>
#include <X11/Xatom.h>
#include <X11/Xutil.h>

// frameExtents reads _NET_FRAME_EXTENTS, the border the window manager puts
// around a window: left, right, top and bottom. 0 is returned until the window
// manager has managed the window and set it.
static int frameExtents(Display *xdisplay, Window xwindow, int *left, int *top) {
	Atom atom = XInternAtom(xdisplay, "_NET_FRAME_EXTENTS", True);
	if (atom == None) {
		return 0;
	}

	Atom type;
	int format;
	unsigned long count, after;
	unsigned char *data = NULL;
	if (XGetWindowProperty(xdisplay, xwindow, atom, 0, 4, False, XA_CARDINAL,
			&type, &format, &count, &after, &data) != Success) {
		return 0;
	}
	if (data == NULL) {
		return 0;
	}
	if (count < 4 || format != 32) {
		XFree(data);
		return 0;
	}

	long *values = (long *)data;
	*left = (int)values[0];
	*top = (int)values[2];
	XFree(data);
	return 1;
}

// setPositionHints puts x,y into WM_NORMAL_HINTS as the program's own choice,
// keeping whatever else (the size limits, for one) is there.
static void setPositionHints(Display *xdisplay, Window xwindow, int x, int y) {
	XSizeHints *hints = XAllocSizeHints();
	if (hints == NULL) {
		return;
	}
	long supplied = 0;
	if (!XGetWMNormalHints(xdisplay, xwindow, hints, &supplied)) {
		hints->flags = 0;
	}
	hints->flags |= USPosition | PPosition;
	hints->x = x;
	hints->y = y;
	XSetWMNormalHints(xdisplay, xwindow, hints);
	XFree(hints);
}

// setWindowOpacity makes the whole window — frame included — this opaque, from
// 0 (invisible) to 0xffffffff (fully opaque), through the standard property the
// compositor reads.
static void setWindowOpacity(Display *xdisplay, Window xwindow, unsigned long opacity) {
	Atom atom = XInternAtom(xdisplay, "_NET_WM_WINDOW_OPACITY", False);
	XChangeProperty(xdisplay, xwindow, atom, XA_CARDINAL, 32, PropModeReplace,
		(unsigned char *)&opacity, 1);
}

// showWindowAt maps the window at x,y with the position hints already on it.
//
// GDK writes WM_NORMAL_HINTS itself when it presents a window, and drops the
// position, so the map request is sent here — with the hints set — and GTK is
// asked to present the window afterwards, when it is already mapped.
//
// A reparenting window manager places the window's frame at the hint's x,y,
// leaving the content a title bar lower. It reports that border in
// _NET_FRAME_EXTENTS as it manages the window, so the frame is moved up by it
// before the window is revealed. The window is transparent in the meantime, so it
// is never seen anywhere but where it belongs.
static int showWindowAt(void *window, int x, int y) {
	GtkWidget *widget = GTK_WIDGET(window);
	gtk_widget_realize(widget);

	GtkNative *native = gtk_widget_get_native(widget);
	if (native == NULL) {
		return 0;
	}

	GdkSurface *surface = gtk_native_get_surface(native);
	if (surface == NULL || !GDK_IS_X11_SURFACE(surface)) {
		return 0;
	}

	GdkDisplay *display = gdk_surface_get_display(surface);
	Display *xdisplay = gdk_x11_display_get_xdisplay(display);
	Window xwindow = gdk_x11_surface_get_xid(GDK_X11_SURFACE(surface));

	setPositionHints(xdisplay, xwindow, x, y);

	XSetWindowAttributes attributes;
	memset(&attributes, 0, sizeof(attributes));
	attributes.win_gravity = StaticGravity;
	XChangeWindowAttributes(xdisplay, xwindow, CWWinGravity, &attributes);

	XMoveWindow(xdisplay, xwindow, x, y);
	setWindowOpacity(xdisplay, xwindow, 0); // not seen until it is in place
	XMapWindow(xdisplay, xwindow);
	XFlush(xdisplay);

	int left = 0, top = 0;
	for (int attempt = 0; attempt < 40; attempt++) {
		if (frameExtents(xdisplay, xwindow, &left, &top)) {
			break;
		}
		usleep(500);
	}

	attributes.win_gravity = StaticGravity;
	XChangeWindowAttributes(xdisplay, xwindow, CWWinGravity, &attributes);
	XMoveWindow(xdisplay, xwindow, x - left, y - top);
	setPositionHints(xdisplay, xwindow, x - left, y - top);
	XFlush(xdisplay);

	// GTK presents the window (it is already mapped) and starts rendering, writing
	// its own hints and gravity: ours go back, and only then is the window shown.
	gtk_window_present(GTK_WINDOW(widget));
	setPositionHints(xdisplay, xwindow, x - left, y - top);
	attributes.win_gravity = StaticGravity;
	XChangeWindowAttributes(xdisplay, xwindow, CWWinGravity, &attributes);
	setWindowOpacity(xdisplay, xwindow, 0xffffffff);
	XFlush(xdisplay);

	return 1;
}
*/
import "C"

import "github.com/wailsapp/wails/v3/pkg/application"

// showWindowAt puts the window at x,y before it is first seen, so the window
// manager maps it there rather than at a place of its own choosing, and its frame
// is accounted for so the content lands where it was asked for. The answer is
// false when this display cannot be positioned this way (a Wayland session, or a
// platform without X11), and the window is put up and moved instead (see
// showMainWindowFallback).
func showWindowAt(window *application.WebviewWindow, x, y int) bool {
	shown := false

	// GTK and Xlib are only safe on the thread that owns them.
	application.InvokeSync(func() {
		native := window.NativeWindow()
		if native == nil {
			return
		}
		shown = C.showWindowAt(native, C.int(x), C.int(y)) != 0
	})

	return shown
}
```
