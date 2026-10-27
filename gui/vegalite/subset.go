package vegalite

import (
	"maps"
	"slices"
)

// SubsetInfo is the subset Lay draws, read from the tables its reader checks a spec against, so
// it says what Lay does. A key on none of its lists is ErrUnsupported: a transform, a log scale, a
// binned or time-unit field, another channel (see the package documentation).
type SubsetInfo struct {
	// Marks are the mark types drawn ("circle" is a filled point).
	Marks []string
	// Channels are the encoding channels drawn (color and fill are one channel by two names).
	Channels []string
	// FieldTypes are a field's measurement types.
	FieldTypes []string
	// ScaleTypes are the scale types drawn.
	ScaleTypes []string
	// Aggregates are the aggregate operations ("average" is mean's alias).
	Aggregates []string
	// Layers: a spec's layer, each a mark of its own over shared scales, is drawn.
	Layers bool
	// MaxRows, MaxBands and MaxSeries are the limits: the rows of data, a discrete axis's
	// categories, a colour's series. A spec past one is ErrTooLarge.
	MaxRows, MaxBands, MaxSeries int
	// Passed are the keys read and passed over, which change only how a chart looks or behaves in a
	// browser, by where they sit: "top", "mark", "channel", "encoding", "scale", "axis", "legend",
	// "title", "config", "mark config" and "select" (a selection param's).
	Passed map[string][]string
}

// Subset is the subset Lay draws, each list sorted.
func Subset() SubsetInfo {
	sorted := func(m map[string]bool) []string { return slices.Sorted(maps.Keys(m)) }
	return SubsetInfo{
		Marks:      slices.Sorted(maps.Keys(markTypes)),
		Channels:   slices.Sorted(maps.Keys(channels)),
		FieldTypes: slices.Sorted(slices.Values(fieldTypeNames[:])),
		ScaleTypes: sorted(scaleTypes),
		Aggregates: slices.Sorted(maps.Keys(aggregates)),
		Layers:     true, // parse reads a top-level layer
		MaxRows:    maxRows, MaxBands: maxBands, MaxSeries: maxSeries,
		Passed: map[string][]string{
			"top": sorted(passedTop), "mark": sorted(passedMark), "channel": sorted(passedChannel),
			"encoding": sorted(passedEncoding), "scale": sorted(passedScale), "axis": sorted(passedAxis),
			"legend": sorted(passedLegend), "title": sorted(passedTitle), "config": sorted(passedConfig),
			"mark config": sorted(passedMarkConfig), "select": sorted(passedSelect),
		},
	}
}
