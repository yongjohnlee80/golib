// Package extract turns document containers (PDF, DOCX, plain text) into Markdown, streaming, so that
// a search index or an embedding model reads text rather than files.
//
// An [Extractor] reads one container through an io.ReaderAt and writes its Markdown to an io.Writer,
// so neither the file nor its text is held in memory whole. A [Set] chooses the extractor by file
// extension and enforces two limits: one on the file, [MaxContainer], checked before anything is
// read, and one on the text that comes out, [MaxText].
//
//	set := extract.New(
//		extract.Register(extract.Text{}, ".md", ".markdown", ".txt"),
//		extract.Register(pdfExtractor, ".pdf"), // an Extractor of the caller's own
//		extract.MaxContainer(250<<20),
//		extract.MaxText(16<<20),
//	)
//	f, _ := os.Open(path)
//	st, _ := f.Stat()
//	info, err := set.Extract(ctx, path, f, st.Size(), w)
//
// This package defines the interface and ships only what the standard library can do. A format whose
// parser needs a third-party module, such as PDF, is an Extractor in the product that accepts that
// dependency, registered with [Register]; golib itself stays free of it.
package extract
