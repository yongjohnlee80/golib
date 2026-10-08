package code

import "github.com/yongjohnlee80/golib/search"

// Extensions maps each file extension this package chunks to its chunker.
func Extensions() map[string]search.Chunker {
	return map[string]search.Chunker{
		".go":  Go{},
		".ts":  TypeScript{},
		".tsx": TypeScript{},
		".js":  TypeScript{},
		".jsx": TypeScript{},
		".mjs": TypeScript{},
		".py":  Python{},
		".rs":  Rust{},
	}
}

// Register makes this package's chunkers those of their extensions in r, which the caller builds:
// there is no global registry. An extension r already has a chunker for is refused
// (search.ErrChunkerTaken), and nothing is registered after the first refusal.
func Register(r *search.Chunkers) error {
	for _, ext := range []string{".go", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".py", ".rs"} {
		if err := r.Register(ext, Extensions()[ext]); err != nil {
			return err
		}
	}
	return nil
}
