// Package docx extracts Markdown from Word documents (.docx) as a golib extract.Extractor, with the
// standard library alone (archive/zip, encoding/xml).
//
// It opens the archive through its io.ReaderAt (a ZIP's directory sits at its end), reads the
// styles and numbering first, then the document one XML token at a time, holding one paragraph or
// one table row. Every archive entry is read through a counting limit, per entry and in total,
// because the uncompressed size a ZIP header states can lie: a small archive that inflates to
// gigabytes stops at the limit. XML depth and a block's size are bounded too.
package docx
