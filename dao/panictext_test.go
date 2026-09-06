package dao

// panicText renders a recovered panic value the way a person reads it,
// accepting either shape.
//
// The panics in this package used to carry strings and now carry errs.Fatal
// values, so a caller can errors.As the operation and the broken rule back out.
// The conversion was built to be MESSAGE-PRESERVING — errs.Fatal renders
// "Op: Rule", and each site's Op and Rule were split out of its existing
// sentence at a ": " boundary — so every assertion below reads the same text it
// read before.
//
// That makes these tests the evidence for the preservation claim rather than a
// casualty of the change: if a split had been made at the wrong place, or a
// message rebuilt rather than divided, the substring assertions would fail.
func panicText(rec any) string {
	if err, ok := rec.(error); ok {
		return err.Error()
	}
	s, _ := rec.(string)
	return s
}
