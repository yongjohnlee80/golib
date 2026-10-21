package widget

import (
	"strings"
	"sync"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/i18n"
)

// toolkitCatalogs is golib's own catalogs, for text a widget shows before it is mounted. It
// is only read, never added to, so one Set serves every widget.
var toolkitCatalogs = sync.OnceValue(i18n.Toolkit)

// translate is m in the App's language once the widget is mounted. Before that there is no
// App to ask, so it is golib's English for one of golib's own ids, and the id otherwise.
func (b *Base) translate(m tui.Message) string {
	if b.ctx != nil {
		return b.ctx.Translate(m)
	}
	return englishText(m)
}

// englishText is m as golib's own English catalog writes it, or its id when golib has none.
func englishText(m tui.Message) string {
	if text, ok := toolkitCatalogs().Lookup(i18n.English, m.ID); ok {
		return text
	}
	return m.ID
}

// withArg fills a message's "%1" with a, as Qt's QString::arg does. A message is translated
// whole and then filled, so no language ever sees its sentence assembled from fragments.
func withArg(text, a string) string { return strings.ReplaceAll(text, "%1", a) }

// uiText is a text a widget shows: plain text, shown as it is, or a catalog message, resolved when
// read in the language of the moment. A widget holds one per text and reads it (text) where it
// measures and paints, so no Layout stores a translation and a language switch needs only a new
// layout. The zero value is the empty plain text.
type uiText struct {
	plain string
	msg   tui.Message
}

// plainText is s, shown as given.
func plainText(s string) uiText { return uiText{plain: s} }

// messageText is the catalog message m.
func messageText(m tui.Message) uiText { return uiText{msg: m} }

// inLanguage is u as b shows it now: the plain text, or the message in b's App's language (English before
// mount).
func (u uiText) inLanguage(b *Base) string {
	if u.msg != (tui.Message{}) {
		return b.translate(u.msg)
	}
	return u.plain
}
