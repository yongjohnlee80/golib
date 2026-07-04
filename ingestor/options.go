package ingestor

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

// config holds the file-writer settings shared by the CSV and JSON ingestors.
type config struct {
	dir    string
	opener func(name string) (io.WriteCloser, error)
}

// Option configures a file-writing ingestor (CSV, JSON).
type Option func(*config)

// WithDir sets the directory batch files are written into. The default is the
// current working directory. The directory must exist.
func WithDir(dir string) Option {
	return func(c *config) { c.dir = dir }
}

// WithOpener replaces the file-creation function entirely — the ingestor
// writes each batch to the io.WriteCloser returned for the batch's file name.
// Use it to redirect output to in-memory buffers (tests), sockets, or object
// storage. When set, WithDir is ignored.
func WithOpener(fn func(name string) (io.WriteCloser, error)) Option {
	return func(c *config) { c.opener = fn }
}

func newConfig(opts []Option) config {
	c := config{dir: "."}
	for _, opt := range opts {
		opt(&c)
	}
	return c
}

// open creates the write target for one batch file.
func (c config) open(name string) (io.WriteCloser, error) {
	if c.opener != nil {
		return c.opener(name)
	}
	return os.Create(filepath.Join(c.dir, name))
}

// sanitizeDescription makes a description safe for use in a file name.
func sanitizeDescription(desc string) string {
	r := strings.NewReplacer("/", "-", "\\", "-", " ", "-")
	return r.Replace(desc)
}
