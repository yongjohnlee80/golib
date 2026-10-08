package docx

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"runtime"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/extract"
)

// build is a DOCX archive of the given parts, in order.
func build(t *testing.T, parts ...[2]string) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for _, p := range parts {
		w, err := zw.Create(p[0])
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(w, p[1])
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

const ns = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"`

func document(body string) [2]string {
	return [2]string{"word/document.xml", `<?xml version="1.0"?><w:document ` + ns + `><w:body>` + body + `</w:body></w:document>`}
}

func p(style, text string) string {
	pPr := ""
	if style != "" {
		pPr = `<w:pPr><w:pStyle w:val="` + style + `"/></w:pPr>`
	}
	return `<w:p>` + pPr + `<w:r><w:t>` + text + `</w:t></w:r></w:p>`
}

func li(numID, ilvl, text string) string {
	return `<w:p><w:pPr><w:numPr><w:ilvl w:val="` + ilvl + `"/><w:numId w:val="` + numID + `"/></w:numPr></w:pPr><w:r><w:t>` + text + `</w:t></w:r></w:p>`
}

var styles = [2]string{"word/styles.xml", `<w:styles ` + ns + `>
<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/></w:style>
<w:style w:type="paragraph" w:styleId="H2x"><w:name w:val="Custom"/><w:pPr><w:outlineLvl w:val="1"/></w:pPr></w:style>
<w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/></w:style>
</w:styles>`}

var numbering = [2]string{"word/numbering.xml", `<w:numbering ` + ns + `>
<w:abstractNum w:abstractNumId="10"><w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/></w:lvl><w:lvl w:ilvl="1"><w:numFmt w:val="decimal"/></w:lvl></w:abstractNum>
<w:abstractNum w:abstractNumId="11"><w:lvl w:ilvl="0"><w:numFmt w:val="decimal"/></w:lvl></w:abstractNum>
<w:num w:numId="1"><w:abstractNumId w:val="10"/></w:num>
<w:num w:numId="2"><w:abstractNumId w:val="11"/></w:num>
</w:numbering>`}

var core = [2]string{"docProps/core.xml", `<cp:coreProperties xmlns:cp="x" xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>The Manual</dc:title></cp:coreProperties>`}
var app = [2]string{"docProps/app.xml", `<Properties><Pages>12</Pages></Properties>`}

func run(t *testing.T, e Extractor, data []byte) (string, extract.Info, error) {
	t.Helper()
	var out bytes.Buffer
	info, err := e.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), &out)
	return out.String(), info, err
}

func TestDocxToMarkdown(t *testing.T) {
	body := p("Title", "Big Title") + p("Heading1", "Intro") + p("", "Plain text.") +
		p("H2x", "Sub") +
		li("1", "0", "bullet one") + li("1", "1", "nested ordered") + li("2", "0", "ordered") +
		`<w:p><w:hyperlink r:id="x"><w:r><w:t>a link</w:t></w:r></w:hyperlink><w:r><w:t> after</w:t></w:r></w:p>` +
		`<w:p><w:r><w:drawing><w:t>IMAGE ALT</w:t></w:drawing></w:r><w:r><w:t>beside</w:t></w:r></w:p>` +
		`<w:tbl><w:tr><w:tc>` + p("", "h|1") + `</w:tc><w:tc>` + p("", "h2") + `</w:tc></w:tr>` +
		`<w:tr><w:tc><w:p><w:r><w:t>line</w:t><w:br/><w:t>break</w:t></w:r></w:p></w:tc><w:tc>` + p("", "x") + `</w:tc></w:tr></w:tbl>` +
		p("", "End.")
	data := build(t, styles, numbering, core, app, document(body))
	got, info, err := run(t, Extractor{}, data)
	if err != nil {
		t.Fatal(err)
	}
	want := "# Big Title\n\n# Intro\n\nPlain text.\n\n## Sub\n\n" +
		"- bullet one\n  1. nested ordered\n1. ordered\n\n" +
		"a link after\n\nbeside\n\n" +
		"| h\\|1 | h2 |\n| --- | --- |\n| line<br>break | x |\n\nEnd."
	if got != want {
		t.Errorf("markdown:\n got %q\nwant %q", got, want)
	}
	if info.Title != "The Manual" || info.Pages != 12 {
		t.Errorf("info = %+v", info)
	}
	if strings.Contains(got, "IMAGE ALT") {
		t.Error("media text was kept")
	}
}

func TestDocxWithoutStylesOrPropertiesStillReads(t *testing.T) {
	got, info, err := run(t, Extractor{}, build(t, document(p("", "only"))))
	if err != nil || got != "only" || info.Title != "" {
		t.Errorf("%q %+v %v", got, info, err)
	}
	if _, _, err := run(t, Extractor{}, build(t, [2]string{"other.xml", "<x/>"})); !errors.Is(err, ErrNotDocx) {
		t.Errorf("a ZIP without a document: %v", err)
	}
	if _, _, err := run(t, Extractor{}, []byte("not a zip")); err == nil {
		t.Error("garbage accepted")
	}
	if _, err := (Extractor{}).Extract(context.Background(), bytes.NewReader(nil), -1, io.Discard); err == nil {
		t.Error("a negative size accepted")
	}
}

