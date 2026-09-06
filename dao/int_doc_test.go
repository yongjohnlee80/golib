package dao

import "testing"

// Asserts the exact renderings quoted in Int's doc comment, so the examples
// cannot drift from the code they document.
func TestInt_DocExamplesRenderAsDocumented(t *testing.T) {
	d := GenericDialect{}
	for _, c := range []struct{ got, want string }{
		{Coalesce(T("track", "plays"), Int(0)).render(d), `COALESCE("track"."plays", 0)`},
		{Coalesce(T("track", "plays"), SQL("0")).render(d), `COALESCE("track"."plays", 0)`},
		{Coalesce(T("label_group", "name"), SQL("''")).render(d), `COALESCE("label_group"."name", '')`},
		{Coalesce(T("label_group", "name"), SQL("'n/a'")).render(d), `COALESCE("label_group"."name", 'n/a')`},
	} {
		if c.got != c.want {
			t.Errorf("got %s, want %s", c.got, c.want)
		}
	}
}
