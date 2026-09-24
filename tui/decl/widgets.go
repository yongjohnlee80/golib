package decl

import (
	"fmt"

	"github.com/yongjohnlee80/golib/parse"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// StdRegistry returns a registry covering a small, deliberately AWKWARD set of
// the standard widgets.
//
// The set is chosen for the shapes it forces, not for coverage:
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
func StdRegistry() *Registry {
	r := NewRegistry()
	Register(r, "Split", buildSplit)
	Register(r, "Flex", buildFlex)
	Register(r, "Button", buildButton)
	Register(r, "Text", buildText)
	return r
}

// StdProperties returns the complete property contract for [StdRegistry]: the
// runtime setters AND the declarations of what each type takes only at
// construction.
//
// Both halves are needed, and the second is the one that is easy to forget.
// Without it the adapter cannot tell `orientation` — which Split really does
// take at construction — from a misspelling, so a typo would demolish a working
// widget and blame the constructor for it.
func StdProperties() []Option {
	return []Option{
		// Split takes its orientation as a constructor argument and has no
		// SetOrientation; Flex takes its direction the same way.
		WithConstructorProps("Split", "orientation"),
		WithConstructorProps("Flex", "direction"),
		WithSetters("Text", map[string]Setter{
			"text": func(c tui.Component, v parse.SpecValue) error {
				t, ok := c.(*widget.Text)
				if !ok {
					return fmt.Errorf("not a Text")
				}
				s, err := stringOf(v)
				if err != nil {
					return err
				}
				t.SetText(s)
				return nil
			},
		}),
		WithSetters("Button", map[string]Setter{
			"enabled": func(c tui.Component, v parse.SpecValue) error {
				b, ok := c.(*widget.Button)
				if !ok {
					return fmt.Errorf("not a Button")
				}
				on, err := boolOf(v)
				if err != nil {
					return err
				}
				b.SetEnabled(on)
				return nil
			},
			"label": func(c tui.Component, v parse.SpecValue) error {
				b, ok := c.(*widget.Button)
				if !ok {
					return fmt.Errorf("not a Button")
				}
				s, err := stringOf(v)
				if err != nil {
					return err
				}
				b.SetLabel(s)
				return nil
			},
		}),
	}
}

// buildSplit is the shape that broke the first seam: orientation and both
// children are required arguments with no later path in.
func buildSplit(b Build) (tui.Component, []string, error) {
	if len(b.Children) != 2 {
		return nil, nil, fmt.Errorf("Split needs exactly 2 children, got %d (at %s)", len(b.Children), b.Pos)
	}
	o := widget.Horizontal
	consumed := []string{}
	for _, p := range b.Props {
		if p.Name != "orientation" {
			continue
		}
		switch p.Value.Raw {
		case "horizontal":
			o = widget.Horizontal
		case "vertical":
			o = widget.Vertical
		default:
			return nil, nil, fmt.Errorf("orientation must be horizontal or vertical, got %q (at %s)",
				p.Value.Raw, p.Value.Pos)
		}
		// Consumed, and it MUST be reported: there is no SetOrientation, so an
		// engine that re-applied this would find no setter and fail the mount.
		consumed = append(consumed, "orientation")
	}
	return widget.NewSplit(o, b.Children[0], b.Children[1]), consumed, nil
}

// buildFlex takes its direction at construction and its children afterwards.
func buildFlex(b Build) (tui.Component, []string, error) {
	dir := tui.Vertical
	consumed := []string{}
	for _, p := range b.Props {
		if p.Name != "direction" {
			continue
		}
		switch p.Value.Raw {
		case "horizontal":
			dir = tui.Horizontal
		case "vertical":
			dir = tui.Vertical
		default:
			return nil, nil, fmt.Errorf("direction must be horizontal or vertical, got %q (at %s)",
				p.Value.Raw, p.Value.Pos)
		}
		consumed = append(consumed, "direction")
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

// buildText consumes nothing: every property it has can be set at runtime.
func buildText(b Build) (tui.Component, []string, error) {
	return widget.NewText(""), nil, nil
}

func stringOf(v parse.SpecValue) (string, error) {
	if v.Kind != parse.SpecValueString {
		return "", fmt.Errorf("want a string, got %s (at %s)", v.Kind, v.Pos)
	}
	return v.Raw, nil
}

func boolOf(v parse.SpecValue) (bool, error) {
	if v.Kind != parse.SpecValueBool {
		return false, fmt.Errorf("want a bool, got %s (at %s)", v.Kind, v.Pos)
	}
	return v.Raw == "true", nil
}
