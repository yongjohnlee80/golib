package vegalite

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// Subset is the tables Lay checks a spec against: each list is what the reader dispatches from,
// so every name on it lays out, in a small spec, and a neighbour on no list is ErrUnsupported.
// The lists are pinned here, and each pinned name is laid: a table entry dropped is both missing
// from Subset and refused by Lay, since the reader dispatches from the table.
func TestSubsetIsWhatLayDraws(t *testing.T) {
	s := Subset()
	wantMarks := []string{"area", "bar", "circle", "line", "point", "rule"}
	wantChannels := []string{"color", "fill", "opacity", "shape", "x", "y"}
	wantFieldTypes := []string{"nominal", "ordinal", "quantitative", "temporal"}
	wantScaleTypes := []string{"band", "linear", "ordinal", "point", "time", "utc"}
	wantAggregates := []string{"average", "count", "max", "mean", "median", "min", "sum"}
	for what, c := range map[string]struct{ got, want []string }{
		"marks":       {s.Marks, wantMarks},
		"channels":    {s.Channels, wantChannels},
		"field types": {s.FieldTypes, wantFieldTypes},
		"scale types": {s.ScaleTypes, wantScaleTypes},
		"aggregates":  {s.Aggregates, wantAggregates},
	} {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("Subset's %s: %v, want %v", what, c.got, c.want)
		}
	}
	if !s.Layers || s.MaxRows != maxRows || s.MaxBands != maxBands || s.MaxSeries != maxSeries {
		t.Errorf("Subset's layers and limits: %+v", s)
	}

	const data = `{"values":[{"k":"a","d":"2024-01-01","v":1},{"k":"b","d":"2024-01-02","v":2}]}`
	const x, y = `"x":{"field":"k","type":"nominal"}`, `"y":{"field":"v","type":"quantitative"}`
	spec := func(mark, enc, more string) string {
		return `{"data":` + data + `,"mark":` + mark + `,"encoding":{` + enc + `}` + more + `}`
	}
	draws := func(what, src string) {
		t.Helper()
		if _, err := lay(t, src, 400); err != nil {
			t.Errorf("%s: %v\n%s", what, err, src)
		}
	}
	refuses := func(what, src string) {
		t.Helper()
		if _, err := lay(t, src, 400); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%s: %v; want ErrUnsupported\n%s", what, err, src)
		}
	}

	for _, m := range wantMarks {
		if m == "rule" { // a rule is drawn over another mark, never alone
			draws("mark rule", `{"data":`+data+`,"layer":[{"mark":"bar","encoding":{`+x+","+y+`}},{"mark":"rule","encoding":{`+y+`}}]}`)
			continue
		}
		draws("mark "+m, spec(`"`+m+`"`, x+","+y, ""))
	}
	refuses("mark tick", spec(`"tick"`, x+","+y, ""))

	channels := map[string]string{
		"x": x + "," + y, "y": x + "," + y, "color": x + "," + y + `,"color":{"field":"k","type":"nominal"}`,
		"fill": x + "," + y + `,"fill":{"field":"k","type":"nominal"}`, "opacity": x + "," + y + `,"opacity":{"value":0.5}`,
	}
	for _, c := range wantChannels {
		if c == "shape" {
			draws("channel shape", spec(`"point"`, x+","+y+`,"shape":{"field":"k","type":"nominal"}`, ""))
			continue
		}
		draws("channel "+c, spec(`"bar"`, channels[c], ""))
	}
	refuses("channel size", spec(`"point"`, x+","+y+`,"size":{"field":"v","type":"quantitative"}`, ""))

	fields := map[string]string{"nominal": "k", "ordinal": "k", "quantitative": "v", "temporal": "d"}
	for _, ft := range wantFieldTypes {
		draws("field type "+ft, spec(`"point"`, `"x":{"field":"`+fields[ft]+`","type":"`+ft+`"},`+y, ""))
	}
	refuses("field type geojson", spec(`"point"`, `"x":{"field":"k","type":"geojson"},`+y, ""))

	scales := map[string]string{
		"linear":  `"point"|"x":{"field":"v","type":"quantitative","scale":{"type":"linear"}},` + y,
		"time":    `"point"|"x":{"field":"d","type":"temporal","scale":{"type":"time"}},` + y,
		"utc":     `"point"|"x":{"field":"d","type":"temporal","scale":{"type":"utc"}},` + y,
		"band":    `"bar"|"x":{"field":"k","type":"nominal","scale":{"type":"band"}},` + y,
		"point":   `"point"|"x":{"field":"k","type":"nominal","scale":{"type":"point"}},` + y,
		"ordinal": `"bar"|` + x + "," + y + `,"color":{"field":"k","type":"nominal","scale":{"type":"ordinal"}}`,
	}
	for _, st := range wantScaleTypes {
		mark, enc, _ := strings.Cut(scales[st], "|")
		draws("scale type "+st, spec(mark, enc, ""))
	}
	refuses("scale type log", spec(`"bar"`, x+`,"y":{"field":"v","type":"quantitative","scale":{"type":"log"}}`, ""))

	for _, a := range wantAggregates {
		draws("aggregate "+a, spec(`"bar"`, x+`,"y":{"field":"v","type":"quantitative","aggregate":"`+a+`"}`, ""))
	}
	refuses("aggregate variance", spec(`"bar"`, x+`,"y":{"field":"v","type":"quantitative","aggregate":"variance"}`, ""))

	// every passed key is read and passed over where it sits; a key on no list there is refused
	color := `,"color":{"field":"k","type":"nominal"`
	passed := map[string]func(k string) string{
		"top":      func(k string) string { return spec(`"bar"`, x+","+y, `,"`+k+`":null`) },
		"mark":     func(k string) string { return spec(`{"type":"bar","`+k+`":null}`, x+","+y, "") },
		"channel":  func(k string) string { return spec(`"bar"`, `"x":{"field":"k","type":"nominal","`+k+`":null},`+y, "") },
		"encoding": func(k string) string { return spec(`"bar"`, x+","+y+`,"`+k+`":null`, "") },
		"scale": func(k string) string {
			return spec(`"bar"`, x+`,"y":{"field":"v","type":"quantitative","scale":{"`+k+`":null}}`, "")
		},
		"axis": func(k string) string {
			return spec(`"bar"`, x+`,"y":{"field":"v","type":"quantitative","axis":{"`+k+`":null}}`, "")
		},
		"legend": func(k string) string { return spec(`"bar"`, x+","+y+color+`,"legend":{"`+k+`":null}}`, "") },
		"title":  func(k string) string { return spec(`"bar"`, x+","+y, `,"title":{"text":"T","`+k+`":"T"}`) },
		"config": func(k string) string { return spec(`"bar"`, x+","+y, `,"config":{"`+k+`":null}`) },
		"mark config": func(k string) string {
			return spec(`"bar"`, x+","+y, `,"config":{"mark":{"`+k+`":null}}`)
		},
		"select": func(k string) string {
			return spec(`"bar"`, x+","+y, `,"params":[{"name":"pick","select":{"type":"point","`+k+`":null}}]`)
		},
	}
	if len(s.Passed) != len(passed) {
		t.Errorf("Subset's passed keys sit in %d places, want %d: %v", len(s.Passed), len(passed), s.Passed)
	}
	for where, keys := range s.Passed {
		at := passed[where]
		if at == nil {
			t.Errorf("passed keys at %q, which this cell does not place", where)
			continue
		}
		for _, k := range keys {
			if (where == "title" && k == "text") || (where == "select" && k == "type") {
				continue // read, and set above
			}
			draws(where+" key "+k, at(k))
		}
		refuses(where+" key notAKey", at("notAKey"))
	}
}
