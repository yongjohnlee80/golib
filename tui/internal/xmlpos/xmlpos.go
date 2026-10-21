// Package xmlpos is where an xml.Decoder is in its input, for the errors of the XML readers that
// name a line (tui/i18n's catalogs, tui/keymap's schemes).
package xmlpos

import "encoding/xml"

// Line is the 1-based line d has read up to.
func Line(d *xml.Decoder) int {
	l, _ := d.InputPos()
	return l
}
