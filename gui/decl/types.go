package decl

import (
	"fmt"
	"image/color"

	"github.com/yongjohnlee80/golib/gui/internal/csscolor"
	"github.com/yongjohnlee80/golib/gui/widget"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
)

// Types are golib/gui's own widgets as tui/decl types: TitleBar and Shape. Add them to a program
// with tuidecl.Types(decl.Types()...). The same document runs on a terminal, where each widget takes
// its fallback, and in a window.
//
//	TitleBar { title: "AutoDoc"; Text { text: "synced" } }   // trailing children at its right
//	Shape { shape: Shape.Ellipse; fillColor: "#4080c0"; width: 8; height: 4 }
//
// tuiFallback chooses what a terminal shows of a widget that has a mock: Gui.Mock (the default)
// or Gui.Notice.
func Types() []tuidecl.Type { return []tuidecl.Type{titleBarType(), shapeType()} }

// fallbackEnum is tuiFallback's values.
var fallbackEnum = tuidecl.Enum{Scope: "Gui", Values: []string{"Mock", "Notice"}}

// shapeEnum is a Shape's kind.
var shapeEnum = tuidecl.Enum{Scope: "Shape", Values: []string{"Rectangle", "Ellipse"}}

// fallbackSetter is tuiFallback on any widget with gui.Base's SetNotice.
func fallbackSetter[W interface{ SetNotice(bool) }]() tuidecl.Setter {
	return tuidecl.EnumSetter(fallbackEnum, func(w W, v string) { w.SetNotice(v == "Notice") })
}

func titleBarType() tuidecl.Type {
	return tuidecl.Type{
		Name: "TitleBar",
		Build: func(b tuidecl.Build) (tui.Component, []string, error) {
			var title string
			used, err := b.ReadProps(map[string]tuidecl.Field{"title": tuidecl.StringField(&title)})
			if err != nil {
				return nil, nil, err
			}
			return widget.NewTitleBar(title, b.Children...), used, nil
		},
		Setters: map[string]tuidecl.Setter{
			"title":       tuidecl.StringSetter(func(t *widget.TitleBar, s string) { t.SetTitle(s) }),
			"tuiFallback": fallbackSetter[*widget.TitleBar](),
		},
		Getters: map[string]tuidecl.Getter{
			"title": tuidecl.StringGetter(func(t *widget.TitleBar) string { return t.Title() }),
		},
		Enums: []tuidecl.Enum{fallbackEnum}, // Gui.Mock and Gui.Notice, for every gui type: one scope, published once
	}
}

func shapeType() tuidecl.Type {
	return tuidecl.Type{
		Name:     "Shape",
		Children: tuidecl.NoChildren(),
		Ctor:     []string{"shape", "strokeColor", "strokeWidth", "width", "height"},
		Build: func(b tuidecl.Build) (tui.Component, []string, error) {
			kind := "Rectangle"
			var fill, stroke string
			var strokeWidth, radius, w, h float64
			used, err := b.ReadProps(map[string]tuidecl.Field{
				"shape":       tuidecl.EnumField(&kind, shapeEnum),
				"fillColor":   tuidecl.StringField(&fill),
				"strokeColor": tuidecl.StringField(&stroke),
				"strokeWidth": tuidecl.NumberField(&strokeWidth),
				"radius":      tuidecl.NumberField(&radius),
				"width":       tuidecl.NumberField(&w),
				"height":      tuidecl.NumberField(&h),
			})
			if err != nil {
				return nil, nil, err
			}
			opts := []widget.ShapeOption{widget.WithRadius(float32(radius)), widget.WithShapeSize(int(w), int(h))}
			if fill != "" {
				c, err := cssColor(fill, "fillColor", b)
				if err != nil {
					return nil, nil, err
				}
				opts = append(opts, widget.WithFillColor(c))
			}
			if stroke != "" {
				c, err := cssColor(stroke, "strokeColor", b)
				if err != nil {
					return nil, nil, err
				}
				opts = append(opts, widget.WithStroke(c, float32(max(strokeWidth, 1))))
			}
			k := widget.ShapeRect
			if kind == "Ellipse" {
				k = widget.ShapeEllipse
			}
			return widget.NewShape(k, opts...), used, nil
		},
		Setters: map[string]tuidecl.Setter{
			"fillColor": tuidecl.StringSetter(func(s *widget.Shape, v string) {
				if c, ok := csscolor.Parse(v, color.NRGBA{}); ok {
					s.SetFillColor(c)
				}
			}),
			"radius":      tuidecl.NumberSetter(func(s *widget.Shape, r float64) { s.SetRadius(float32(r)) }),
			"tuiFallback": fallbackSetter[*widget.Shape](),
		},
		Enums: []tuidecl.Enum{shapeEnum}, // Gui (tuiFallback's) is published once, by TitleBar's Type
	}
}

// cssColor reads a colour property as CSS writes one ("#4080c0", "steelblue", "rgb(…)").
func cssColor(v, prop string, b tuidecl.Build) (color.NRGBA, error) {
	c, ok := csscolor.Parse(v, color.NRGBA{})
	if !ok {
		return color.NRGBA{}, fmt.Errorf("%s: %s %q is not a colour (at %s)", b.Type, prop, v, b.Pos)
	}
	return c, nil
}
