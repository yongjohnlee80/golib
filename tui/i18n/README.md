# tui/i18n

The translation catalogs `golib/tui` shows its text from. Catalogs are written in Qt's TS
XML schema, the format Qt Linguist edits, and read directly: no compiled `.qm` form, no
build step. Standard library only; imports no other tui package.

```bash
go get github.com/yongjohnlee80/golib/tui/i18n
```

```go
import "github.com/yongjohnlee80/golib/tui/i18n"
```

## A catalog

One language's messages, each named by a stable id. Files use the `.xml` extension and are
named `<prefix>_<language>.xml`:

```xml
<!-- i18n/editor_ko_KR.xml -->
<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE TS>
<TS version="2.1" language="ko_KR">
<context>
    <name>editor</name>
    <message id="editor.menu.file">
        <source>&amp;File</source>
        <translation>파일(&amp;f)</translation>
    </message>
    <message id="editor.option.language">
        <source>&amp;Language</source>
        <translation type="unfinished"></translation>
    </message>
</context>
</TS>
```

- Lookup is by `id` alone, as Qt's `qsTrId` is. `<context>` only groups messages for a
  translator; `<source>`, `<comment>` and `<location>` are read past.
- A translation that is empty, or marked `unfinished`, `vanished` or `obsolete`, is left out,
  so the lookup falls back past it.
- `&` marks the mnemonic (written `&amp;` in XML). `&&` is a literal `&`.
- Plural messages (`numerus="yes"`) are refused with `errs.ErrUnsupported`; every other
  refusal carries `errs.ErrInvalidArgument` and names the file and line.

## Sets, layering and fallback

```go
s := i18n.Toolkit()                                   // golib's own: "tui.…" ids
if err := s.LoadDir(files, "i18n", "editor"); err != nil { // editor_<lang>.xml
    return err
}
text, ok := s.Lookup("ko_KR", "editor.menu.file")     // "파일(&f)", true
```

- `Toolkit` returns golib/tui's catalogs in English, Korean (`ko_KR`), Japanese (`ja_JP`),
  Simplified Chinese (`zh_CN`), Brazilian Portuguese (`pt_BR`) and Spanish (`es`). Each call
  is a new Set.
- A catalog added later replaces the same ids from earlier ones in its language. An
  application rewords a toolkit message by carrying its id, and adds a language golib does
  not ship with one more file, including the `tui.…` ids to translate golib's own labels.
- `Lookup` tries the language (`es_MX`), then its base (`es`), then English. `-` and `_` are
  one separator: `pt-BR` is `pt_BR`.
- A message no catalog holds is reported missing (`ok == false`); golib/tui then shows the
  id itself, as Qt does.
- Fill a Set before it is used: `Add` and `LoadDir` must not run while another goroutine
  calls `Lookup`.

## How this differs from Qt

- Ids only: there is no `qsTr` (source text as the key) and no disambiguation.
- The TS XML is the runtime format. There is no `lrelease`, and no `.qm`.
- No plurals yet: a `%n` message needs a language's plural rule, and none is built in.
- Files end in `.xml`, not `.ts`, which tools read as TypeScript. Qt Linguist opens a copy
  renamed to `.ts`.

## Files

| file          | holds                                                     |
| ------------- | --------------------------------------------------------- |
| `catalog.go`  | `Catalog`, `ParseTS`                                      |
| `set.go`      | `Set`, `Add`, `LoadDir`, `Lookup`, `English`              |
| `toolkit.go`  | `Toolkit`: golib's embedded catalogs (`catalogs/tui_*.xml`) |

## License

[Apache-2.0](../../LICENSE)
