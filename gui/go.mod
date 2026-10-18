module github.com/yongjohnlee80/golib/gui

go 1.25.3

toolchain go1.26.9

require (
	github.com/yongjohnlee80/golib v0.7.6
	golang.org/x/image v0.45.0
)

require (
	github.com/go-text/typesetting v0.3.5
	github.com/yongjohnlee80/gio v0.1.3
)

require (
	gioui.org/shader v1.0.9 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	golang.org/x/exp/shiny v0.0.0-20250408133849-7e4ce0ab07d0 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
)

// A nested module so a TUI-only golib user never resolves Gio or cgo (ADR 1791330692 §4.1).
// Like dao/bigquery, it builds against a RELEASED golib with no replace directive, so it compiles
// from exactly what a third party gets. For local work across both modules, use an uncommitted
// go.work in this directory (`go work init . ..`): a go.work here is invisible to the root module.
