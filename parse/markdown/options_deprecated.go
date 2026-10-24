package markdown

// Deprecated declarations: kept so a consumer still builds, NOT maintained, and removed in the next
// minor version. Each names its replacement.

// GFM enables GitHub Flavored Markdown.
//
// Deprecated: use WithGFM. Not maintained; removed in the next minor version.
func GFM() Option { return WithGFM() }

// Obsidian enables the Obsidian syntax.
//
// Deprecated: use WithObsidian. Not maintained; removed in the next minor version.
func Obsidian() Option { return WithObsidian() }
