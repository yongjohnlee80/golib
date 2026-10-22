package decl

import (
	"errors"
	"fmt"
	"maps"
	"math"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// ErrWindowOperationUnavailable is a requested operation refused by state,
// configuration or input scope. Hosts can compare it through wrapped errors.
var ErrWindowOperationUnavailable = errors.New("tui/decl: window operation unavailable")

// WithWindowCollector supplies a caller-owned model; no registry or taskbar is created. A nil one,
// typed or not (tui.NilLike), is none.
func WithWindowCollector(model widget.WindowCollector) Option {
	if tui.NilLike(model) {
		model = nil
	}
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

// newWindowModConfig is a window declaration's defaults: Alt with the left button moves, Alt with
// the right resizes, as WindowCore's own.
func newWindowModConfig() windowModConfig {
	return windowModConfig{modifier: tui.ModAlt, moveButton: tui.MouseLeft, resizeButton: tui.MouseRight}
}

// windowModFields are the window properties a WindowMod and a Drawer share, read into c. Each adds
// the one that turns resizing on: a WindowMod's resizable, a Drawer's windowResize (its resizable is
// its grip).
func windowModFields(c *windowModConfig) map[string]field {
	return map[string]field{
		"movable":     into(&c.move, boolOf),
		"maximizable": into(&c.maximize, boolOf), "minimizable": into(&c.minimize, boolOf),
		"closable": into(&c.close, boolOf), "label": into(&c.label, stringOf), "key": into(&c.key, stringOf),
		"dragModifier": into(&c.modifier, windowModifierOf),
		"moveButton":   into(&c.moveButton, windowMouseButtons.read), "resizeButton": into(&c.resizeButton, windowMouseButtons.read),
	}
}

// options are the WindowMod options c declares: its label and key, its drag bindings, the
// operations it turns on (minimizing into collector), and changed. Closing is the caller's: a
// WindowMod's closes it, a Drawer's closes the Drawer.
func (c windowModConfig) options(collector widget.WindowCollector, changed func(widget.WindowChangedEvent)) []widget.WindowModOption {
	opts := []widget.WindowModOption{
		widget.WithWindowLabel(c.label), widget.WithWindowKey(c.key),
		widget.WithWindowMoveBinding(c.modifier, c.moveButton), widget.WithWindowResizeBinding(c.modifier, c.resizeButton),
		widget.WithWindowChanged(changed),
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
		opts = append(opts, widget.WithWindowMinimize(collector))
	}
	return opts
}

// windowChangedArgs are the changed signal's arguments for e, as its declaration names them:
// operation, key, x, y, width, height, maximized, minimized.
func windowChangedArgs(e widget.WindowChangedEvent) []qml.SpecValue {
	return []qml.SpecValue{strValue(e.Operation.String()), strValue(e.Key), numberValue(e.Bounds.X), numberValue(e.Bounds.Y),
		numberValue(e.Bounds.W), numberValue(e.Bounds.H), boolValue(e.Maximized), boolValue(e.Minimized)}
}

func readWindowMod(b Build) (windowModConfig, []string, error) {
	c := newWindowModConfig()
	fields := windowModFields(&c)
	fields["resizable"] = into(&c.resize, boolOf)
	consumed, err := b.ReadProps(fields)
	if err == nil && c.minimize && b.WindowCollector == nil {
		err = errors.New("minimizable requires WithWindowCollector")
	}
	if err == nil && c.move && c.resize && c.moveButton == c.resizeButton {
		err = errors.New("move and resize bindings overlap")
	}
	return c, consumed, err
}

func buildWindowMod(b Build) (tui.Component, []string, error) {
	c, consumed, err := readWindowMod(b)
	if err != nil {
		return nil, nil, err
	}
	emit := b.EmitterWith("changed")
	opts := c.options(b.WindowCollector, func(e widget.WindowChangedEvent) { emit(windowChangedArgs(e)...) })
	if c.close {
		opts = append(opts, widget.WithWindowClose())
	}
	if b.WindowTarget != nil {
		opts = append(opts, widget.WithWindowTarget(b.WindowTarget))
	}
	return widget.NewWindowMod(b.Children[0], opts...), consumed, nil
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
			if math.IsNaN(n) || math.IsInf(n, 0) || math.Trunc(n) != n || !inIntRange(n) {
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
	Name: "WindowMod", Children: Exactly(1).Because("its content"), Build: buildWindowMod,
	Ctor:    []string{"movable", "resizable", "maximizable", "minimizable", "closable", "label", "key", "dragModifier", "moveButton", "resizeButton"},
	Signals: map[string][]string{"changed": {"operation", "key", "x", "y", "width", "height", "maximized", "minimized"}},
	Methods: map[string]Method{
		"moveBy": windowStep(true), "resizeBy": windowStep(false),
		"toggleMaximize": windowInvoke(widget.WindowMaximizeAction{}), "minimize": windowInvoke(widget.WindowMinimizeAction{}),
		"restore": windowInvoke(widget.WindowRestoreAction{}), "close": windowInvoke(widget.WindowCloseAction{}),
	},
	Getters: map[string]Getter{
		"maximized": getter(func(w *widget.WindowMod) (qml.SpecValue, error) {
			return boolValue(w.Core().State().Maximized), nil
		}),
		"minimized": getter(func(w *widget.WindowMod) (qml.SpecValue, error) {
			return boolValue(w.Core().State().Minimized), nil
		}),
	},
}

// windowBehaver is a component that carries a window: a WindowMod, a Drawer with window
// controls, a tile, a style's own window widget.
type windowBehaver interface {
	WindowBehavior() *widget.WindowCore
}

func windowCoreOf(c tui.Component) (*widget.WindowCore, bool) {
	if w, ok := c.(windowBehaver); ok {
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
		consumed, err := b.ReadProps(map[string]field{
			"target": into(&n.target, stringOf), "closeText": into(&n.labels[0], stringOf),
			"maximizeText": into(&n.labels[1], stringOf), "minimizeText": into(&n.labels[2], stringOf),
		})
		return n, consumed, err
	},
}

var windowTaskbarType = Type{Name: "WindowTaskbar", Children: NoChildren().Because("it supplies its own entry controls"), Build: func(b Build) (tui.Component, []string, error) {
	// any collector that also presents its entries (widget.WindowEntries) will do: a
	// MinimizedWindows, or a consumer's own
	model, ok := b.WindowCollector.(widget.WindowEntries)
	if !ok || tui.NilLike(model) {
		return nil, nil, fmt.Errorf("WindowTaskbar requires WithWindowCollector with a collector it can present (widget.WindowEntries), as a MinimizedWindows is")
	}
	return widget.NewWindowTaskbar(model), nil, nil
}}
