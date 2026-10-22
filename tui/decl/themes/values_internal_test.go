package themes

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
)

// values reads a theme as Validate does, and refuses what it refuses, naming every problem: not a
// Theme, not a string, and also an unknown key or a colour the toolkit cannot read, which values
// once took. The shipped themes never reach these; a copy a program edits can.
func TestValuesRefusesWhatValidateRefuses(t *testing.T) {
	dark, err := fs.ReadFile(files, "dark.qml")
	if err != nil {
		t.Fatal(err)
	}
	src := string(dark)
	end := strings.LastIndex(src, "}")
	colour := regexp.MustCompile(`"[^"]+"`)
	fsys := fstest.MapFS{
		"broken.qml":  {Data: []byte("Theme { app { window: ")},
		"flex.qml":    {Data: []byte(`Flex { app { window: "white" } }`)},
		"number.qml":  {Data: []byte(strings.Replace(src, colour.FindString(src), "4", 1))},
		"unknown.qml": {Data: []byte(src[:end] + "\n    app.notAColourKey: \"white\"\n}\n")},
		"badtone.qml": {Data: []byte(strings.Replace(src, colour.FindString(src), `"notacolour"`, 1))},
		"good.qml":    {Data: dark},
	}
	for name, want := range map[string]string{
		"broken":  "themes: ",
		"flex":    "root must be Theme",
		"number":  "colour must be a quoted string constant",
		"unknown": "app.notAColourKey: unknown colour key",
		"badtone": "notacolour",
	} {
		if _, err := values(fsys, name); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: error = %v, want %q", name, err, want)
		}
	}
	v, err := values(fsys, "good")
	if err != nil || len(v) == 0 {
		t.Errorf("the shipped dark theme = %d values, %v", len(v), err)
	}
}

// A theme with no words of its own, one added upstream before its label, shows as its name and is
// still listed, after the first five.
func TestAThemeWithoutWordsShowsAsItsName(t *testing.T) {
	labels := menuLabels([]string{"newtheme", "sepia", "dark"})
	if len(labels) != 3 || labels[0].Name != "dark" || labels[1].Name != "sepia" {
		t.Fatalf("labels %+v; want dark and sepia first", labels)
	}
	if last := labels[2]; last.Name != "newtheme" || last.Text != "newtheme" {
		t.Fatalf("the unlabelled theme: %+v; want its name as its words", last)
	}
}
