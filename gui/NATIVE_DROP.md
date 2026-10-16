# Native file drops

The GUI backend consumes the native `app.DropEvent` from
`github.com/yongjohnlee80/gio`. This is an explicit module dependency, not a
`replace`, so a consumer gets the same driver and event types as this module.
The root golib module remains Gio-free.

Drops become one ordinary `tui.PasteEvent`, routed by the App to its current
keyboard focus. A drop never clicks the widget under the pointer or opens a
file. The agent terminal therefore receives paths through its existing paste
handling, just as it does when hosted by a terminal emulator.

- URI lists accept only absolute local `file:` URIs (empty host or localhost).
  Decode percent escapes, skip URI-list comments and empty lines, shell-quote
  each decoded path when needed, then join paths with a single space.
- Reject the whole list if any entry is malformed, remote, not a file URI,
  contains a query/fragment, or decodes to invalid UTF-8, C0, DEL or C1 controls. Do not paste a
  partial list. No shell command or file read is performed by this conversion.
- Plain UTF-8 text is pasted as text with CRLF/CR normalized to LF. Unsupported
  MIME types and charsets are refused. The native driver limits transfer size;
  it refuses oversized or failed transfers instead of delivering partial data.

The fork baseline and backend limitations are documented in its `NATIVE_DROP.md`.
The fork changes Gio's Go import path: callers directly importing Gio types used
by gui's low-level drawing APIs must migrate those imports too. High-level tui/
widget users need no application-side replacement directive.

Synthetic drop tests cover URI decoding, quoting, atomic refusal and ordinary
PasteEvent delivery; headless tests do not stand in for native gestures. The native
checks were done by hand on 2026-10-16: Finder drops and Hangul on macOS, Nautilus drops
(after gio v0.1.3's fix for Hyprland's stale move action) and the Hangul L1–L9 matrix on
Wayland. X11 drops are covered by the fork's Xvfb XDND cells, not yet by a native drag.
`TestGioIsTheVersionTheIMEContractWasVerifiedAgainst` requires the Gio version those
checks verified (`verifiedGio`); moving Gio means repeating them.
