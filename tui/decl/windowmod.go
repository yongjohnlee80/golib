package decl

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"strconv"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// ErrWindowOperationUnavailable is a requested operation refused by state,
// configuration or input scope. Hosts can compare it through wrapped errors.
var ErrWindowOperationUnavailable = errors.New("tui/decl: window operation unavailable")

// WithWindowCollector supplies a caller-owned model; no registry or taskbar is created.
func WithWindowCollector(model widget.WindowCollector) Option {
	return func(a *Adapter) { a.windowCollector = model }
}

// WithWindowTargets injects placement/zoom policies by declared widget ID. The
// map is copied at construction; each target remains owned by its application.
func WithWindowTargets(targets map[string]any) Option {
	copy := maps.Clone(targets)
	return func(a *Adapter) { a.windowTargets = copy }
}

var windowModifierNames = enum[tui.Mods]{values: map[string]tui.Mods{
	"NoModifier": 0, "Shift": tui.ModShift, "Alt": tui.ModAlt, "Option": tui.ModAlt,
	"Control": tui.ModCtrl, "Super": tui.ModSuper, "Meta": tui.ModMeta,
}}
var windowMouseButtons = enum[tui.MouseButton]{values: map[string]tui.MouseButton{
	"LeftButton": tui.MouseLeft, "RightButton": tui.MouseRight, "MiddleButton": tui.MouseMiddle,
}}
var windowModifierFlags = flagSet{singleton: "WindowMod", values: map[string]int64{
	"NoModifier": 0, "Shift": int64(tui.ModShift), "Alt": int64(tui.ModAlt), "Option": int64(tui.ModAlt),
	"Control": int64(tui.ModCtrl), "Super": int64(tui.ModSuper), "Meta": int64(tui.ModMeta),
}}

func windowModifierOf(v qml.SpecValue) (tui.Mods, error) {
	if v.Kind == qml.SpecValueString {
		return windowModifierNames.read(v)
	}
	n, err := windowModifierFlags.read(v)
	return tui.Mods(n), err
}

type windowModConfig struct {
	move, resize, maximize, minimize, close bool
	label, key                              string
	modifier                                tui.Mods
	moveButton, resizeButton                tui.MouseButton
}

func readWindowMod(b Build) (windowModConfig, []string, error) {
	c := windowModConfig{modifier: tui.ModAlt, moveButton: tui.MouseLeft, resizeButton: tui.MouseRight}
	consumed, err := readProps(b.Props, map[string]field{
		"movable": into(&c.move, boolOf), "resizable": into(&c.resize, boolOf),
		"maximizable": into(&c.maximize, boolOf), "minimizable": into(&c.minimize, boolOf),
		"closable": into(&c.close, boolOf), "label": into(&c.label, stringOf), "key": into(&c.key, stringOf),
		"dragModifier": into(&c.modifier, windowModifierOf),
		"moveButton":   into(&c.moveButton, windowMouseButtons.read), "resizeButton": into(&c.resizeButton, windowMouseButtons.read),
	})
	if err == nil && c.minimize && b.WindowCollector == nil {
		err = errors.New("minimizable requires WithWindowCollector")
	}
	if err == nil && c.move && c.resize && c.moveButton == c.resizeButton {
		err = errors.New("move and resize bindings overlap")
	}
	return c, consumed, err
}

func buildWindowMod(b Build) (tui.Component, []string, error) {
	if len(b.Children) != 1 {
		return nil, nil, errors.New("WindowMod wraps exactly one content child")
	}
	c, consumed, err := readWindowMod(b)
	if err != nil {
		return nil, nil, err
	}
	emit := b.EmitterWith("changed")
	opts := []widget.WindowModOption{
		widget.WithWindowLabel(c.label), widget.WithWindowKey(c.key),
		widget.WithWindowMoveBinding(c.modifier, c.moveButton), widget.WithWindowResizeBinding(c.modifier, c.resizeButton),
		widget.WithWindowChanged(func(e widget.WindowChangedEvent) {
			emit(strValue(e.Operation.String()), strValue(e.Key), windowNumber(e.Bounds.X), windowNumber(e.Bounds.Y),
				windowNumber(e.Bounds.W), windowNumber(e.Bounds.H), windowBool(e.Maximized), windowBool(e.Minimized))
		}),
	}
	if c.move {
		opts = append(opts, widget.WithWindowMove())
	}
	if c.resize {
		opts = append(opts, widget.WithWindowResize())
	}
	if c.maximize {
		opts = append(opts, widget.WithWindowMaximize())
	}
	if c.minimize {
		opts = append(opts, widget.WithWindowMinimize(b.WindowCollector))
	}
	if c.close {
		opts = append(opts, widget.WithWindowClose())
	}
	if b.WindowTarget != nil {
		opts = append(opts, widget.WithWindowTarget(b.WindowTarget))
	}
	return widget.NewWindowMod(b.Children[0], opts...), consumed, nil
}

func windowNumber(n int) qml.SpecValue {
	return qml.SpecValue{Kind: qml.SpecValueNumber, Raw: strconv.Itoa(n)}
}
func windowBool(b bool) qml.SpecValue {
	return qml.SpecValue{Kind: qml.SpecValueBool, Raw: strconv.FormatBool(b)}
}

