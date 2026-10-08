package mermaid_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/yongjohnlee80/golib/parse/mermaid"
)

func journeyParse(t *testing.T, src string) *mermaid.JourneyDiagram {
	t.Helper()
	d, err := mermaid.Parse(src)
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	j, ok := d.(*mermaid.JourneyDiagram)
	if !ok || d.Kind() != mermaid.Journey {
		t.Fatalf("%q parsed as %T", src, d)
	}
	return j
}

// Mermaid's documented example: the title, two sections in order, each task's score and actors,
// and the actors in order of first mention.
func TestJourneyTheDocumentedExample(t *testing.T) {
	j := journeyParse(t, `journey
    title My working day
    section Go to work
      Make tea: 5: Me
      Go upstairs: 3: Me
      Do work: 1: Me, Cat
    section Go home
      Go downstairs: 5: Me
      Sit down: 5: Me`)
	if j.Title != "My working day" {
		t.Errorf("title %q", j.Title)
	}
	if !reflect.DeepEqual(j.Actors, []string{"Me", "Cat"}) {
		t.Errorf("actors %q", j.Actors)
	}
	if len(j.Sections) != 2 || j.Sections[0].Name != "Go to work" || j.Sections[1].Name != "Go home" {
		t.Fatalf("sections %+v", j.Sections)
	}
	got := j.Sections[0].Tasks
	want := []mermaid.JourneyTask{{Name: "Make tea", Score: 5, Actors: []string{"Me"}}, {Name: "Go upstairs", Score: 3, Actors: []string{"Me"}},
		{Name: "Do work", Score: 1, Actors: []string{"Me", "Cat"}}}
	if len(got) != len(want) {
		t.Fatalf("tasks %+v", got)
	}
	for i, w := range want {
		g := got[i]
		if g.Name != w.Name || g.Score != w.Score || !reflect.DeepEqual(g.Actors, w.Actors) || g.Span[1] <= g.Span[0] {
			t.Errorf("task %d: %+v, want %+v", i, g, w)
		}
	}
	if len(j.Sections[1].Tasks) != 2 {
		t.Errorf("Go home's tasks %+v", j.Sections[1].Tasks)
	}
}

// A task before any section is in a section with no name; a task needs no actors; the
// accessibility lines and comments draw nothing.
func TestJourneyTasksOutsideASection(t *testing.T) {
	j := journeyParse(t, "journey\n  accTitle: a day\n  %% a comment\n  Wake up: 2\n  section Later\n  Rest: 4: Me")
	if len(j.Sections) != 2 || j.Sections[0].Name != "" || j.Sections[0].Tasks[0].Name != "Wake up" ||
		j.Sections[0].Tasks[0].Score != 2 || j.Sections[0].Tasks[0].Actors != nil {
		t.Fatalf("sections %+v", j.Sections)
	}
	if e := journeyParse(t, "journey"); e.Title != "" || len(e.Sections) != 0 || len(e.Actors) != 0 {
		t.Errorf("an empty journey: %+v", e)
	}
}

// A malformed task is a SyntaxError on its line; what golib does not draw is ErrUnsupported.
func TestJourneyErrors(t *testing.T) {
	for src, line := range map[string]int{
		"journey\n  section A\n  Make tea: 6: Me": 3,
		"journey\n  Make tea: 0: Me":              2,
		"journey\n  Make tea: five: Me":           2,
		"journey\n  Make tea":                     2,
		"journey\n  Make tea: 5: Me: Cat":         2,
		"journey\n  : 5: Me":                      2,
		"journey\n  Make tea: 5: Me,":             2,
		"journey\n  section":                      2,
		"journey LR\n  Make tea: 5":               1,
	} {
		_, err := mermaid.Parse(src)
		var se *mermaid.SyntaxError
		if !errors.As(err, &se) || se.Line != line {
			t.Errorf("%q: %v, want a SyntaxError on line %d", src, err, line)
		}
	}
	for _, src := range []string{
		"journey\n  accDescr {\n  long\n  }",
		"journey\n  <b>Make tea</b>: 5: Me",
	} {
		if _, err := mermaid.Parse(src); !errors.Is(err, mermaid.ErrUnsupported) {
			t.Errorf("%q: %v, want ErrUnsupported", src, err)
		}
	}
}
