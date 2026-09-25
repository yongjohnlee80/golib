package decl

import (
	"fmt"
	"strconv"

	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
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

// ReadProps reads a builder's constructor properties by name and returns the
// ones it read — which is exactly what the builder must return as CONSUMED.
// Deriving one from the other is what stops a builder reading a property and
// forgetting to say so, which makes the engine apply it a second time.
func ReadProps(props []qml.SpecProp, fields map[string]Field) ([]string, error) {
	return readProps(props, fields)
}

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
//	tuidecl.WithTypes(tuidecl.Instance("Terminal", myTerminal))
//	Split { Editor { }  Terminal { } }                     // in QML
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