func windowInvoke(action tui.Action) Method {
	return NoArgMethod(func(w *widget.WindowMod) error {
		if !w.InvokeInput(action) {
			return ErrWindowOperationUnavailable
		}
		return nil
	})
}

func windowStep(move bool) Method {
	return func(component tui.Component, args []qml.SpecValue) error {
		w, ok := component.(*widget.WindowMod)
		if !ok || len(args) != 2 {
			return errors.New("window step requires a WindowMod and two integer deltas")
		}
		delta := [2]int{}
		for i, v := range args {
			n, err := numberOf(v)
			if err != nil {
				return err
			}
			if math.IsNaN(n) || math.IsInf(n, 0) || math.Trunc(n) != n || n < float64(math.MinInt) || n >= float64(math.MaxInt) {
				return errors.New("window delta must be a finite integer in range")
			}
			delta[i] = int(n)
		}
		var action tui.Action = widget.WindowResizeByAction{DW: delta[0], DH: delta[1]}
		if move {
			action = widget.WindowMoveByAction{DX: delta[0], DY: delta[1]}
		}
		if !w.InvokeInput(action) {
			return ErrWindowOperationUnavailable
		}
		return nil
	}
}

var windowModType = Type{
	Name: "WindowMod", Build: buildWindowMod,
	Ctor:    []string{"movable", "resizable", "maximizable", "minimizable", "closable", "label", "key", "dragModifier", "moveButton", "resizeButton"},
	Signals: map[string][]string{"changed": {"operation", "key", "x", "y", "width", "height", "maximized", "minimized"}},
	Methods: map[string]Method{
		"moveBy": windowStep(true), "resizeBy": windowStep(false),
		"toggleMaximize": windowInvoke(widget.WindowMaximizeAction{}), "minimize": windowInvoke(widget.WindowMinimizeAction{}),
		"restore": windowInvoke(widget.WindowRestoreAction{}), "close": windowInvoke(widget.WindowCloseAction{}),
	},
	Getters: map[string]Getter{
		"maximized": func(c tui.Component) (qml.SpecValue, error) {
			return windowBool(c.(*widget.WindowMod).Core().State().Maximized), nil
		},
		"minimized": func(c tui.Component) (qml.SpecValue, error) {
			return windowBool(c.(*widget.WindowMod).Core().State().Minimized), nil
		},
	},
}

func windowCoreOf(c tui.Component) (*widget.WindowCore, bool) {
	if w, ok := c.(*widget.WindowMod); ok {
		return w.Core(), true
	}
	if w, ok := c.(interface{ WindowBehavior() *widget.WindowCore }); ok {
		return w.WindowBehavior(), true
	}
	return nil, false
}

type windowButtonsNode struct {
	widget.Base
	resolve func(string) (*widget.WindowCore, bool)
	target  string
	labels  [3]string
	buttons *widget.WindowButtons
}

func (n *windowButtonsNode) Init(ctx *tui.Context) {
	n.Base.Init(ctx)
	var core *widget.WindowCore
	if n.target != "" {
		core, _ = n.resolve(n.target)
	} else {
		parent := ctx.Ancestor(func(c tui.Component) bool { _, ok := windowCoreOf(c); return ok })
		if parent != nil {
			core, _ = windowCoreOf(parent)
		}
	}
	if core == nil {
		panic(errs.Fatal{Op: "tui/decl: WindowButtons.Init", Rule: "no target WindowMod; nest the controls in it or name target"})
	}
	n.buttons = widget.NewWindowButtons(core, widget.WithWindowButtonLabels(n.labels[0], n.labels[1], n.labels[2]))
	ctx.Mount(n.buttons)
}
func (n *windowButtonsNode) Layout(c tui.Constraints) tui.Size {
	s := n.Context().LayoutChild(n.buttons, c)
	n.Context().PlaceChild(n.buttons, tui.Rect{W: s.W, H: s.H})
	return s
}
func (n *windowButtonsNode) Render(tui.Surface) {}

var windowButtonsType = Type{Name: "WindowButtons", Ctor: []string{"target", "closeText", "maximizeText", "minimizeText"},
	Build: func(b Build) (tui.Component, []string, error) {
		if len(b.Children) != 0 {
			return nil, nil, errors.New("WindowButtons supplies its own controls")
		}
		n := &windowButtonsNode{resolve: b.WindowCore, labels: [3]string{"x", "+", "_"}}
		consumed, err := readProps(b.Props, map[string]field{
			"target": into(&n.target, stringOf), "closeText": into(&n.labels[0], stringOf),
			"maximizeText": into(&n.labels[1], stringOf), "minimizeText": into(&n.labels[2], stringOf),
		})
		return n, consumed, err
	},
}

var windowTaskbarType = Type{Name: "WindowTaskbar", Build: func(b Build) (tui.Component, []string, error) {
	if len(b.Children) != 0 {
		return nil, nil, errors.New("WindowTaskbar supplies its own entry controls")
	}
	model, ok := b.WindowCollector.(*widget.MinimizedWindows)
	if !ok || model == nil {
		return nil, nil, fmt.Errorf("WindowTaskbar requires WithWindowCollector with a MinimizedWindows model")
	}
	return widget.NewWindowTaskbar(model), nil, nil
}}
