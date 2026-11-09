package terminal

// tabX is where tab id starts on the strip, and addX where its + does: read from the strip's spans,
// whose first are the tabs, then +, while the tabs fit (the cells that use them have a few tabs).
func (t *TerminalTabs) tabX(id TerminalTabID) int {
	spans := t.strip.TabSpans()
	for i, tb := range t.strip.Tabs() {
		if tb.ID == string(id) && i < len(spans) {
			return spans[i].X
		}
	}
	return 0
}

func (t *TerminalTabs) addX() int {
	if spans, n := t.strip.TabSpans(), len(t.strip.Tabs()); n < len(spans) {
		return spans[n].X
	}
	return 0
}
