module github.com/yongjohnlee80/golib/gui

go 1.25.3

require (
	gioui.org v0.10.3
	github.com/yongjohnlee80/golib v0.6.54
	golang.org/x/image v0.26.0
)

require (
	gioui.org/shader v1.0.9 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	golang.org/x/exp/shiny v0.0.0-20250408133849-7e4ce0ab07d0 // indirect
	golang.org/x/net v0.48.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.38.0 // indirect
)

// A nested module so a TUI-only golib user never resolves Gio or cgo (ADR 1791330692 §4.1).
// Like dao/bigquery, it builds against a RELEASED golib with no replace directive, so it compiles
// from exactly what a third party gets. For local work across both modules, use an uncommitted
// go.work in this directory (`go work init . ..`): a go.work here is invisible to the root module.
