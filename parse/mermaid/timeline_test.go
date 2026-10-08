package mermaid_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/mermaid"
)

func timelineParse(t *testing.T, src string) *mermaid.TimelineDiagram {
	t.Helper()
	d, err := mermaid.Parse(src)
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	tl, ok := d.(*mermaid.TimelineDiagram)
	if !ok || d.Kind() != mermaid.Timeline {
		t.Fatalf("%q parsed as %T", src, d)
	}
	return tl
}

// timelineShape is a timeline as section › period: events, for comparing.
func timelineShape(d *mermaid.TimelineDiagram) []string {
	var out []string
	for _, s := range d.Sections {
		for _, p := range s.Periods {
			var ev []string
			for _, e := range p.Events {
				ev = append(ev, e.Text)
			}
			out = append(out, s.Title+" › "+p.Label+": "+strings.Join(ev, " | "))
		}
	}
	return out
}

// Mermaid's documented timeline: a period with its events on its line, and more on the lines
// under it, each its own.
func TestTimelineOfPeriodsAndEvents(t *testing.T) {
	d := timelineParse(t, `timeline
    title History of Social Media Platform
    2002 : LinkedIn
    2004 : Facebook
         : Google
    2005 : YouTube
    2006 : Twitter : Tumblr
    %% a comment
    %%{init: { 'theme': 'forest' } }%%
    accTitle: The platforms
    2007`)
	if d.Title != "History of Social Media Platform" {
		t.Errorf("title %q", d.Title)
	}
	want := []string{
		" › 2002: LinkedIn",
		" › 2004: Facebook | Google",
		" › 2005: YouTube",
		" › 2006: Twitter | Tumblr",
		" › 2007: ",
	}
	if got := timelineShape(d); !reflect.DeepEqual(got, want) {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

// Sections hold the periods after them; an event's text keeps its commas, and <br> breaks it.
func TestTimelineSections(t *testing.T) {
	d := timelineParse(t, `timeline
    title Timeline of Industrial Revolution
    0 : before any section
    section 17th-20th century
        Industry 1.0 : Machinery, Water power, Steam <br>power
        Industry 2.0 : Electricity, Internal combustion engine, Mass production
    section 21st century
        Industry 4.0 : Internet, Robotics, Internet of Things
        "Industry 5.0" : "Artificial intelligence"`)
	want := []string{
		" › 0: before any section",
		"17th-20th century › Industry 1.0: Machinery, Water power, Steam \npower",
		"17th-20th century › Industry 2.0: Electricity, Internal combustion engine, Mass production",
		"21st century › Industry 4.0: Internet, Robotics, Internet of Things",
		"21st century › Industry 5.0: Artificial intelligence",
	}
	if got := timelineShape(d); !reflect.DeepEqual(got, want) {
		t.Errorf("got %q\nwant %q", got, want)
	}
	if len(d.Sections) != 3 || d.Sections[0].Title != "" || d.Sections[1].Span[0] == 0 {
		t.Errorf("sections %+v", d.Sections)
	}
}

// Every period and event carries its source bytes.
func TestTimelineSpans(t *testing.T) {
	src := "timeline\n  2004 : Facebook : Google\n  : Instagram\n"
	d := timelineParse(t, src)
	p := d.Sections[0].Periods[0]
	if got := src[p.Span[0]:p.Span[1]]; got != "2004 : Facebook : Google" {
		t.Errorf("the period's span is %q", got)
	}
	for i, want := range []string{"Facebook", "Google", "Instagram"} {
		e := p.Events[i]
		if got := src[e.Span[0]:e.Span[1]]; got != want {
			t.Errorf("event %d's span is %q, want %q", i, got, want)
		}
	}
}

// An empty timeline, or one with a title alone, parses.
func TestTimelineEmpty(t *testing.T) {
	if d := timelineParse(t, "timeline"); len(d.Sections) != 0 || d.Title != "" {
		t.Errorf("empty: %+v", d)
	}
	if d := timelineParse(t, "timeline\n  title Only a title"); d.Title != "Only a title" || len(d.Sections) != 0 {
		t.Errorf("title only: %+v", d)
	}
}

// What is wrong in the grammar is a SyntaxError at its line; what is outside the subset is
// ErrUnsupported.
func TestTimelineErrors(t *testing.T) {
	for src, line := range map[string]int{
		"timeline\n  : an event first":      2,
		"timeline\n  2002 :":                2,
		"timeline\n  2002 : : LinkedIn":     2,
		"timeline\n  2002 : a\n  :":         3,
		"timeline\n  section\n  2002 : a":   2,
		"timeline\n  title x\n  \"\" : a":   3,
		"timeline\n  section s\n  : orphan": 3,
	} {
		_, err := mermaid.Parse(src)
		var se *mermaid.SyntaxError
		if !errors.As(err, &se) || se.Line != line {
			t.Errorf("%q: %v, want a SyntaxError on line %d", src, err, line)
		}
	}
	for _, src := range []string{
		"timeline LR\n  2002 : a",
		"timeline\n  accDescr {\n  long\n  }",
		"timeline\n  2002 : <b>bold</b>",
		// unsupported anywhere wins over a syntax error elsewhere
		"timeline\n  : orphan\n  2002 : <i>x</i>",
	} {
		if _, err := mermaid.Parse(src); !errors.Is(err, mermaid.ErrUnsupported) {
			t.Errorf("%q: %v, want ErrUnsupported", src, err)
		}
	}
}
