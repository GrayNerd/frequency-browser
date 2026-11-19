# Sub-windows: keep Settings, Import, and other auxiliary windows centred on the main window

**Status:** implementation plan (generalized for any Wails v3 application)
**Scope:** webview child windows such as Settings and Import. Native
`application.Dialog` boxes are already positioned by the platform and are out of
scope.

## What the application does today

In `main.go`:

- **Import** (`openImportWindow`, lines 35–60)
  - created with `Hidden: true` and a fixed 480×220 size;
  - `SetTransientFor(mainWindow)` is called before `Show()`;
  - there is no explicit position calculation — it relies on the window manager
    centring a transient dialog over its parent.
- **Settings** (`openSettings`, lines 63–96)
  - created with `Hidden: true` and a 420×220 size;
  - `SetTransientFor(mainWindow)` is called before the first `Show()`;
  - when the window already exists, the code re-centres it explicitly:
    ```go
    mx, my := mainWindow.Position()
    mw, mh := mainWindow.Size()
    sw, sh := settingsWindow.Size()
    settingsWindow.SetPosition(mx+(mw-sw)/2, my+(mh-sh)/2)
    ```
    but only **after** `Show()` and `Focus()`, so a drifted window is seen for a
    frame before it moves.

`SetTransientFor` is the "special code" that makes the child stay above the
parent and keeps it on the same monitor/workspace. On many Linux window
managers it also centres the child, but that is a **window-manager policy, not a
guarantee**: some WMs place transients at the pointer, at the top-left, or
wherever the last dialog was. The explicit formula is the guarantee.

`shutdown.go` already closes the auxiliary windows before the main window; keep
that ordering.

## The rule

A child is centred on the **content rectangle** of its parent:

```
childX = parentX + (parentWidth  - childWidth)  / 2
childY = parentY + (parentHeight - childHeight) / 2
```

Then:

1. clamp the result to the work area of the monitor that contains the parent,
   so a dialog is never put under a panel or off-screen;
