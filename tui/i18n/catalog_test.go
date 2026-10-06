package i18n

import (
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
)

func ts(lang, body string) string {
	return `<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE TS>
<TS version="2.1" language="` + lang + `">
<context>
    <name>app</name>
` + body + `
</context>
</TS>`
}

func TestParseTS_KeepsTranslationsByID(t *testing.T) {
	t.Parallel()
	c, err := ParseTS(strings.NewReader(ts("ko_KR", `
    <message id="app.file">
        <source>&amp;File</source>
        <comment>the File menu</comment>
        <location filename="editor.qml" line="12"/>
        <translation>파일(&amp;f)</translation>
    </message>
    <message id="app.quit">
        <source>Are you sure to quit?
Unsaved changes will be lost.</source>
        <translation>정말 끝내시겠습니까?
저장하지 않은 변경 사항은 사라집니다.</translation>
    </message>`)))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Language(); got != "ko_KR" {
		t.Errorf("Language() = %q, want ko_KR", got)
	}
	want := map[string]string{
		"app.file": "파일(&f)",
		"app.quit": "정말 끝내시겠습니까?\n저장하지 않은 변경 사항은 사라집니다.",
	}
	if len(c.texts) != len(want) {
		t.Fatalf("kept %d messages, want %d: %v", len(c.texts), len(want), c.texts)
	}
	for id, w := range want {
		if got := c.texts[id]; got != w {
			t.Errorf("%s = %q, want %q", id, got, w)
		}
	}
}

func TestParseTS_LeavesOutWhatIsNotTranslated(t *testing.T) {
	t.Parallel()
	c, err := ParseTS(strings.NewReader(ts("es", `
    <message id="a"><source>A</source><translation type="unfinished">borrador</translation></message>
    <message id="b"><source>B</source><translation type="vanished">viejo</translation></message>
    <message id="c"><source>C</source><translation type="obsolete">viejo</translation></message>
    <message id="d"><source>D</source><translation></translation></message>
    <message id="e"><source>E</source></message>
    <message id="f"><source>F</source><translation>Efe</translation></message>`)))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.texts) != 1 || c.texts["f"] != "Efe" {
		t.Fatalf("kept %v, want only f=Efe: unfinished, vanished, obsolete, empty and missing translations must fall back", c.texts)
	}
}

func TestParseTS_WritesTheLanguageOneWay(t *testing.T) {
	t.Parallel()
	c, err := ParseTS(strings.NewReader(ts("pt-BR", "")))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Language(); got != "pt_BR" {
		t.Errorf("Language() = %q, want pt_BR: pt-BR and Qt's pt_BR must be one language", got)
	}
}

func TestParseTS_Refuses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src string
		want      error
		mention   string
	}{
		{"a message with no id", ts("en", `<message><source>x</source><translation>x</translation></message>`),
			errs.ErrInvalidArgument, "has no id"},
		{"an id written twice", ts("en", `
    <message id="a"><translation>one</translation></message>
    <message id="a"><translation>two</translation></message>`), errs.ErrInvalidArgument, `"a"`},
		{"a plural message", ts("en", `<message id="n" numerus="yes"><translation><numerusform>%n file</numerusform></translation></message>`),
			errs.ErrUnsupported, `"n"`},
		{"a translation holding an element", ts("en", `<message id="a"><translation>x<b>y</b></translation></message>`),
			errs.ErrInvalidArgument, `"a"`},
		{"a message cut off", `<TS language="en"><context><message id="a">`, errs.ErrInvalidArgument, `"a" does not end`},
		{"XML that is not well-formed", `<TS language="en"><context></TS>`, errs.ErrInvalidArgument, "not well-formed"},
		{"a file with no TS element", `<?xml version="1.0"?><other/>`, errs.ErrInvalidArgument, "no <TS>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseTS(strings.NewReader(tc.src))
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if !strings.Contains(err.Error(), tc.mention) {
				t.Errorf("err = %q, want it to mention %s", err, tc.mention)
			}
		})
	}
}

func TestParseTS_APluralRefusalIsUnsupportedOnly(t *testing.T) {
	t.Parallel()
	_, err := ParseTS(strings.NewReader(ts("en", `<message id="n" numerus="yes"><translation/></message>`)))
	if errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("err = %v: a plural message is a capability this package lacks, not a bad argument", err)
	}
}
