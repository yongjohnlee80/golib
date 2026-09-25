package decl_test

import (
	"strings"
	"testing"
)

// palette_test.go covers how a document's `palette.<role>` values are READ.
// What they look like on screen is the editor example's acceptance tests; the
// refusals are here, where a wrong colour has to be reported and not painted
// as something else.

func TestAColourIsReadInEverySpellingATheme(t *testing.T) {
	for _, c := range []string{
		"blue", "BLUE", " white ", "brightyellow", "BrightWhite",
		"gray", "grey", "#1e90ff", "#ABCDEF", "default",
	} {
		src := "StatusBar { palette.window: \"" + c + "\" }"
		if _, err := mountDoc(t, src); err != nil {
			t.Errorf("%q was refused: %v", c, err)
		}
	}
}

func TestAWrongColourIsRefusedAndSaysWhatIsAccepted(t *testing.T) {
	for _, c := range []struct{ value, want string }{
		{`"bleu"`, "is not a colour"},
		{`"brightbleu"`, "is not a colour"},
		{`"#12345"`, "#rrggbb"},
		{`"#1234567"`, "#rrggbb"},
		{`"#gggggg"`, "#rrggbb"},
		{`""`, "is not a colour"},
		{`4`, "want a string"},
	} {
		_, err := mountDoc(t, "StatusBar { palette.window: "+c.value+" }")
		if err == nil {
			t.Errorf("%s was accepted", c.value)
			continue
		}
		if !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "palette.window") {
			t.Errorf("%s: diagnostic = %q, want %q and the property's name", c.value, err, c.want)
		}
	}
}

// TestARoleIsAcceptedOnEveryTypeAndAMisspeltOneRefused: `palette` is on every
// Qt Item, and a role a widget does not wear still reaches its children — an
// accent on a Flex dresses the menus inside it (ADR-tui-0014 D2). What IS
// refused is a role that does not exist, by name.
func TestARoleIsAcceptedOnEveryTypeAndAMisspeltOneRefused(t *testing.T) {
	for _, src := range []string{
		`StatusBar { palette.accent: "red" }`,
		"Flex { palette.window: \"blue\"\n Text { } }",
		"Frame { palette.text: \"red\"\n Text { } }",
	} {
		if _, err := mountDoc(t, src); err != nil {
			t.Errorf("refused %s: %v", src, err)
		}
	}
	for _, src := range []string{`StatusBar { palette.windw: "blue" }`, `Text { palette.inactive.window: "red" }`} {
		_, err := mountDoc(t, src)
		if err == nil || !strings.Contains(err.Error(), "is not a palette role") {
			t.Errorf("%s: err = %v, want the role refused by name", src, err)
		}
	}
	if _, err := mountDoc(t, `Text { palette.window: "nope" }`); err == nil ||
		!strings.Contains(err.Error(), "palette.window") {
		t.Errorf("a bad colour: err = %v", err)
	}
}
