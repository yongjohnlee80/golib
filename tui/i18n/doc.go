// Package i18n holds the translation catalogs golib/tui shows its text from, and looks a
// message up in them.
//
// A catalog is one language's messages, each named by a stable id, written in Qt's TS XML
// schema: the format Qt Linguist edits, read directly, with no compiled form. Files use the
// .xml extension:
//
//	<TS version="2.1" language="ko_KR">
//	<context>
//	    <name>editor</name>
//	    <message id="editor.menu.file">
//	        <source>&amp;File</source>
//	        <translation>파일(&amp;f)</translation>
//	    </message>
//	</context>
//	</TS>
//
// Lookup is by id alone, as Qt's qsTrId is: a <context> only groups messages for a
// translator. A message whose translation is empty, or marked unfinished, vanished or
// obsolete, is left out, so a lookup falls past it. "&" marks a mnemonic, as everywhere in
// golib/tui.
//
// A [Set] layers catalogs. [Toolkit] returns golib's own (English, Korean, Japanese,
// Simplified Chinese, Brazilian Portuguese and Spanish), and an application adds its own
// over them with [Set.LoadDir]. A later catalog's message replaces an earlier one's, so an
// application can reword a toolkit message, or add a language golib does not ship.
//
// [Set.Lookup] tries the language ("es_MX"), then its base ("es"), then English. A message
// no catalog has is reported as missing; golib/tui then shows the id itself, as Qt does.
//
// The package imports nothing from golib/tui, and uses only the standard library.
package i18n
