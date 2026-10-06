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
