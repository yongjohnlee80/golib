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
//
// # Stylesheet Resolution & Cascade Pipeline
//
// Below is the external stylesheet fetching and CSS cascade integration pipeline:
//
//	  <link rel="stylesheet" href="...">
//	                   │
//	                   ▼
//	          htmlSheets.entry(href)
//	                   │
//	        ┌──────────┴──────────┐
//	        │  Cached Entry?      │─── Yes ───► Use existing sheetEntry
//	        └──────────┬──────────┘
//	                   │ No
//	                   ▼
//	          ctx.Go(loadSheet)
//	                   │
//	                   ▼
//	          Resolver.Resolve(href)
//	                   │
//	        ┌──────────┴──────────┐
//	        │  Within 1MB Cap?    │─── No ───► imgFailed / ErrImageCap
//	        └──────────┬──────────┘
//	                   │ Yes
//	                   ▼
//	          htmlSheets.done() -> sheetEntry.state = imgReady
//	                   │
//	                   ▼
//	          Re-parse CSS rules & re-evaluate style cascade

const (
	maxSheetBytes = 1 << 20 // a stylesheet
	maxSheets     = 32      // linked stylesheets a page loads
)

var errSheetCap = errors.New("htmlview: stylesheet over a size cap")

type sheetEntry struct {
	state   imgState
	text    string
	refused bool // the resolver refused it: reported again each time a page uses it
}

type htmlSheets struct {
	v                 loadHost
	loads[sheetEntry] // by href; reset when a new resolver reads href as another file
}

// newHTMLSheets constructs the external stylesheet manager for an HTML layout instance.
func newHTMLSheets(v loadHost) *htmlSheets {
	return &htmlSheets{v: v, loads: newLoads[sheetEntry]()}
}

// entry is href's sheet, its load started on first sight; one with nothing to load it fails.
func (m *htmlSheets) entry(href string) *sheetEntry {
	if e, ok := m.entries[href]; ok {
		if e.refused {
			m.v.Refused(href)
		}
		return e
	}
	e := &sheetEntry{state: imgLoading}
	m.entries[href] = e
	ctx, resolve := m.v.Context(), m.v.Images()
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
	href, e, stale, ok := m.take(r.ID)
	if !ok {
		return false
	}
	if stale {
		return true // the old resolver's: dropped
	}
	if errors.Is(r.Err, tuiwidget.ErrImageRefused) {
		m.v.Refused(href)
		if e != nil {
			e.refused = true
		}
	}
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
