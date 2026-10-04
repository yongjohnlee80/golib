package view

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/synctest"
)

func viewOf(t *testing.T, format, body string) *View {
	t.Helper()
	return mustNew(t, "---\nversion: 1\nname: v\nsource: sqlite\nprocess: SELECT 1\nformat: "+format+"\n---\n"+body)
}

func render(t *testing.T, v *View, x string) string {
	t.Helper()
	doc, err := v.Parse([]byte(x))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// A JSON body stays valid JSON whatever the text it prints holds: quotes, backslashes, newlines,
// control characters, and markup, which encoding/json would otherwise turn into \u escapes.
func TestFormat_JSONEscapesEveryPrintedValue(t *testing.T) {
	t.Parallel()
	v := viewOf(t, FormatJSON, `{"id": "{{.id}}", "lyric": "{{.lyric}}", "bpm": {{.bpm}}, "note": "{{.missing}}"}`)
	lyric := "She said \"run\"\\\nthen\ttab \x01 <b>&</b>"
	x, _ := json.Marshal(map[string]any{"id": "trk_1", "lyric": lyric, "bpm": 142})
	doc := render(t, v, string(x))
	var got map[string]any
	if err := json.Unmarshal([]byte(doc), &got); err != nil {
		t.Fatalf("the document is not JSON: %v\n%s", err, doc)
	}
	if got["lyric"] != lyric || got["id"] != "trk_1" || got["bpm"] != float64(142) || got["note"] != "" {
		t.Errorf("round trip = %#v", got)
	}
	if !strings.Contains(doc, "<b>&</b>") {
		t.Errorf("markup was escaped: %s", doc)
	}
}

func TestFormat_XMLAndHTMLEscapeCharacterData(t *testing.T) {
	t.Parallel()
	for _, format := range []string{FormatXML, FormatHTML} {
		v := viewOf(t, format, `<t a="{{.a}}">{{.b}}</t>{{raw .b}}`)
		got := render(t, v, `{"a":"\"q' &","b":"<i>x</i>"}`)
		if want := `<t a="&#34;q&#39; &amp;">&lt;i&gt;x&lt;/i&gt;</t><i>x</i>`; got != want {
			t.Errorf("%s: %s, want %s", format, got, want)
		}
	}
}

func TestFormat_TextAndMarkdownPrintAsIs(t *testing.T) {
	t.Parallel()
	for _, format := range []string{FormatText, FormatMarkdown} {
		v := viewOf(t, format, `{{.a}}`)
		if got := render(t, v, `{"a":"<\"*x*\">"}`); got != `<"*x*">` {
			t.Errorf("%s: %q", format, got)
		}
	}
}

// A NULL column and a key the row does not have both print as nothing, never as "<no value>".
func TestRender_NullAndMissingPrintNothing(t *testing.T) {
	t.Parallel()
	v := viewOf(t, FormatText, `[{{.null}}][{{.missing}}][{{.nested.missing}}]`)
	if got := render(t, v, `{"null":null,"nested":{}}`); got != "[][][]" {
		t.Errorf("got %q", got)
	}
}

func TestRender_Values(t *testing.T) {
	t.Parallel()
	v := viewOf(t, FormatText, `{{.big}} {{.float}} {{.yes}} {{.list}} {{.obj}}`)
	got := render(t, v, `{"big":12345678901234567890,"float":1.50,"yes":true,"list":[1,"a"],"obj":{"k":"<v>"}}`)
	if want := `12345678901234567890 1.50 true [1,"a"] {"k":"<v>"}`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRender_Helpers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ format, body, x, want string }{
		{FormatJSON, `{{toJson .assoc}}`, `{"assoc":[{"type":"label","title":"A \"B\" <C>"}]}`, `[{"title":"A \"B\" <C>","type":"label"}]`},
		{FormatJSON, `{{toJson .n}}`, `{"n":0.10}`, `0.10`},
		{FormatText, `{{quote .s}} {{quote .n}} {{quote .missing}}`, `{"s":"a\"b","n":7}`, `"a\"b" "7" ""`},
		{FormatJSON, `{{raw .s}}`, `{"s":"\"x\""}`, `"x"`},
		{FormatText, `[{{trim .s}}][{{.s | trim | upper}}][{{lower "AbC"}}]`, `{"s":"  ab \n"}`, `[ab][AB][abc]`},
		{FormatText, `{{default "N/A" .missing}}|{{default "N/A" .null}}|{{default "N/A" .e}}|{{default "N/A" .l}}|{{default "N/A" .o}}`, `{"null":null,"e":"","l":[],"o":{}}`, `N/A|N/A|N/A|N/A|N/A`},
		{FormatText, `{{.zero | default "N/A"}}|{{.f | default "N/A"}}|{{.s | default "N/A"}}`, `{"zero":0,"f":false,"s":"x"}`, `0|false|x`},
		{FormatText, `{{default "d" (raw "")}}`, `{}`, `d`},
		{FormatHTML, `{{default "<none>" .missing}}`, `{}`, `&lt;none&gt;`},
		{FormatText, `{{range .tags}}- {{.}}
{{end}}`, `{"tags":["a","b"]}`, "- a\n- b\n"},
		{FormatText, `{{with .o}}{{.k}}{{else}}none{{end}}`, `{"o":{"k":"v"}}`, `v`},
		{FormatText, `{{$x := .k}}{{$x}}`, `{"k":"v"}`, `v`},
		{FormatJSON, `{{define "q"}}"{{.}}"{{end}}{{template "q" .s}}`, `{"s":"a\"b"}`, `"a\"b"`},
	} {
		v := viewOf(t, tc.format, tc.body)
		if got := render(t, v, tc.x); got != tc.want {
			t.Errorf("%s %s: got %q, want %q", tc.format, tc.body, got, tc.want)
		}
	}
}

