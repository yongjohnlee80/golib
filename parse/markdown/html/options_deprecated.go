package html

// Deprecated declarations: kept so a consumer still builds, NOT maintained, and removed in the next
// minor version. Each names its replacement.

// Unsafe passes raw HTML through as written, and writes every link and image URL.
//
// Deprecated: use WithUnsafe. Not maintained; removed in the next minor version.
func Unsafe() Option { return WithUnsafe() }

// SourceSpans puts each block element's source bytes on it as data-src="start-end".
//
// Deprecated: use WithSourceSpans. Not maintained; removed in the next minor version.
func SourceSpans() Option { return WithSourceSpans() }
