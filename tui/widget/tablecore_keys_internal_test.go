package widget

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// oldTableKey is TableCore.HandleKey at golib 6a0b271, verbatim: the oracle of the no-map
// differential. The table's keys are a grammar with state, so the differential compares effects, not
// names: two cores in one starting state, each given the same key, one by this function and one by
// the converted HandleKey.
func oldTableKey(c *TableCore, k tui.KeyEvent) bool {
	if k.Kind == tui.KeyRelease {
		return false
	}
	if mods := k.Mods & nonTextMods; mods != 0 {
		c.dropPending()
		if mods == tui.ModCtrl && (k.Code == 'v' || k.Code == 'V') {
			c.toggleSelect(TableSelectBlock)
			return true
		}
		return false
	}
	code := k.Code
	if k.Text != "" {
		code = []rune(k.Text)[0]
	}
	if code >= '1' && code <= '9' || code == '0' && c.count > 0 {
		c.pendingG, c.yanked = false, false
		c.count = min(c.count*10+int(code-'0'), tableCountMax)
		return true
	}
	if code == 'g' && !c.pendingG {
		c.pendingG, c.yanked = true, false // the count waits with it, for [count]gg
		return true
	}
	n, counted := max(c.count, 1), c.count > 0
	wasG, yanked := c.pendingG, c.yanked
	c.dropPending()
	if wasG && code != 'g' {
		n, counted = 1, false // the count went with the g it was typed for
	}
	switch code {
	case 'g': // the second g
		row := 0
		if counted {
			row = n - 1
		}
		c.SetCursor(TablePos{row, c.cur.Col})
	case 'k', tui.KeyUp:
		c.Move(-n, 0)
	case 'j', tui.KeyDown:
		c.Move(n, 0)
	case 'h', tui.KeyLeft:
		c.Move(0, -n)
	case 'l', tui.KeyRight:
		c.Move(0, n)
	case 'w':
		c.moveFields(n)
	case 'b':
		c.moveFields(-n)
	case tui.KeyPageUp:
		c.Move(-c.page(), 0)
	case tui.KeyPageDown:
		c.Move(c.page(), 0)
	case 'G':
		row := c.Len() - 1
		if counted {
			row = n - 1
		}
		c.SetCursor(TablePos{row, c.cur.Col})
	case '0', tui.KeyHome:
		c.SetCursor(TablePos{c.cur.Row, 0})
	case '$', tui.KeyEnd:
		c.SetCursor(TablePos{c.cur.Row, len(c.Fields()) - 1})
	case 'v':
		c.toggleSelect(TableSelectCells)
	case 'V':
		c.toggleSelect(TableSelectRows)
	case tui.KeyEscape:
		if c.anchor == nil {
			return wasG || counted // a pending command dropped is the key's work; otherwise it is the application's
		}
		c.SetAnchor(nil)
	case 'y':
		switch {
		case c.anchor != nil:
			c.Copy()
		case yanked:
			var b strings.Builder
			c.writeRow(&b, c.cur.Row, 0, len(c.Fields())-1)
			c.copyString(b.String())
		default:
			c.Copy()
			c.yanked = true
		}
	case 's':
		if !c.sortable {
			return false
		}
		c.sortKey(c.cur.Col)
	case tui.KeyEnter:
		if c.onActivate == nil || c.Len() == 0 {
			return false
		}
		c.onActivate(c.SourceRow(c.cur.Row))
	default:
		return false
	}
	return true
}

// tableEffect is everything a key can change on a core, as a comparable string.
func tableEffect(c *TableCore, ret bool, copies, acts []string) string {
	anchor := "none"
	if c.anchor != nil {
		anchor = fmt.Sprint(*c.anchor)
	}
	keys, _ := c.SortedKeys()
	return fmt.Sprintf("ret=%v cur=%v anchor=%s kind=%v count=%d g=%v yanked=%v sort=%v copies=%v acts=%v",
		ret, c.cur, anchor, c.kind, c.count, c.pendingG, c.yanked, keys, copies, acts)
}

// tableStates are the starting states every grid key is pressed in.
var tableStates = []struct {
	name    string
	prepare func(c *TableCore)
}{
	{"fresh", func(*TableCore) {}},
	{"a count", func(c *TableCore) { c.count = 2 }},
	{"a g", func(c *TableCore) { c.pendingG = true }},
	{"a counted g", func(c *TableCore) { c.count, c.pendingG = 2, true }},
	{"a selection", func(c *TableCore) { c.cur = TablePos{1, 1}; c.SetAnchor(&TablePos{0, 0}) }},
	{"after y", func(c *TableCore) { c.yanked = true }},
	{"mid table", func(c *TableCore) { c.cur = TablePos{1, 1} }},
}

func TestTableKeysMatchTheOldHandlerWithNoMap(t *testing.T) {
	for _, sortable := range []bool{false, true} {
		for _, activates := range []bool{false, true} {
			for _, st := range tableStates {
				run := func(handle func(*TableCore, tui.KeyEvent) bool) func(tui.KeyEvent) string {
					return func(e tui.KeyEvent) string {
						var copies, acts []string
						opts := []TableOption{TableSortable(sortable), TableOnCopy(func(s string) { copies = append(copies, s) })}
						if activates {
							opts = append(opts, TableOnActivate(func(row int) { acts = append(acts, fmt.Sprint(row)) }))
						}
						c := NewTableCore(people(), opts...)
						c.Bind(nil, &fakeTableLayout{})
						st.prepare(c)
						ret := handle(c, e)
						return tableEffect(c, ret, copies, acts)
					}
				}
				keyGridDiff(t, fmt.Sprintf("Table (%s, sortable %v, activates %v)", st.name, sortable, activates),
					run(oldTableKey), run((*TableCore).HandleKey), noTextShiftedPrintable)
			}
		}
	}
}

// Every table command reaches HandleKey: none is declared and then dropped by the switch.
func TestEveryTableCommandDoesSomething(t *testing.T) {
	for _, cmd := range tableCommands {
		keys := cmd.Keys
		if len(keys) == 0 {
			t.Fatalf("%s has no default key", cmd.ID)
		}
		c := NewTableCore(people(), TableSortable(true), TableOnActivate(func(int) {}))
		c.Bind(nil, &fakeTableLayout{})
		c.cur = TablePos{1, 1}
		before := tableEffect(c, false, nil, nil)
		k := keys[0]
		e := tui.KeyEvent{Code: k.Code, Mods: k.Mods}
		ret := c.HandleKey(e)
		if cmd.ID == TableGoPrefix.ID {
			ret = c.HandleKey(e) // gg
		}
		if !ret && cmd.ID != TableEscape.ID {
			t.Errorf("%s (%v): not handled", cmd.ID, k)
		}
		if after := tableEffect(c, false, nil, nil); after == before && !slices.Contains([]string{TableEscape.ID, TableYank.ID, TableActivate.ID, TableFieldPrev.ID}, cmd.ID) {
			t.Errorf("%s (%v): changed nothing", cmd.ID, k)
		}
	}
}
