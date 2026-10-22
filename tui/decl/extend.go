package decl

import (
	"fmt"
	"strconv"

	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// EXTENDING THE VOCABULARY — a consumer's own Go widgets, usable from QML.
//
//	gauge := tuidecl.Type{
//	    Name:  "Gauge",
//	    Build: func(b tuidecl.Build) (tui.Component, []string, error) {
//	        return mywidgets.NewGauge(), nil, nil
//	    },
//	    Setters: map[string]tuidecl.Setter{
//	        "value": tuidecl.NumberSetter((*mywidgets.Gauge).SetValue),
//	        "label": tuidecl.StringSetter((*mywidgets.Gauge).SetLabel),
//	    },
//	    Methods: map[string]tuidecl.Method{
//	        "reset": tuidecl.NoArgMethod((*mywidgets.Gauge).Reset),
//	    },
//	}
//	adapter := tuidecl.New(tuidecl.StdRegistry(), append(tuidecl.StdProperties(),
//	    tuidecl.WithTypes(gauge), tuidecl.WithErrorSink(sink))...)
//
//	Gauge { id: cpu; value: App.cpu; label: "CPU" }       // in QML
//	MenuItem { text: "&Reset"; onTriggered: cpu.reset() }
//
// A [Type] is the WHOLE contract of a widget in one value — the same value the
// standard vocabulary is a table of — so a custom widget binds to sources,
// reloads, is called by id and raises signals exactly as a built-in one does.
// The helpers below read values the way the built-ins read them, so "value
// must be a number" is said the same way for a Gauge as for anything else.

// WithTypes adds widget types to the adapter's vocabulary: each one's builder
// to the registry, and its properties, methods and signal parameters to the
// adapter. A name the registry already has panics, as [Register] does — a
// vocabulary is code, and two types of one name is a programming mistake.
func WithTypes(types ...Type) Option {
	return func(a *Adapter) {
		registerTypes(a.reg, types)
		for _, o := range typeOptions(types) {
			o(a)
		}
	}
}

// ---------------------------------------------------------------- setters

// StringSetter is a runtime property holding a string.
func StringSetter[W any](apply func(W, string)) Setter {
	return setter(widgetName[W](), stringOf, apply)
}

// BoolSetter is a runtime property holding a bool.
func BoolSetter[W any](apply func(W, bool)) Setter {
	return setter(widgetName[W](), boolOf, apply)
}

// NumberSetter is a runtime property holding a number.
func NumberSetter[W any](apply func(W, float64)) Setter {
	return setter(widgetName[W](), numberOf, apply)
}

// ColorSetter is a runtime property holding a colour, written as a theme
// writes one: an ANSI slot name, "#rrggbb" or "default".
func ColorSetter[W any](apply func(W, style.Color)) Setter {
	return setter(widgetName[W](), colorOf, apply)
}

// ModelField reads a model property into dst: `model: App.connections`, a host's [ItemModel].
func ModelField(dst *ItemModel) Field { return into(dst, modelOf) }

// ModelSetter is a runtime property holding a model, a host's [ItemModel].
func ModelSetter[W any](apply func(W, ItemModel)) Setter {
	return setter(widgetName[W](), modelOf, apply)
}

// HostObject marks a Go value a host hands a document as an object, beyond the kinds decl knows
// (a model, a message, a row's Index, a FileSource, Keys): [Value] passes one through as an
// object, and the type that reads it takes it back from [qml.SpecValue.Obj]. A consumer's own
// host object implements it rather than waiting for decl to list its type.
type HostObject interface {
	// HostObject is the marker; it does nothing.
	HostObject()
}

// StringGetter is a readable property holding a string.
func StringGetter[W any](read func(W) string) Getter {
	return getter(func(w W) (qml.SpecValue, error) { return strValue(read(w)), nil })
}

// Destroyer is a Type's Destroyed hook for a W: release runs with the widget when its node is
// destroyed. A widget that is not a W (a style's Build made another) holds nothing of a W's, so
// nothing is released, rather than a panic on a copied Type.
func Destroyer[W any](release func(W)) func(tui.Component) { return destroyer(release) }

// Restyler is a Type's Restyle hook for a W: wear runs with the widget and its palette. A widget
// that is not a W is left as it is, rather than a panic on a copied Type whose Build makes another.
func Restyler[W any](wear func(W, Palette)) func(tui.Component, Palette) {
	return func(c tui.Component, p Palette) {
		if w, ok := any(c).(W); ok {
			wear(w, p)
		}
	}
}

// NoArgMethod is a method a handler calls with no arguments: `gauge.reset()`.
func NoArgMethod[W any](run func(W) error) Method {
	return method(widgetName[W](), run)
}

// widgetName names a widget type for a diagnostic: "not a *mywidgets.Gauge".
func widgetName[W any]() string {
	var zero W
	return fmt.Sprintf("a %T", zero)
}

// ---------------------------------------------------------------- builders

// StringField reads a string property into dst.
func StringField(dst *string) Field { return into(dst, stringOf) }

// BoolField reads a bool property into dst.
func BoolField(dst *bool) Field { return into(dst, boolOf) }

// NumberField reads a number property into dst.
func NumberField(dst *float64) Field { return into(dst, numberOf) }

// ColorField reads a colour property into dst.
func ColorField(dst *style.Color) Field { return into(dst, colorOf) }

func numberOf(v qml.SpecValue) (float64, error) {
	if v.Kind != qml.SpecValueNumber {
		return 0, fmt.Errorf("want a number, got %s (at %s)", v.Kind, v.Pos)
	}
	n, err := strconv.ParseFloat(v.Raw, 64)
	if err != nil {
		if i, ierr := strconv.ParseInt(v.Raw, 0, 64); ierr == nil {
			return float64(i), nil
		}
		return 0, fmt.Errorf("%q is not a number (at %s)", v.Raw, v.Pos)
	}
	return n, nil
}

// ---------------------------------------------------------------- instances

// Instance is a type whose one widget the HOST already built: a document
// places it by name, and gets that widget rather than a new one.
//
//	tuidecl.WithTypes(tuidecl.Instance("Preview", myPreview))
//	Split { Editor { }  Preview { } }                      // in QML
//
// For a widget whose construction the host owns — one wired to a process, a
// socket, a model already running. A widget can be in the tree once, so a
// second placement while the first node exists is refused, by name and
// position.
//
// PLACE IT WHERE ITS NODE IS STABLE. A reload that keeps the node keeps the
// widget. One that would give it a NEW node — moving it to another parent,
// say — builds the replacement before releasing the old one, as every rebuild
// does, and is refused rather than letting the widget be in two places.
//
// An Instance takes no properties of its own: whatever it needs, the host gave
// it before handing it over. Give it setters with a full [Type] instead.
func Instance(name string, c tui.Component) Type {
	// placed is whether the widget has been handed to a node that still
	// exists. Handed out at Build and taken back when that node is Destroyed —
	// not read off the widget's mount state, which is false for BOTH of two
	// placements in one document, since a mount builds every node before it
	// mounts any.
	placed := false
	return Type{
		Name: name,
		Build: func(b Build) (tui.Component, []string, error) {
			if placed {
				return nil, nil, fmt.Errorf("%s is the host's one %s, and it is already placed; "+
					"a widget can be in the tree once (at %s)", name, name, b.Pos)
			}
			placed = true
			return c, nil, nil
		},
		Destroyed: func(tui.Component) { placed = false },
	}
}

// ---------------------------------------------------------------- enums

// Enum is a closed set of names a property takes, written as Qt writes an
// enum: the type that defines it, a dot, the value — `TextInput.Password`.
//
// A [Type] lists the enums its properties take in [Type.Enums]. Each Scope
// becomes a singleton of the tui module and each value a constant under it,
// so `echoMode: TextInput.Password` resolves before the builder runs, and a
// misspelt value is refused by name. Read one with [EnumField] or [EnumSetter],
// which hand the builder the value's name: "Password".
type Enum struct {
	// Scope is the name before the dot: the type that defines the enum in Qt.
	Scope string
	// Values are the names after it.
	Values []string
}

// read takes a resolved constant back to the value's name, refusing anything
// that is not one of this enum's. A builder receives a constant's VALUE, not
// where it was written, so a string spelling the qualified name —
// `"TextInput.Password"` — is accepted as the constant would be; an
// unqualified one, `"Password"`, is not.
func (e Enum) read(v qml.SpecValue) (string, error) { return e.enum().read(v) }

// enum is the Enum on the one enum engine: qualified, so `Scope.Value` resolves to the qualified
// name and an unqualified string ("Password") is not one of its values; each value reads as its
// name.
func (e Enum) enum() enum[string] {
	values := make(map[string]string, len(e.Values))
	for _, n := range e.Values {
		values[n] = n
	}
	return enum[string]{values: values, scope: e.Scope, qualified: true, order: e.Values}
}

// EnumField reads an enum property into dst: the value's name.
func EnumField(dst *string, e Enum) Field { return into(dst, e.read) }

// EnumSetter is a runtime property holding one of an enum's values.
func EnumSetter[W any](e Enum, apply func(W, string)) Setter {
	return setter(widgetName[W](), e.read, apply)
}

// ---------------------------------------------------------------- overlays

// Overlaid is a component that opens OVER the screen rather than taking a
// place in it — Qt's Popup, and every type built on one: a Dialog, a
// FileDialog, a command prompt. A Window does not lay it out; when it arranges
// its children it hands each one its overlay and what to run once it has
// closed (the keyboard back to the document's `focus: true` node). Outside a
// Window the builder has [Build.Overlay] — the adapter's WithOverlay host — to
// start from.
type Overlaid interface {
	tui.Component
	SetOverlay(host *widget.OverlayHost, afterClose func())
}
