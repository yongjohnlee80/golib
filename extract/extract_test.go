package extract

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
)

// fixed is an Extractor that writes its text and reports its info, recording whether it ran.
type fixed struct {
	text   string
	info   Info
	err    error
	ignore bool // keep writing, and return no error, after a failed write
	mu     sync.Mutex
	calls  int
}

func (f *fixed) Extract(_ context.Context, _ io.ReaderAt, _ int64, w io.Writer) (Info, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	for _, line := range strings.SplitAfter(f.text, "\n") {
		if _, err := io.WriteString(w, line); err != nil && !f.ignore {
			return f.info, err
		}
	}
	return f.info, f.err
}

func extractString(t *testing.T, s *Set, name, src string) (string, Info, error) {
	t.Helper()
	var out bytes.Buffer
	info, err := s.Extract(context.Background(), name, strings.NewReader(src), int64(len(src)), &out)
	return out.String(), info, err
}

func TestSet_ChoosesByExtension(t *testing.T) {
	t.Parallel()
	pdf := &fixed{text: "# from pdf\n", info: Info{Title: "Manual", Pages: 3}}
	s := New(Register(Text{}, ".md", "txt"), Register(pdf, ".PDF"))

	for _, tc := range []struct{ name, want string }{
		{"notes/a.md", "body"},
		{"A.TXT", "body"},
		{"spec.pdf", "# from pdf\n"},
		{"dir.v2/spec.Pdf", "# from pdf\n"},
	} {
		got, _, err := extractString(t, s, tc.name, "body")
		if err != nil || got != tc.want {
			t.Errorf("%s: %q, %v; want %q", tc.name, got, err, tc.want)
		}
	}
	if !s.Supports("x.PDF") || !s.Supports("y.txt") || s.Supports("z.docx") || s.Supports("Makefile") {
		t.Error("Supports disagrees with the registrations")
	}
	_, info, _ := extractString(t, s, "spec.pdf", "")
	if info.Title != "Manual" || info.Pages != 3 {
		t.Errorf("info = %+v, want the extractor's", info)
	}
}

func TestSet_LaterRegistrationWins(t *testing.T) {
	t.Parallel()
	s := New(Register(Text{}, ".md"), Register(&fixed{text: "override"}, "md"))
	if got, _, _ := extractString(t, s, "a.md", "body"); got != "override" {
		t.Errorf("got %q, want the later registration's output", got)
	}
}

func TestSet_Unsupported(t *testing.T) {
	t.Parallel()
	s := New(Register(Text{}, ".md"))
	for _, name := range []string{"a.docx", "Makefile", "archive.tar.gz", ""} {
		_, _, err := extractString(t, s, name, "x")
		if !errors.Is(err, ErrUnsupported) || !errors.Is(err, errs.ErrUnsupported) {
			t.Errorf("%q: err = %v, want ErrUnsupported", name, err)
		}
	}
}

func TestSet_TitleFallsBackToTheFileName(t *testing.T) {
	t.Parallel()
	s := New(Register(Text{}, ".md"))
	_, info, err := extractString(t, s, "docs/Release Notes.md", "x")
	if err != nil || info.Title != "Release Notes" {
		t.Errorf("title = %q, %v; want the base name without its extension", info.Title, err)
	}
}

// The container limit is checked before the extractor runs: an oversized file is never read.
func TestSet_ContainerLimit(t *testing.T) {
	t.Parallel()
	e := &fixed{text: "x"}
	s := New(Register(e, ".pdf"), MaxContainer(10))
	if _, _, err := extractString(t, s, "big.pdf", strings.Repeat("a", 11)); !errors.Is(err, ErrContainerTooLarge) || !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("err = %v, want ErrContainerTooLarge", err)
	}
	if e.calls != 0 {
		t.Errorf("the extractor ran %d times for a file over the limit", e.calls)
	}
	if _, _, err := extractString(t, s, "fits.pdf", strings.Repeat("a", 10)); err != nil {
		t.Errorf("a file at the limit: %v", err)
	}
}

func TestSet_TextLimit(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		e    *fixed
		max  int64
		want string
		err  bool
	}{
		"under":                  {&fixed{text: "abc\n"}, 10, "abc\n", false},
		"exactly at":             {&fixed{text: "abc\n"}, 4, "abc\n", false},
		"over":                   {&fixed{text: "abc\ndef\n"}, 6, "abc\nde", true},
		"over, error ignored":    {&fixed{text: "abc\ndef\nghi\n", ignore: true}, 6, "abc\nde", true},
		"over, extractor errors": {&fixed{text: "abc\ndef\n", err: errors.New("parse failed")}, 6, "abc\nde", true},
		"no limit":               {&fixed{text: strings.Repeat("x", 1<<16)}, 0, strings.Repeat("x", 1<<16), false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := New(Register(tc.e, ".pdf"), MaxText(tc.max))
			got, _, err := extractString(t, s, "a.pdf", "")
			if got != tc.want {
				t.Errorf("wrote %q, want %q", got, tc.want)
			}
			if tc.err != errors.Is(err, ErrTextTooLarge) {
				t.Errorf("err = %v, want ErrTextTooLarge %v", err, tc.err)
			}
		})
	}
}

type failingWriter struct{ err error }

