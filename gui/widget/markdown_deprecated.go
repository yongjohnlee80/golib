package widget

// Deprecated declarations: kept so a consumer still builds, NOT maintained, and removed in the next
// minor version. Each names its replacement.

// HeadingScale is h1's size as a multiple of the text's.
//
// Deprecated: use WithHeadingScale. Not maintained; removed in the next minor version.
func HeadingScale(h1 float32) MarkdownOption { return WithHeadingScale(h1) }

// HideMetadata omits closed frontmatter and HTML comments from the reading view.
//
// Deprecated: use WithHideMetadata. Not maintained; removed in the next minor version.
func HideMetadata() MarkdownOption { return WithHideMetadata() }
