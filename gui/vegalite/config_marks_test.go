package vegalite

import "testing"

// The config has a section for every mark a chart draws, rule included: a spec configuring its
// rules is laid out, as one configuring its bars is. Before, config.rule was refused as unsupported,
// so the whole chart fell back to its source.
func TestTheConfigHasASectionForEveryMark(t *testing.T) {
	// TestRulesByFieldAndByDatum's points and rules: a chart of rules alone is not drawn
	const layers = `"data":{"values":[{"x":1,"y":1,"at":2},{"x":3,"y":2,"at":2},{"x":5,"y":3,"at":4}]},
		"layer":[{"mark":"point","encoding":{"x":{"field":"x","type":"quantitative"},"y":{"field":"y","type":"quantitative"}}},
		{"mark":"rule","encoding":{"x":{"field":"at","type":"quantitative"}}}]`
	for _, mark := range []string{"point", "rule"} {
		if _, err := lay(t, `{`+layers+`,"config":{"`+mark+`":{"color":"red"}}}`, 300); err != nil {
			t.Errorf("config.%s: %v", mark, err)
		}
	}
}
