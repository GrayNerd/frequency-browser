# Main window: open at a chosen size and position without a visible jump

**Status:** implemented and verified in `frequency-browser` (Wails v3.0.0-beta.20,
GTK4 / WebKitGTK 6.0, X11). This document describes the working solution so the
same technique can be ported to other Wails v3 apps (e.g. `renamer`).

**Scope:** restoring a saved main-window size/position on startup with **no
visible flash** (no frame shown at a default position and then moved).

## The problem

A Wails window that is shown and *then* repositioned always flashes: the window
manager maps it at a default place (usually centred), the user sees that frame,
and only then does the move arrive. Two naive approaches both fail:

- **Not hidden + `SetPosition` after `Show()`** — the default-position frame is
  visible before the move. This is the flash.
- **`Hidden: true` + `SetPosition` before `Show()`** — does nothing. On GTK4/X11
  a hidden window has **no `GdkSurface`**, and Wails' `window_move_x11` /
  `window_get_position_x11` both bail out early when
  `gtk_native_get_surface()` returns NULL. So the position is never applied and
  the window appears centred anyway.

The only way to avoid the flash is to make the window's **first map** already be
at the final position, using X11 pre-map placement.

## Why the obvious Wails options are not enough

- `WebviewWindowOptions.InitialPosition = WindowXY` with `X`/`Y` **is
  monitor-relative on Linux**: `applyScreenPlacement()` calls
  `setRelativePosition(X, Y)` = `move(X + monitorX, Y + monitorY)`. Saved
  *absolute* X11 coordinates must first be converted to an offset within the
  containing screen's `WorkArea`.
- That conversion needs the screen list, but Wails populates its screen cache in
  its **own** `ApplicationStartup` listener, and application-event listeners run
  in **separate goroutines**. So `app.Screen.GetAll()` returns **0 screens**
  inside your `ApplicationStartup` callback — the lookup races and fails.
- Even with `Screen` + `WindowXY` set correctly, `applyScreenPlacement()` still
  runs while the window is hidden (no surface), so the move is a no-op; the
  window only lands correctly because `run()` re-applies placement *after*
  showing — which is exactly the flash we are trying to avoid.

## The solution that works

Three parts, all in `frequency-browser`:

### 1. Defer window creation until the screen cache is populated

`main.go` registers `ApplicationStartup` and, instead of creating the window
inline, launches `createMainWindowWhenScreensReady(app)` in a goroutine. It
polls `app.Screen.GetAll()` (bounded, ~3 s) and then calls `createMainWindow`
on the UI thread via `application.InvokeSync`. By then the screen list is
available for the coordinate conversion.

### 2. Create the window hidden, then realize it with `Run()`

`createMainWindow` builds `WebviewWindowOptions` with `Hidden: true` (so the
window is never mapped at a default position), calls
`app.Window.NewWithOptions(opts)`, then `mainWindow.Run()` to build the
platform implementation while still hidden. The saved size is passed through
`Width`/`Height`.

### 3. Map at the saved position with X11 pre-map placement (`showWindowAt`)

`window_place_linux.go` (build tag `linux && cgo && !gtk3 && !android &&
!server`) contains a cgo helper `showWindowAt(window, x, y)` that:

1. `gtk_widget_realize()` the widget so a `GdkSurface` exists (this is the step
   a plain hidden window never gets).
2. Reads the X11 `Display`/`Window` from the surface.
3. Writes `WM_NORMAL_HINTS` (`USPosition | PPosition`) with the target x,y and
   sets `StaticGravity`.
4. `XMoveWindow` to x,y, sets `_NET_WM_WINDOW_OPACITY = 0`, and `XMapWindow` —
   the window is mapped **at the target**, invisibly.
5. Polls `_NET_FRAME_EXTENTS` (the WM's title-bar/border) and re-moves to
   `x - left, y - top` so the **content** lands exactly at the saved point.
6. `gtk_window_present()` (already mapped), restores the hints/gravity, sets
   opacity back to `0xffffffff`, and flushes.

Because the map happens at the final coordinates and the window is transparent
until the frame offset is corrected, there is **no visible jump**. The function
returns `false` when pre-map placement is impossible (Wayland, no X11 surface),
and the caller falls back to show-then-move.

`window_place_other.go` (build tag `!linux || !cgo || gtk3 || android ||
server`) is a stub returning `false` so other platforms still compile.

### Restore logic in `createMainWindow`

```go
mainWindow = app.Window.NewWithOptions(opts) // Hidden: true, saved Width/Height
mainWindow.RegisterHook(events.Common.WindowClosing, onMainWindowClosing)
mainWindow.Run()                              // realize impl while hidden

if s.WindowSaved && showWindowAt(mainWindow, s.WindowX, s.WindowY) {
    return                                    // mapped at saved pos, no flash
}
if s.WindowSaved {                            // fallback (e.g. Wayland)
    mainWindow.Show()
    mainWindow.SetPosition(s.WindowX, s.WindowY)
    return
}
mainWindow.Show()                             // no saved pos: centred
```

## Verified result

`xwininfo -root -tree` after launch shows the window at the saved coordinates
(the small residual offset is the WM frame, which `showWindowAt` compensates so
the content area is exact). No frame is ever shown at a default position.

## Porting checklist (e.g. to `renamer`)

- [ ] Copy `window_place_linux.go` and `window_place_other.go` verbatim; they
      only depend on `github.com/wailsapp/wails/v3/pkg/application`.
- [ ] Create the main window with `Hidden: true` and the saved `Width`/`Height`.
- [ ] If you need screen-relative math, defer creation until
      `app.Screen.GetAll()` is non-empty (separate-goroutine race).
- [ ] Call `Run()` to realize the hidden window, then `showWindowAt(win, x, y)`.
- [ ] Keep the show-then-move fallback for Wayland / non-X11.
- [ ] Save absolute X11 coordinates via `Position()`; `showWindowAt` handles the
      frame-extent correction so the content lands exactly.
- [ ] Do **not** rely on `WindowXY` for absolute restore on Linux — it is
      monitor-relative and needs the (racing) screen cache.
