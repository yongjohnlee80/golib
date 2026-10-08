package widget

import (
	"context"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	phtml "github.com/yongjohnlee80/golib/parse/html"
	"github.com/yongjohnlee80/golib/tui"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// LINKED STYLESHEETS — a page's <link rel="stylesheet" href>, loaded through the view's resolver
// off the loop, as images are: the page is built at once without a sheet still on its way, and
// built again when it lands. Sheets keep their place in the cascade among the page's <style>s.
// A new resolver drops them with the images. @import is not followed.
const (
	maxSheetBytes = 1 << 20 // a stylesheet
	maxSheets     = 32      // linked stylesheets a page loads
)

var errSheetCap = errors.New("htmlview: stylesheet over a size cap")

type sheetEntry struct {
	state imgState
	text  string
}

type htmlSheets struct {
	l       *htmlLayout
	entries map[string]*sheetEntry
	tasks   map[tui.TaskID]string
	stale   map[tui.TaskID]struct{} // loads started before the last reset: their results are dropped
}

func newHTMLSheets(l *htmlLayout) *htmlSheets {
	return &htmlSheets{l: l, entries: map[string]*sheetEntry{}, tasks: map[tui.TaskID]string{},
		stale: map[tui.TaskID]struct{}{}}
}

// reset forgets every sheet and every load on its way: a new resolver reads href as another file.
func (m *htmlSheets) reset() {
	for id := range m.tasks {
		m.stale[id] = struct{}{}
	}
	m.entries, m.tasks = map[string]*sheetEntry{}, map[tui.TaskID]string{}
}

// entry is href's sheet, its load started on first sight; one with nothing to load it fails.
func (m *htmlSheets) entry(href string) *sheetEntry {
	if e, ok := m.entries[href]; ok {
		return e
	}
	e := &sheetEntry{state: imgLoading}
	m.entries[href] = e
	ctx, resolve := m.l.v.Context(), m.l.v.Images()
	if ctx == nil || resolve == nil || href == "" {
		e.state = imgFailed
		return e
	}
	id := ctx.Go(func(c context.Context) (any, error) { return loadSheet(c, href, resolve) })
	m.tasks[id] = href
	return e
}

// done takes a load's result; it reports whether the result was a sheet's.
func (m *htmlSheets) done(r tui.TaskResult) bool {
	if _, ok := m.stale[r.ID]; ok {
		delete(m.stale, r.ID)
		return true
	}
	href, ok := m.tasks[r.ID]
	if !ok {
		return false
	}
	delete(m.tasks, r.ID)
	if errors.Is(r.Err, tuiwidget.ErrImageRefused) {
		m.l.v.Refused(href)
	}
	e := m.entries[href]
	text, isText := r.Value.(string)
	if e == nil || r.Err != nil || !isText {
		if e != nil {
			e.state = imgFailed
		}
		return true
	}
	e.text, e.state = text, imgReady
	return true
}

// loadSheet reads a stylesheet off the loop, under the cap; text that is not UTF-8 is refused.
func loadSheet(ctx context.Context, href string, resolve tuiwidget.ImageResolver) (any, error) {
	rc, err := resolve(ctx, href)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, maxSheetBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxSheetBytes {
		return nil, errSheetCap
	}
	if !utf8.Valid(b) {
		return nil, errors.New("htmlview: a stylesheet that is not UTF-8")
	}
	return string(b), nil
}

// sheetLink is the href of a <link> that is a stylesheet; false for any other element.
func sheetLink(n *phtml.Node) (string, bool) {
	if n.Name != "link" {
		return "", false
	}
	rel, _ := n.Attr("rel")
	stylesheet := false
	for _, tok := range strings.Fields(strings.ToLower(rel)) {
		switch tok {
		case "stylesheet":
			stylesheet = true
		case "alternate":
			return "", false // an alternate sheet is not applied
		}
	}
	href, _ := n.Attr("href")
	href = strings.TrimSpace(href)
	return href, stylesheet && href != ""
}

// mediaWrapped is a linked sheet's text under its media attribute: whole when there is none or it
// is "all", else inside an @media block the cascade evaluates as any other.
func mediaWrapped(text, media string) string {
	media = strings.TrimSpace(media)
	if media == "" || strings.EqualFold(media, "all") {
		return text
	}
	return "@media " + media + " {\n" + text + "\n}"
}
