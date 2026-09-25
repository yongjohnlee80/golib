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

// TestARoleAWidgetDoesNotTakeIsRefused: an accent on a status bar would be a
// colour silently dropped, so it is refused rather than accepted and ignored.
func TestARoleAWidgetDoesNotTakeIsRefused(t *testing.T) {
	for _, src := range []string{
		`StatusBar { palette.accent: "red" }`,
		`StatusBar { palette.base: "blue" }`,
		"Frame { palette.text: \"red\"\n Text { } }",
		`Editor { palette.window: "blue" }`,
		`StatusBar { palette.windw: "blue" }`,
	} {
		_, err := mountDoc(t, src)
		if err == nil {
			t.Errorf("accepted: %s", src)
			continue
		}
		if !strings.Contains(err.Error(), "palette.") {
			t.Errorf("%s: refused for another reason: %v", src, err)
		}
	}
}
