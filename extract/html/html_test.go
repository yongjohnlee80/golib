package html

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/extract"
	phtml "github.com/yongjohnlee80/golib/parse/html"
)

func run(t *testing.T, e Extractor, src string) (string, extract.Info, error) {
	t.Helper()
	var b strings.Builder
	info, err := e.Extract(context.Background(), strings.NewReader(src), int64(len(src)), &b)
	return b.String(), info, err
}

func mustRun(t *testing.T, src string) (string, extract.Info) {
	t.Helper()
	out, info, err := run(t, Extractor{}, src)
	if err != nil {
		t.Fatal(err)
	}
	return out, info
}

// A page's noise is gone and its content stays: scripts, styles, the nav bar, the footer, a
// cookie banner in an aside and inline handlers leave no text.
func TestTheContentAlone(t *testing.T) {
	src := `<!doctype html><html><head><title>Fish &amp; Chips</title>
<style>body { color: red }</style><script>track("visit")</script></head>
<body onload="boot()">
<nav><a href="/">Home</a> <a href="/about">About</a></nav>
<header><p>Site banner</p></header>
<h1>Frying</h1>
<p>Heat the <strong>oil</strong> to <em>180&nbsp;°C</em>, then use <code>fry()</code>.</p>
<ul><li>Cod<ul><li>fresh</li></ul></li><li>Haddock</li></ul>
<ol start="3"><li>Third</li><li>Fourth</li></ol>
<table><tr><th>Fish</th><th>Minutes</th></tr><tr><td>Cod</td><td>6</td></tr><tr><td>A|B</td></tr></table>
<pre class="language-go">
if hot { fry() }
</pre>
<blockquote><p>Never leave the pan.</p></blockquote>
<img src="x.png" alt="A golden fillet">
<p hidden>hidden text</p><div aria-hidden="true">aria text</div>
<noscript>enable scripts</noscript><svg><text>svg text</text></svg>
<form><input value="form text">Form label</form>
<aside>We use cookies</aside>
<footer>© footer</footer>
<script>var leaked = "script text";</script>
</body></html>`
	out, info := mustRun(t, src)
	want := "# Frying\n\n" +
		"Heat the **oil** to *180 °C*, then use `fry()`.\n\n" +
		"- Cod\n  - fresh\n- Haddock\n\n" +
		"3. Third\n4. Fourth\n\n" +
		"| Fish | Minutes |\n| --- | --- |\n| Cod | 6 |\n| A\\|B |  |\n\n" +
		"```go\nif hot { fry() }\n```\n\n" +
		"> Never leave the pan.\n\n" +
		"A golden fillet\n"
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
	for _, noise := range []string{"track", "color: red", "Home", "About", "Site banner", "hidden text", "aria text",
		"enable scripts", "svg text", "Form label", "cookies", "footer", "script text", "boot"} {
		if strings.Contains(out, noise) {
			t.Errorf("%q leaked into the content", noise)
		}
	}
	if info.Title != "Fish & Chips" {
		t.Errorf("title %q", info.Title)
	}
}

// A page with main is read as main alone, and one with an article as its first article: the
// chrome around them is not read, even where it is not nav or footer.
func TestMainOrArticle(t *testing.T) {
	out, _ := mustRun(t, `<body><div>sidebar</div><main><p>the story</p></main><div>more chrome</div></body>`)
	if out != "the story\n" {
		t.Errorf("main: %q", out)
	}
	out, _ = mustRun(t, `<body><p>chrome</p><article><h2>One</h2></article><article><h2>Two</h2></article></body>`)
	if out != "## One\n" {
		t.Errorf("article: %q", out)
	}
}

func TestTitleFallsBackToTheFirstHeading(t *testing.T) {
	_, info := mustRun(t, `<h2>Sub</h2><h1>The <b>Heading</b></h1>`)
	if info.Title != "The **Heading**" {
		t.Errorf("title %q", info.Title)
	}
}

func TestWhitespaceCollapses(t *testing.T) {
	out, _ := mustRun(t, "<p>  one\n\t two <b> three </b>four<i></i> five</p><p>a<br>b</p>")
	if out != "one two **three** four five\n\na\nb\n" {
		t.Errorf("%q", out)
	}
}

func TestFenceOutgrowsBackticks(t *testing.T) {
	out, _ := mustRun(t, "<pre><code>a ``` b</code></pre>")
	if out != "````\na ``` b\n````\n" {
		t.Errorf("%q", out)
	}
}

func TestLimits(t *testing.T) {
	attrs := func(n int) string {
		var b strings.Builder
		for i := range n {
			b.WriteString(" d" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + "=1")
		}
		return b.String()
	}
	for _, c := range []struct {
		name, src string
		e         Extractor
	}{
		{"source", "<p>" + strings.Repeat("x", 100) + "</p>", Extractor{Limits: phtml.Limits{MaxSource: 50}}},
		{"nodes", strings.Repeat("<b>x</b>", 200), Extractor{Limits: phtml.Limits{MaxNodes: 100}}},
		{"65 attributes, hidden last", "<p" + attrs(64) + " hidden>secret</p>", Extractor{}},
		{"a 65 KiB value", `<p title="` + strings.Repeat("v", 65<<10) + `" hidden>secret</p>`, Extractor{}},
	} {
		out, _, err := run(t, c.e, c.src)
		if !errors.Is(err, ErrTooLarge) {
			t.Errorf("%s: err %v, want ErrTooLarge", c.name, err)
		}
		if out != "" {
			t.Errorf("%s: wrote %q", c.name, out)
		}
	}
}

// A noscript opened past the parse's depth limit still drops its text.
func TestDeepSuppressionHolds(t *testing.T) {
	src := strings.Repeat("<div>", 300) + "<noscript>secret</noscript><p>kept</p>" + strings.Repeat("</div>", 300)
	out, _ := mustRun(t, src)
	if strings.Contains(out, "secret") || !strings.Contains(out, "kept") {
		t.Errorf("%q", out)
	}
}

func TestCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	src := strings.Repeat("<p>x</p>", 10000)
	if _, err := (Extractor{}).Extract(ctx, strings.NewReader(src), int64(len(src)), &strings.Builder{}); !errors.Is(err, context.Canceled) {
		t.Errorf("err %v", err)
	}
}

func TestIdentity(t *testing.T) {
	if id, v := (Extractor{}).ID(), (Extractor{}).Version(); id != "golib/html" || v != "1" {
		t.Errorf("identity %s@%s", id, v)
	}
}

// Through a Set, the output is cut at MaxText as any extractor's is.
func TestThroughASet(t *testing.T) {
	s := extract.New(extract.Register(Extractor{}, ".html"), extract.MaxText(20))
	src := "<p>" + strings.Repeat("word ", 100) + "</p>"
	_, err := s.Extract(context.Background(), "a.html", strings.NewReader(src), int64(len(src)), &strings.Builder{})
	if !errors.Is(err, extract.ErrTextTooLarge) {
		t.Errorf("err %v, want ErrTextTooLarge", err)
	}
}

// Suppression past the depth limit costs each encloser once: 100,000 flattened elements, each
// followed by text, would take hours if every text walked its whole chain of enclosers.
func TestDeepChainsCostLinearTime(t *testing.T) {
	var b strings.Builder
	for range 100000 {
		b.WriteString("<span>t")
	}
	src := b.String()
	out, _, err := run(t, Extractor{}, src)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, "t") != 100000 {
		t.Errorf("%d texts, want 100000", strings.Count(out, "t"))
	}
}