func TestDocxEntryLimitStopsAZipBomb(t *testing.T) {
	// 8 MiB of one character compresses to a few KiB: the entry's header says its true size, and
	// the counting limit stops the read anyway at the limit set, whatever a header claims.
	bomb := document(p("", strings.Repeat("A", 8<<20)))
	data := build(t, bomb)
	if len(data) > 64<<10 {
		t.Fatalf("the bomb is %d bytes compressed, not a bomb", len(data))
	}
	_, _, err := run(t, Extractor{MaxEntry: 1 << 20, MaxBlock: 16 << 20}, data)
	if !errors.Is(err, ErrEntryTooLarge) {
		t.Errorf("err = %v, want ErrEntryTooLarge", err)
	}
}

func TestDocxTotalLimitCountsEveryEntry(t *testing.T) {
	big := strings.Repeat("x", 600<<10)
	data := build(t,
		[2]string{"word/styles.xml", `<w:styles ` + ns + `><!--` + big + `--></w:styles>`},
		[2]string{"word/numbering.xml", `<w:numbering ` + ns + `><!--` + big + `--></w:numbering>`},
		document(p("", "x")))
	// each entry is under the entry limit; together they pass the total
	_, _, err := run(t, Extractor{MaxEntry: 1 << 20, MaxTotal: 1 << 20}, data)
	if !errors.Is(err, ErrArchiveTooLarge) {
		t.Errorf("err = %v, want ErrArchiveTooLarge", err)
	}
	if _, _, err := run(t, Extractor{MaxEntry: 1 << 20, MaxTotal: 4 << 20}, data); err != nil {
		t.Errorf("under both limits: %v", err)
	}
}

func TestDocxDepthLimit(t *testing.T) {
	deep := strings.Repeat("<w:x>", 300) + strings.Repeat("</w:x>", 300)
	_, _, err := run(t, Extractor{}, build(t, document(deep)))
	if !errors.Is(err, ErrTooDeep) {
		t.Errorf("err = %v, want ErrTooDeep", err)
	}
	if _, _, err := run(t, Extractor{MaxDepth: 1000}, build(t, document(deep))); err != nil {
		t.Errorf("a raised depth limit: %v", err)
	}
}

func TestDocxBlockLimit(t *testing.T) {
	_, _, err := run(t, Extractor{MaxBlock: 1000}, build(t, document(p("", strings.Repeat("y", 2000)))))
	if !errors.Is(err, ErrBlockTooLarge) {
		t.Errorf("err = %v, want ErrBlockTooLarge", err)
	}
}

func TestDocxMaxTextThroughSet(t *testing.T) {
	data := build(t, document(p("", strings.Repeat("word ", 1000))))
	s := extract.New(extract.Register(Extractor{}, ".docx"), extract.MaxText(100))
	_, err := s.Extract(context.Background(), "a.docx", bytes.NewReader(data), int64(len(data)), io.Discard)
	if !errors.Is(err, extract.ErrTextTooLarge) {
		t.Errorf("err = %v, want extract.ErrTextTooLarge", err)
	}
}

func TestDocxStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	data := build(t, document(p("", "x")))
	if _, err := (Extractor{}).Extract(ctx, bytes.NewReader(data), int64(len(data)), io.Discard); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
}

// The document is read a token at a time and one paragraph is held: a large document's peak heap
// stays near a constant, far under its decompressed size.
func TestDocxPeakHeapIsBounded(t *testing.T) {
	var body strings.Builder
	for i := 0; i < 60000; i++ {
		body.WriteString(p("", "paragraph number with some words in it to make it longer than a few bytes"))
	}
	data := build(t, document(body.String()))
	size := len(document(body.String())[1])
	old := debug.SetGCPercent(10)
	defer debug.SetGCPercent(old)
	runtime.GC()
	var base runtime.MemStats
	runtime.ReadMemStats(&base)
	var peak atomic.Uint64
	done := make(chan struct{})
	go func() {
		var m runtime.MemStats
		for {
			select {
			case <-done:
				return
			default:
			}
			runtime.ReadMemStats(&m)
			if m.HeapAlloc > peak.Load() {
				peak.Store(m.HeapAlloc)
			}
			time.Sleep(time.Millisecond)
		}
	}()
	// io.Discard: the output is the caller's to hold, not the extractor's
	_, err := (Extractor{}).Extract(context.Background(), bytes.NewReader(data), int64(len(data)), io.Discard)
	close(done)
	if err != nil {
		t.Fatal(err)
	}
	grew := int64(peak.Load()) - int64(base.HeapAlloc)
	t.Logf("document.xml %d bytes decompressed; peak heap growth %d bytes", size, grew)
	if grew > 16<<20 || grew > int64(size)/2 {
		t.Errorf("peak heap grew %d bytes for a %d-byte document", grew, size)
	}
}

// The identity a derived cache keys this extractor's text by: a new name for its move into golib,
// and its first version.
func TestExtractorIdentity(t *testing.T) {
	if id, v := (Extractor{}).ID(), (Extractor{}).Version(); id != "golib/docx" || v != "1" {
		t.Fatalf("identity %s@%s, want golib/docx@1", id, v)
	}
}
