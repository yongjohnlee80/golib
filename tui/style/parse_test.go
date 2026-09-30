package style

import (
	"strings"
	"testing"
)

// TestParseColorReadsTheThemesVocabulary: names are ANSI slots, bright the
// slot eight above, gray bright black, #rrggbb truecolour, default the
// terminal's own; case and surrounding space aside.
func TestParseColorReadsTheThemesVocabulary(t *testing.T) {
	for _, c := range []struct {
		in   string
		want Color
	}{
		{"blue", ANSI(4)}, {"BLUE", ANSI(4)}, {" white ", ANSI(7)},
		{"brightyellow", ANSI(11)}, {"BrightWhite", ANSI(15)}, {"black", ANSI(0)},
		{"gray", ANSI(8)}, {"grey", ANSI(8)},
		{"#1e90ff", RGB(0x1e, 0x90, 0xff)}, {"#ABCDEF", RGB(0xab, 0xcd, 0xef)},
		{"default", Default()},
	} {
		got, err := ParseColor(c.in)
		if err != nil || got != c.want {
			t.Errorf("ParseColor(%q) = %v, %v; want %v", c.in, got, err, c.want)
		}
	}
}

// TestParseColorRefusesAndSaysWhatIsAccepted: a wrong colour names what is.
func TestParseColorRefusesAndSaysWhatIsAccepted(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"bleu", "is not a colour: want one of black, red, green, yellow, blue, magenta, cyan, white"},
		{"brightbleu", "is not a colour"},
		{"", "is not a colour"},
		{"#12345", "#rrggbb"}, {"#1234567", "#rrggbb"}, {"#gggggg", "#rrggbb"},
	} {
		_, err := ParseColor(c.in)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("ParseColor(%q) error = %v, want %q", c.in, err, c.want)
		}
	}
}