2. move the child (correcting for the window manager's frame decoration);
3. do it again whenever the parent moves or resizes.

This works for negative monitor coordinates and for parents that span
monitors; the clamping step keeps the dialog on the monitor where the parent's
centre is.

## Why the timing matters

- **First open.** The child must not be mapped at the window manager's choice
  and then moved. Create it hidden, apply its size, set the transient parent,
  then map it at the target. On Linux/X11, reuse the pre-map placement from
  `mainwindow.md` (`showWindowAt`): it maps the window with `WM_NORMAL_HINTS`
  already set and reveals it only after the frame offset is corrected. This
  gives a centred first frame with no visible jump.
- **Re-open.** If the window is still alive, it is already visible; move it to
  the new centre and then focus it. Moving an already-visible window is
  instantaneous, but only do it when it is actually off-centre.
- **Parent move/resize.** Hook `WindowDidMove` and `WindowDidResize` on the
  parent, debounce the stream of events (≈50 ms), and re-centre after the drag
  or resize stops. Do not re-centre on every pixel of the drag: it fights the
  window manager and stutters.

## Implementation

The code below is written against Wails `v3.0.0-beta.20` (the version in
`go.mod`). The centring helpers are cross-platform; the transient-parent helper
and the X11 pre-map step are platform-specific and are guarded or optional.

### 1. Add `subwindow.go` (core helpers)

```go
package main

import (
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// subWindows is the set of auxiliary windows that follow the main window.
// Register each window once when it is created and remove it when it closes.
var (
	subWindowsMu sync.Mutex
	subWindows   []*application.WebviewWindow
)

func trackSubWindow(w *application.WebviewWindow) {
	if w == nil {
		return
	}
	subWindowsMu.Lock()
	subWindows = append(subWindows, w)
	subWindowsMu.Unlock()
}

func untrackSubWindow(w *application.WebviewWindow) {
	subWindowsMu.Lock()
	defer subWindowsMu.Unlock()
	for i, candidate := range subWindows {
		if candidate == w {
			subWindows = append(subWindows[:i], subWindows[i+1:]...)
			return
		}
	}
}

func subWindowSnapshot() []*application.WebviewWindow {
	subWindowsMu.Lock()
	defer subWindowsMu.Unlock()
	return append([]*application.WebviewWindow(nil), subWindows...)
}

// centreTarget returns the content position that centres child on parent,
// clamped to the work area of the monitor the parent is on.
func centreTarget(child, parent *application.WebviewWindow) (int, int) {
	if child == nil || parent == nil {
		return 0, 0
	}

	px, py := parent.Position()
	pw, ph := parent.Size()
	cw, ch := child.Size()

	x := px + (pw-cw)/2
	y := py + (ph-ch)/2

	// The parent's centre chooses the monitor. WorkArea is in CSS/DIP pixels,
	// the same units as Position/Size; PhysicalWorkArea would be wrong on a
	// scaled display.
	if area := workAreaFor(px+pw/2, py+ph/2); area != nil {
		x = clamp(x, area.X, area.X+area.Width-cw)
		y = clamp(y, area.Y, area.Y+area.Height-ch)
	}
	return x, y
}

func workAreaFor(x, y int) *application.Rect {
	for _, screen := range application.Get().Screen.GetAll() {
		area := screen.WorkArea
		if x >= area.X && x < area.X+area.Width &&
			y >= area.Y && y < area.Y+area.Height {
			return &area
		}
	}
	if primary := application.Get().Screen.GetPrimary(); primary != nil {
		area := primary.WorkArea
		return &area
	}
	return nil
}

func clamp(value, low, high int) int {
	if high < low {
		return low // the window is wider/taller than the work area
	}
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

// placeWindow moves w to x,y and corrects for the window manager's frame
// decoration. It returns false when the move was not taken at all.
//
// A reparenting window manager moves the window's frame when asked to move the
// window, while Position() reports the content's top-left: the title bar is
// left between the two. The move is read back and made again by the difference.
// Only a difference the size of a decoration is treated as one; a larger
// difference is the window manager placing the window itself and is left alone.
func placeWindow(w *application.WebviewWindow, x, y int) bool {
	if w == nil {
		return false
	}

	beforeX, beforeY := w.Position()
	if beforeX == x && beforeY == y {
		return true
	}

	w.SetPosition(x, y)

	// The move goes through the window manager, so it is not in effect the
	// moment the call returns.
	gotX, gotY := beforeX, beforeY
	for attempt := 0; attempt < 30 && gotX == beforeX && gotY == beforeY; attempt++ {
		time.Sleep(10 * time.Millisecond)
		gotX, gotY = w.Position()
	}

	dx, dy := gotX-x, gotY-y
	const decoration = 100
	if (dx != 0 || dy != 0) &&
		dx >= -decoration && dx <= decoration &&
		dy >= -decoration && dy <= decoration {
		w.SetPosition(x-dx, y-dy)
		return true
	}
	if gotX == x && gotY == y {
		return true
	}
	if gotX == beforeX && gotY == beforeY {
		return false // the window manager did not take the move
	}
	return true
}

// showChildCentred shows a newly created hidden child at the centre of parent.
//
// On Linux/X11 this reuses showWindowAt from mainwindow.md: the window is mapped
// with the position already in its hints, so its first frame is centred.
// Everywhere else the window is shown and then moved; the move and its readback
// run off the UI thread so a menu action is not blocked.
func showChildCentred(child, parent *application.WebviewWindow) {
	if child == nil || parent == nil {
		return
	}

	x, y := centreTarget(child, parent)

	if showWindowAt(child, x, y) {
		go func() {
			time.Sleep(150 * time.Millisecond)
			if child.IsVisible() {
				child.Focus()
			}
		}()
		return
	}

	child.Show()
	go func() {
		if placeWindow(child, x, y) {
			child.Focus()
		}
	}()
}

// centreChild moves an already-visible child back to the centre of parent.
func centreChild(child, parent *application.WebviewWindow) {
	if child == nil || parent == nil || !child.IsVisible() {
		return
	}
	x, y := centreTarget(child, parent)
	placeWindow(child, x, y)
}

// raiseChild is the re-open path. A visible child is moved and raised; a child
// that exists but is hidden goes through the centred show path so it does not
// reappear at a stale position.
func raiseChild(child, parent *application.WebviewWindow) {
	if child == nil {
		return
	}
	if !child.IsVisible() {
		showChildCentred(child, parent)
		return
	}

	// Move off the UI thread: placeWindow reads the position back through the
	// window manager and can wait a few hundred milliseconds.
	go centreChild(child, parent)
	child.Show()
	child.Focus()
}

// scheduleRecentre coalesces the stream of move/resize events into one
// correction shortly after the parent stops moving.
var (
	recentreMu    sync.Mutex
	recentreTimer *time.Timer
)

func scheduleRecentre(parent *application.WebviewWindow) {
	recentreMu.Lock()
	defer recentreMu.Unlock()

	if recentreTimer != nil {
		recentreTimer.Stop()
	}
	recentreTimer = time.AfterFunc(50*time.Millisecond, func() {
		for _, child := range subWindowSnapshot() {
			centreChild(child, parent)
		}
	})
}

// watchParent keeps every registered auxiliary window centred while the parent
// is moved or resized. Call it once, after the main window is created.
func watchParent(parent *application.WebviewWindow) {
	if parent == nil {
		return
	}
	parent.OnWindowEvent(events.Common.WindowDidMove, func(*application.WindowEvent) {
		scheduleRecentre(parent)
	})
	parent.OnWindowEvent(events.Common.WindowDidResize, func(*application.WindowEvent) {
		scheduleRecentre(parent)
	})
}
```

The timer is shared by the package, which is correct for the usual
one-main-window application. If an application has several independent parent
windows, keep one timer per parent (for example,
`map[*application.WebviewWindow]*time.Timer`) so one parent's drag cannot
cancel another's pending correction.

### 2. Add the transient-parent helper (build-tagged)

`SetTransientFor` is defined on Linux only in Wails beta.20. Putting it behind a
build tag lets the same source build on Windows and macOS, where the explicit
centring still works.

**`transient_linux.go`**

```go
//go:build linux

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// setTransientFor makes child a transient window of parent. The window manager
// then keeps it above the parent, on the same monitor/workspace, and usually
// centres it; the explicit centring code still runs to guarantee the position.
func setTransientFor(child, parent *application.WebviewWindow) {
	if child != nil && parent != nil {
		child.SetTransientFor(parent)
	}
}
```

**`transient_other.go`**

```go
//go:build !linux

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// setTransientFor is a no-op where Wails does not expose transient windows.
// The explicit centring code is what positions the dialog there.
func setTransientFor(_, _ *application.WebviewWindow) {}
```

If the application is Linux-only, these two files can be replaced by a direct
`child.SetTransientFor(parent)` call.

### 3. Use the helpers in the open functions

Replace the body of each open function with a create-once / re-centre pattern.
Capture the window in a local variable so the close handler cannot race a later
re-creation.

**Import**

```go
func openImportWindow() {
	if importWindow != nil {
		raiseChild(importWindow, mainWindow)
		return
	}

	w := application.Get().Window.NewWithOptions(application.WebviewWindowOptions{
		Title:         "Import",
		Width:         480,
		Height:        220,
		URL:           "/import.html",
		Hidden:        true,
		DisableResize: true,
		MinWidth:      480,
		MinHeight:     220,
		MaxWidth:      480,
		MaxHeight:     220,
	})
	importWindow = w

	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		untrackSubWindow(w)
		if importWindow == w {
			importWindow = nil
		}
	})

	setTransientFor(w, mainWindow)
	trackSubWindow(w)
	showChildCentred(w, mainWindow)
}
```

**Settings**

```go
func openSettings() {
	if settingsWindow != nil {
		raiseChild(settingsWindow, mainWindow)
		application.Get().Event.Emit("settingsOpened")
		return
	}

	w := application.Get().Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "Settings",
		Width:  420,
		Height: 220,
		URL:    "/settings.html",
		Hidden: true,
	})
	settingsWindow = w

	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		untrackSubWindow(w)
		if settingsWindow == w {
			settingsWindow = nil
		}
		resetSettingsState()
	})

	setTransientFor(w, mainWindow)
	trackSubWindow(w)
	showChildCentred(w, mainWindow)
	application.Get().Event.Emit("settingsOpened")
}
```

### 4. Watch the parent

After `mainWindow` is created in `main()`:

```go
mainWindow = app.Window.NewWithOptions(/* ... */)
watchParent(mainWindow)
```

That is all that is needed for the child to follow the parent. The debounce
means a drag produces one correction at the end, not a stream of moves.

### 5. Fallback if the app does not use `mainwindow.md`

`showChildCentred` calls `showWindowAt`. If the application has not implemented
the pre-map placement from `mainwindow.md`, add this stub so the helper
compiles and falls back to show-then-move:

```go
func showWindowAt(_ *application.WebviewWindow, _, _ int) bool {
	return false
}
```

With the stub, the first open still ends centred, but on X11 the window can be
seen at the window manager's choice for a frame before it moves. The pre-map
version from `mainwindow.md` removes that frame.

## Reusable wrapper (optional)

If the application has several auxiliary windows, wrap the pattern in a struct
instead of repeating the pointer/close-handler code:

```go
type SubWindow struct {
	parent  *application.WebviewWindow
	window  *application.WebviewWindow
	options application.WebviewWindowOptions
	onClose func()
}

func NewSubWindow(parent *application.WebviewWindow, options application.WebviewWindowOptions) *SubWindow {
	options.Hidden = true
	return &SubWindow{parent: parent, options: options}
}

func (s *SubWindow) Open() *application.WebviewWindow {
	if s.window != nil {
		raiseChild(s.window, s.parent)
		return s.window
	}

	w := application.Get().Window.NewWithOptions(s.options)
	s.window = w
	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		untrackSubWindow(w)
		if s.window == w {
			s.window = nil
		}
		if s.onClose != nil {
			s.onClose()
		}
	})

	setTransientFor(w, s.parent)
	trackSubWindow(w)
	showChildCentred(w, s.parent)
	return w
}

func (s *SubWindow) Window() *application.WebviewWindow {
	return s.window
}

func (s *SubWindow) Close() {
	if s.window != nil {
		s.window.Close()
	}
}
```

The close path stays compatible with `shutdown.go`: `s.Close()` closes the
native window, and the existing `WindowClosing` handler clears the pointer.

## Platform notes

### Linux / X11

- `SetTransientFor` + explicit centring gives the best result.
- `showWindowAt` (from `mainwindow.md`) maps the dialog at the target with
  `WM_NORMAL_HINTS`, waits for `_NET_FRAME_EXTENTS`, corrects the title-bar
  offset, then reveals it. The first visible frame is centred.
- `placeWindow` corrects a reparenting window manager's content/frame offset on
  subsequent moves.

### Linux / Wayland

- A client cannot choose an absolute position. `SetPosition` is a no-op or may
  be ignored.
- `SetTransientFor` is the useful part: the compositor knows the child belongs
  to the parent and normally centres it.
- Treat centring as **best effort**. Do not promise pixel-perfect placement;
  verify on the target compositor.
- The fallback `Show()` + `placeWindow()` path is harmless and keeps the code
  working when a compositor does honour the move.

### Windows

- `SetTransientFor` is not exposed in Wails beta.20, so `transient_other.go`
  supplies the no-op.
- `Position`, `Size`, `SetPosition`, and `SetSize` work; the explicit centring
  is what positions the dialog.
- The dialog is not an owned window, so it can go behind the parent. If that
  matters, use a native owner handle (`SetWindowLongPtr` / `GWLP_HWNDPARENT`)
  in a build-tagged helper, or use a native modal dialog where appropriate.

### macOS

- `SetTransientFor` is not exposed in beta.20. `AttachModal` presents a window
  as a sheet attached to the parent, which macOS centres and animates.
- For a non-modal helper window, the explicit `SetPosition` formula works. A
  native child window (`addChildWindow:ordered:`) would need CGO/Objective-C.

### All platforms

- Use `WorkArea`, not `PhysicalWorkArea`, so HiDPI scaling does not change the
  requested logical size.
- Parent coordinates may be negative on a monitor left of the primary. The
  formula handles that; only the clamp needs the correct work area.
- If the child is larger than the parent, the formula yields a negative offset;
  the clamp keeps it on-screen. Do not shrink the child just to fit the parent.

## Edge cases

- **Fixed-size dialogs.** Keep `DisableResize`, `MinWidth/MinHeight`, and
  `MaxWidth/MaxHeight` equal to `Width/Height`. A resizable dialog can change
  size after creation; then re-centre on its `WindowDidResize` too, debounced.
- **Dynamic content.** If an error message makes the page taller, the native
  window does not resize by itself. Either size the window for the worst case or
  call `SetSize` and then `centreChild`.
- **User moves the dialog.** It stays where the user put it until the parent
  moves or the dialog is reopened; then it snaps back. This is intentional.
  Hooking the child's own `WindowDidMove` to force it back would fight the user.
- **Window destroyed.** Wails cannot revive a closed window. Always set the
  pointer to nil in `WindowClosing` and create a fresh hidden window on the next
  open.
- **Shutdown.** Keep closing auxiliary windows before the main window. The
  debounce timer may fire while windows are closing; `centreChild` checks
  `IsVisible()` and returns for destroyed windows. Add a `shuttingDown` guard if
  the platform reports a window visible during teardown.
- **Modal vs modeless.** `SetTransientFor` keeps the child above the parent but
  does not block input. If the application needs a modal dialog, use
  `application.Dialog` or the platform's modal API instead of a webview
  sub-window.

## Verification

1. **First open.** Open Settings and Import. The first visible frame must be
   centred on the main window. With `showWindowAt`, there must be no frame at
   another position. Without it, accept one frame at the window manager's
   choice and document it.
2. **Re-open.** Close and reopen each dialog several times. Each must open
   centred, never at the previous position.
3. **Move the parent.** Drag the main window across the screen. The dialog must
   follow and end centred within 1–2 px. There must be no jitter during the
   drag; the correction happens after it stops.
4. **Resize the parent.** Resize the main window from each edge. The dialog must
   re-centre (and stay on-screen).
5. **Multi-monitor.** Move the main window to a secondary monitor with a
   non-zero or negative origin, then open a dialog. It must be centred on that
   monitor, not the primary one.
6. **HiDPI.** Repeat at 1.5× or 2× scale. The dialog must have the requested
   CSS-pixel size and be centred.
7. **Small work area.** Make the work area smaller than the dialog; the dialog
   must be clamped so its title bar and buttons are reachable.
8. **Wayland.** Run under Wayland. The app must not crash; the compositor may
   choose the position. Record the result as best-effort.
9. **Shutdown.** Open both dialogs, then quit. The unsaved-Settings prompt must
   still work, and the auxiliary windows must close before the main window.
10. **Geometry readback.** With `xdotool`:
    ```sh
    xdotool search --name "Frequency Browser" getwindowgeometry
    xdotool search --name "Settings" getwindowgeometry
    ```
    Compare the centres; allow for the title-bar frame offset.

## Acceptance criteria

- A newly opened auxiliary window is centred on the main window's content area
  within 1–2 px.
- Reopening an existing window re-centres it every time.
- Moving or resizing the main window re-centres open auxiliary windows after the
  move/resize stops, without visible jitter.
- The dialog stays on the monitor that contains the main window's centre and is
  clamped to that monitor's work area.
- No dialog is ever left hidden, destroyed, or pointing at a stale window after
  close/reopen.
- The code builds on Linux with the GTK4/cgo path; the build-tagged transient
  helper keeps non-Linux builds compiling.
- Shutdown still closes auxiliary windows before the main window and preserves
  the unsaved-Settings prompt.

## Minimal checklist for an existing Wails v3 app

- [ ] Create each auxiliary window with `Hidden: true`.
- [ ] Keep one pointer per window; clear it in `WindowClosing`.
- [ ] Call `setTransientFor(child, parent)` before showing (Linux).
- [ ] On first open, call `showChildCentred(child, parent)`.
- [ ] On re-open, call `raiseChild(child, parent)`.
- [ ] Call `watchParent(mainWindow)` once after creating the main window.
- [ ] Keep the 50 ms debounce; do not re-centre on every move event.
- [ ] Clamp to the parent's monitor work area.
- [ ] Keep auxiliary windows closing before the main window.
