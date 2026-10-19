package decl

import (
	"errors"

	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// RAISABLE — golib's: a part of the layout drawn as a layer of its own, so it rises above the
// panels (the Drawers) when it takes the keyboard, as a panel does, and sinks beneath one that takes
// it next (widget.Raisable). It holds one child, laid out in its place.
//
//	Raisable {
//	    maximumWidth: App.pageWidth // at most this many columns, centred, as a Frame's
//	    Frame { title: "the page"; Editor { … } }
//	}
//
// The margins either side of a capped Raisable are no part of its layer: a panel there is drawn
// and clicked as if it were not.

func buildRaisable(b Build) (tui.Component, []string, error) {
	if len(b.Children) != 1 {
		return nil, nil, errors.New("a Raisable holds exactly one child, laid out in its place")
	}
	return widget.NewRaisable(b.Children[0]), nil, nil
}

// raisableType is the Raisable's entry in the standard vocabulary.
var raisableType = Type{
	Name:  "Raisable",
	Build: buildRaisable,
	Setters: map[string]Setter{
		// as a Frame's: at most this many columns, centred
		"maximumWidth": setter("a Raisable", func(v qml.SpecValue) (int, error) {
			n, err := numberOf(v)
			return int(n), err
		}, (*widget.Raisable).SetMaximumWidth),
	},
}