func (f failingWriter) Write([]byte) (int, error) { return 0, f.err }

func TestSet_ExtractorErrorsAreWrapped(t *testing.T) {
	t.Parallel()
	parse := errors.New("bad xref table")
	s := New(Register(&fixed{err: parse}, ".pdf"))
	_, _, err := extractString(t, s, "broken.pdf", "")
	if !errors.Is(err, parse) || !strings.Contains(err.Error(), "broken.pdf") {
		t.Errorf("err = %v, want the extractor's error naming the file", err)
	}
}

func TestSet_CancelledBeforeStart(t *testing.T) {
	t.Parallel()
	e := &fixed{text: "x"}
	s := New(Register(e, ".pdf"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Extract(ctx, "a.pdf", strings.NewReader(""), 0, io.Discard); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if e.calls != 0 {
		t.Error("the extractor ran after cancellation")
	}
}

func TestNew_RefusesMisconfiguration(t *testing.T) {
	t.Parallel()
	for name, opts := range map[string][]Option{
		"nil extractor":      {Register(nil, ".md")},
		"no extensions":      {Register(Text{})},
		"empty extension":    {Register(Text{}, "")},
		"dot only":           {Register(Text{}, ".")},
		"negative container": {MaxContainer(-1)},
		"negative text":      {MaxText(-1)},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: New accepted it", name)
				}
			}()
			New(opts...)
		}()
	}
	New(nil, Register(Text{}, "md")) // a nil option is skipped
}

func TestFunc(t *testing.T) {
	t.Parallel()
	f := Func(func(_ context.Context, _ io.ReaderAt, size int64, w io.Writer) (Info, error) {
		_, err := io.WriteString(w, "size ok")
		return Info{Pages: int(size)}, err
	})
	got, info, err := extractString(t, New(Register(f, ".x")), "a.x", "12345")
	if err != nil || got != "size ok" || info.Pages != 5 {
		t.Errorf("%q %+v %v", got, info, err)
	}
}

// A Set is safe for concurrent use: many extractions through one Set at once.
func TestSet_ConcurrentExtractions(t *testing.T) {
	t.Parallel()
	s := New(Register(Text{}, ".md"), MaxText(1<<20))
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			src := strings.Repeat(string(rune('a'+i)), 100_000+i)
			var out bytes.Buffer
			if _, err := s.Extract(context.Background(), "a.md", strings.NewReader(src), int64(len(src)), &out); err != nil || out.String() != src {
				t.Errorf("extraction %d: %v, %d bytes", i, err, out.Len())
			}
		})
	}
	wg.Wait()
}

// A destination that fails is reported through the Set, whether or not the text limit was also
// crossed, and even when the extractor ignored the failed write.
func TestSet_DestinationFailureIsReported(t *testing.T) {
	t.Parallel()
	diskFull := errors.New("disk full")
	for name, tc := range map[string]struct {
		e       *fixed
		max     int64
		limited bool
	}{
		"at the limit":                  {&fixed{text: "abc"}, 2, true},
		"at the limit, error ignored":   {&fixed{text: "abc", ignore: true}, 2, true},
		"under no limit":                {&fixed{text: "abc"}, 0, false},
		"under no limit, error ignored": {&fixed{text: "abc", ignore: true}, 0, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := New(Register(tc.e, ".pdf"), MaxText(tc.max))
			_, err := s.Extract(context.Background(), "a.pdf", strings.NewReader(""), 0, failingWriter{diskFull})
			if !errors.Is(err, diskFull) {
				t.Fatalf("err = %v, want the destination's failure", err)
			}
			if strings.Contains(err.Error(), "%!") {
				t.Errorf("err = %q, a message with a formatting fault", err)
			}
			if errors.Is(err, ErrTextTooLarge) != tc.limited {
				t.Errorf("err = %v, want ErrTextTooLarge %v", err, tc.limited)
			}
		})
	}
}

// A Func that is nil is refused where it is registered, not when a file arrives.
func TestNew_RefusesANilFunc(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("New accepted a nil Func")
		}
	}()
	var f Func
	New(Register(f, ".x"))
}

// An extractor that ignores a failed write and then fails for a reason of its own: both errors reach
// the caller.
func TestSet_DestinationFailureAndExtractorError(t *testing.T) {
	t.Parallel()
	diskFull, parse := errors.New("disk full"), errors.New("bad xref table")
	s := New(Register(&fixed{text: "abc", ignore: true, err: parse}, ".pdf"))
	_, err := s.Extract(context.Background(), "a.pdf", strings.NewReader(""), 0, failingWriter{diskFull})
	if !errors.Is(err, diskFull) || !errors.Is(err, parse) {
		t.Errorf("err = %v, want both the destination's and the extractor's errors", err)
	}
}

// A negative size is a caller's mistake, not an empty file: refused before anything is read.
func TestSet_NegativeSize(t *testing.T) {
	t.Parallel()
	e := &fixed{text: "x"}
	s := New(Register(e, ".pdf"))
	if _, err := s.Extract(context.Background(), "a.pdf", strings.NewReader(""), -1, io.Discard); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("err = %v, want errs.ErrInvalidArgument", err)
	}
	if e.calls != 0 {
		t.Error("the extractor ran for a negative size")
	}
}
