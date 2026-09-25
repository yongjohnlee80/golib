package decl

import (
	"fmt"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// StdRegistry returns a registry covering the standard widget vocabulary: a
// small, deliberately AWKWARD core, and the application types an editor-shaped
// program is made of.
//
// The core is chosen for the shapes it forces, not for coverage:
//
//   - Split takes its orientation and BOTH children as required constructor
//     arguments, and has no setter for either. Nothing can build it after the
//     fact.
//   - Button takes its activation callback only as a construction option; there
//     is no SetOnActivate.
//   - Flex takes its direction at construction and its children afterwards,
//     through Add.
//   - Text takes its string either way, so it is the one that proves a
//     property can flow through the runtime path instead.
//
// A registry that can build these four can build the easy cases. One that was
// designed against Text alone would have looked finished and collapsed on the
// first Split.
//
// Enum-valued properties are written as QUALIFIED ENUMS — `orientation:
// Tui.Horizontal` — exactly as Qt spells them (`Qt.Horizontal`). The engine
// resolves them to a terminal before a builder runs, and each builder still
// checks the KIND, because "the engine resolves it first" is a property of the
// current wiring rather than of a builder's signature: a host may inject a
// constant of any kind.
func StdRegistry() *Registry {
	r := NewRegistry()
	registerTypes(r, stdTypes())
	registerStdAttached(r)
	return r
}

// StdProperties returns the complete property contract for [StdRegistry]: the
// runtime setters AND the declarations of what each type takes only at
// construction — both derived from the same type table the registry is.
//
// Both halves are needed, and the second is the one that is easy to forget.
// Without it the adapter cannot tell `orientation` — which Split really does
// take at construction — from a misspelling, so a typo would demolish a working
// widget and blame the constructor for it.
func StdProperties() []Option { return typeOptions(stdTypes()) }

// stdTypes is every standard widget type, in one table.
func stdTypes() []Type { return append(coreTypes(), appTypes()...) }

// tuiEnums is every enum the standard vocabulary accepts. The Tui singleton's
// constants are derived from it, so a new enum is visible to documents by being
// listed here — and nowhere else.
var tuiEnums = []enumeration{
	orientations, directions, dockEdges, keysets, menuAligns, wrapModes, fileModes,
}

// tuiFlags is every flag set, each under its own singleton. Listed here and
// nowhere else: the constants AND the module's exports derive from it.
var tuiFlags = []flagSet{dialogButtons}

// ---------------------------------------------------------------- core

var orientations = enum[widget.Orientation]{values: map[string]widget.Orientation{
	"Horizontal": widget.Horizontal,
	"Vertical":   widget.Vertical,
}}

var directions = enum[tui.Direction]{values: map[string]tui.Direction{
	"Horizontal": tui.Horizontal,
	"Vertical":   tui.Vertical,
}}

func coreTypes() []Type {
	return []Type{
		{Name: "Split", Build: buildSplit, Ctor: []string{"orientation"}},
		{Name: "Flex", Build: buildFlex, Ctor: []string{"direction"}},
		{Name: "Button", Build: buildButton, Setters: map[string]Setter{
			"enabled": setter("a Button", boolOf, (*widget.Button).SetEnabled),
			"label":   setter("a Button", stringOf, (*widget.Button).SetLabel),
		}},
		{Name: "Text", Build: buildText, Ctor: []string{"wrapMode"}, restyle: restyleText, Setters: map[string]Setter{
			"text": setter("a Text", stringOf, (*widget.Text).SetText),
		}},
	}
}

// buildSplit is the shape that broke the first seam: orientation and both
// children are required arguments with no later path in.
//
// The orientation is REPORTED consumed: there is no SetOrientation, so an engine
// that re-applied it would find no setter and fail the mount.
func buildSplit(b Build) (tui.Component, []string, error) {
	if len(b.Children) != 2 {
		return nil, nil, fmt.Errorf("Split needs exactly 2 children, got %d (at %s)", len(b.Children), b.Pos)
	}
	o := widget.Horizontal
	consumed, err := readProps(b.Props, map[string]field{
		"orientation": into(&o, orientations.read),
	})
	if err != nil {
		return nil, nil, err
	}
	return widget.NewSplit(o, b.Children[0], b.Children[1]), consumed, nil
}

// buildFlex takes its direction at construction and its children afterwards.
func buildFlex(b Build) (tui.Component, []string, error) {
	dir := tui.Vertical
	consumed, err := readProps(b.Props, map[string]field{
		"direction": into(&dir, directions.read),
	})
	if err != nil {
		return nil, nil, err
	}
	f := tui.NewFlex(dir)
	for _, c := range b.Children {
		f.Add(c)
	}
	return f, consumed, nil
}

// buildButton wires its activation at construction, which is the only chance
// the widget gives: there is no SetOnActivate.
//
// It builds with an EMPTY label and consumes nothing. Button has a SetLabel, so
// the label travels the runtime path and exercises the half of the seam that
// Split cannot.
//
// The empty label is the point, not laziness. An earlier version read the label
// here AND reported nothing consumed, so the engine applied it again: the value
// was set twice, and SetLabel's equality guard hid that for a single
// declaration. Two declarations of the same property exposed it — construction
// took the LAST while the replay ran in DOCUMENT order — which is the general
// shape of consuming a property without saying so.
func buildButton(b Build) (tui.Component, []string, error) {
	return widget.NewButton("", widget.WithOnActivate(b.Emitter("clicked"))), nil, nil
}

// wrapModes are Qt's Text.wrapMode values this toolkit has.
//
// NoWrap is golib's Truncate: one line, newlines flattened, an ellipsis at the
// edge. WordWrap keeps the author's newlines and soft-wraps at the width it is
// given — what any multi-line message, a dialog's say, has to be.
var wrapModes = enum[widget.WrapMode]{values: map[string]widget.WrapMode{
	"NoWrap":   widget.Truncate,
	"WordWrap": widget.Wrap,
}}

// buildText consumes its wrap mode and its colours, which golib takes at
// construction; the text itself can be set at runtime.
//
// A Text paints its own background. golib has no transparent cell — a style
// that names no background paints the terminal's — so text on a coloured card
// is given that card's colours, or it sits in a strip of the terminal's.
func buildText(b Build) (tui.Component, []string, error) {
	mode := widget.Truncate
	consumed, err := readProps(b.Props, map[string]field{"wrapMode": into(&mode, wrapModes.read)})
	if err != nil {
		return nil, nil, err
	}
	return widget.NewText("", widget.WithWrapMode(mode)), consumed, nil
}