func TestRender_RowMustBeOneJSONValue(t *testing.T) {
	t.Parallel()
	v := viewOf(t, FormatText, `{{.a}}`)
	for _, x := range []string{``, `{`, `{"a":1} {"a":2}`, `{"a":1} x`} {
		if _, err := v.Parse([]byte(x)); err == nil {
			t.Errorf("%q: rendered", x)
		}
		if _, err := v.Read([]byte(x)); err == nil {
			t.Errorf("%q: Read accepted", x)
		}
		if err := v.Execute(io.Discard, []byte(x)); err == nil {
			t.Errorf("%q: Execute accepted", x)
		}
	}
	if got := render(t, v, " {\"a\":1}\n "); got != "1" {
		t.Errorf("surrounding space: %q", got)
	}
}

func TestRender_ExecutionErrors(t *testing.T) {
	t.Parallel()
	v := viewOf(t, FormatText, `{{index .a 5}}`)
	if _, err := v.Parse([]byte(`{"a":[1]}`)); err == nil {
		t.Error("Parse: an out-of-range index rendered")
	}
	r, err := v.Read([]byte(`{"a":[1]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(r); err == nil {
		t.Error("Read: the stream ended cleanly after a failed render")
	}
	r.Close()
}

func TestExecute_WritesToTheWriter(t *testing.T) {
	t.Parallel()
	v := viewOf(t, FormatText, `# {{.t}}`)
	var buf bytes.Buffer
	if err := v.Execute(&buf, []byte(`{"t":"x"}`)); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "# x" {
		t.Errorf("got %q", buf.String())
	}
}

func TestRead_StreamsTheDocument(t *testing.T) {
	t.Parallel()
	v := viewOf(t, FormatText, `# {{.t}}`)
	r, err := v.Read([]byte(`{"t":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil || string(b) != "# x" {
		t.Errorf("read %q, %v", b, err)
	}
}

// A consumer that stops reading early closes the stream, and the goroutine writing the document
// ends: synctest fails the test if any goroutine it started is still blocked when it returns.
func TestRead_CloseEndsTheWriterMidDocument(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		v := viewOf(t, FormatText, `{{range .rows}}{{.}}{{end}}`)
		rows := make([]string, 10000)
		for i := range rows {
			rows[i] = strings.Repeat("x", 100)
		}
		x, _ := json.Marshal(map[string]any{"rows": rows})
		r, err := v.Read(x)
		if err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 10)
		if _, err := io.ReadFull(r, buf); err != nil {
			t.Fatal(err)
		}
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if _, err := r.Read(buf); !errors.Is(err, io.ErrClosedPipe) {
			t.Errorf("read after close: %v", err)
		}
	})
}

func TestView_IsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()
	v := viewOf(t, FormatJSON, `{"t":"{{.t}}"}`)
	done := make(chan string, 8)
	for i := range 8 {
		go func() {
			x, _ := json.Marshal(map[string]any{"t": strings.Repeat("\"", i)})
			doc, err := v.Parse(x)
			if err != nil {
				done <- err.Error()
				return
			}
			r, err := v.Read(x)
			if err != nil {
				done <- err.Error()
				return
			}
			b, _ := io.ReadAll(r)
			r.Close()
			if string(b) != doc {
				done <- "Read and Parse differ"
				return
			}
			done <- ""
		}()
	}
	for range 8 {
		if msg := <-done; msg != "" {
			t.Error(msg)
		}
	}
}
