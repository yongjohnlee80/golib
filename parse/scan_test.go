package parse

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"unicode/utf8"
	"unsafe"

	"github.com/yongjohnlee80/golib/streamcache"
)

// sqlish is a realistic form list: delimited constructs first, then the runs and
// operator sets, then a ByteForm so nothing is left unclaimed. `/*` before `/` is
// the precedence that makes the shared-prefix cases interesting.
func sqlish() []Form {
	word := func(i int, b byte) bool {
		letter := b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
		if i == 0 {
			return letter
		}
		return letter || (b >= '0' && b <= '9')
	}
	space := func(_ int, b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' }
	digit := func(_ int, b byte) bool { return b >= '0' && b <= '9' }

	return []Form{
		BlockComment("/*", "*/", true),
		LineComment("--"),
		QuoteForm("'", "'", QuoteOpts{Doubling: true}),
		RunForm(Space, space),
		RunForm(Word, word),
		RunForm(Number, digit),
		SetForm(Operator, "<=", ">=", "<>", "<", ">", "=", "/", "-", "+", "*"),
		SetForm(Terminator, ";"),
		ByteForm(Punct),
	}
}

// oneByteAtATime is the reader that makes every chunk boundary land inside a
// construct, which is where a form that guessed shows up.
type oneByteAtATime struct {
	b []byte
	i int
}

func (r *oneByteAtATime) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	if len(p) == 0 {
		return 0, nil
	}
	p[0] = r.b[r.i]
	r.i++
	return 1, nil
}

// collect drains a scan, acquiring each token's bytes AS IT ARRIVES — which is
// the documented discipline, since the scan releases retention behind itself.
func collect(t *testing.T, s *Scan) ([]Token, string) {
	t.Helper()
	var toks []Token
	var text []byte
	for tok, err := range s.Tokens() {
		if err != nil {
			t.Fatalf("token stream error: %v", err)
		}
		toks = append(toks, tok)
		if tok.Len() > 0 {
			v, err := s.Acquire(tok)
			if err != nil {
				t.Fatalf("Acquire(%v): %v", tok, err)
			}
			text, err = v.AppendTo(text)
			v.Close()
			if err != nil {
				t.Fatalf("AppendTo(%v): %v", tok, err)
			}
		}
	}
	return toks, string(text)
}

const corpus = "select a1, 'it''s' /* c /* n */ */ from t -- tail\nwhere x <= 3;\n"

// A source supplied as []byte and as a one-byte-at-a-time reader yields
// IDENTICAL tokens; concatenating every token's bytes, trivia included,
// reproduces the source exactly; and the spans are
// disjoint and ordered.
func TestScan_ByteAndStreamAgreeAndReproduceTheSource(t *testing.T) {
	lex := New(WithForms(sqlish()...))

	sb := lex.ScanBytes(context.Background(), []byte(corpus))
	defer sb.Close()
	byteToks, byteText := collect(t, sb)

	sr := lex.Scan(context.Background(), &oneByteAtATime{b: []byte(corpus)}, BorrowReader)
	defer sr.Close()
	streamToks, streamText := collect(t, sr)

	if len(byteToks) != len(streamToks) {
		t.Fatalf("token counts differ: []byte %d, stream %d", len(byteToks), len(streamToks))
	}
	for i := range byteToks {
		if byteToks[i] != streamToks[i] {
			t.Errorf("token %d differs: []byte %v, stream %v", i, byteToks[i], streamToks[i])
		}
	}
	if byteText != corpus {
		t.Errorf("concatenation != source:\n got %q\nwant %q", byteText, corpus)
	}
	if streamText != corpus {
		t.Errorf("stream concatenation != source:\n got %q\nwant %q", streamText, corpus)
	}

	// Disjoint, ordered, gapless — a tree over these cannot overlap.
	var at int64
	for i, tk := range byteToks {
		if tk.Start != at {
			t.Fatalf("token %d (%v) starts at %d, want %d — the spans must be gapless", i, tk, tk.Start, at)
		}
		if tk.End < tk.Start {
			t.Fatalf("token %d (%v) is inverted", i, tk)
		}
		at = tk.End
	}
	if last := byteToks[len(byteToks)-1]; last.Kind != EOF || last.Start != int64(len(corpus)) {
		t.Errorf("last token = %v, want an EOF token at offset %d", last, len(corpus))
	}
}

// TestScan_TriviaIsEmitted: comments and whitespace are tokens like any other,
// because a layer that cannot undo a decision does not get to make it.
func TestScan_TriviaIsEmitted(t *testing.T) {
	lex := New(WithForms(sqlish()...))
	s := lex.ScanBytes(context.Background(), []byte("a /*c*/ -- t\nb"))
	defer s.Close()
	toks, _ := collect(t, s)

	var kinds []string
	for _, tk := range toks {
		kinds = append(kinds, tk.Kind.String())
	}
	got := strings.Join(kinds, " ")
	want := "Word Space Comment Space Comment Space Word EOF"
	if got != want {
		t.Errorf("kinds = %q, want %q", got, want)
	}
}

// A source ending in a shared prefix lexes as the SHORTER form —
// `/` with `/*` declared first is an operator, not an unterminated block
// comment. Driven at EOF and with the same bytes mid-stream, which must differ:
// mid-stream it waits, and then resolves on what actually arrives.
func TestScan_SourceEndingInASharedPrefixLexesAsTheShorterForm(t *testing.T) {
	lex := New(WithForms(sqlish()...))

	s := lex.ScanBytes(context.Background(), []byte("a/"))
	defer s.Close()
	toks, text := collect(t, s)
	if len(toks) != 3 || toks[1].Kind != Operator || toks[1].Len() != 1 {
		t.Fatalf("tokens = %v, want Word Operator EOF with a one-byte operator", toks)
	}
	if text != "a/" {
		t.Errorf("text = %q, want %q", text, "a/")
	}

	// The same bytes mid-stream, where more DOES arrive, become a comment.
	s2 := lex.ScanBytes(context.Background(), []byte("a/*c*/"))
	defer s2.Close()
	toks2, _ := collect(t, s2)
	if len(toks2) != 3 || toks2[1].Kind != Comment {
		t.Fatalf("tokens = %v, want Word Comment EOF", toks2)
	}

	// And one byte at a time, so the decision is made at a real chunk edge.
	s3 := lex.Scan(context.Background(), &oneByteAtATime{b: []byte("a/")}, BorrowReader)
	defer s3.Close()
	toks3, _ := collect(t, s3)
	if len(toks3) != 3 || toks3[1].Kind != Operator {
		t.Fatalf("streamed tokens = %v, want Word Operator EOF", toks3)
	}
}

// TestScan_UnterminatedQuoteReportsItsOffset: the form names the construct, the
// core attaches where it started, and the typed error survives the wrap.
func TestScan_UnterminatedQuoteReportsItsOffset(t *testing.T) {
	lex := New(WithForms(sqlish()...))
	s := lex.ScanBytes(context.Background(), []byte("a 'oops"))
	defer s.Close()

	var got error
	for _, err := range s.Tokens() {
		if err != nil {
			got = err
			break
		}
	}
	var unterm *UnterminatedError
	if !errors.As(got, &unterm) {
		t.Fatalf("error = %v, want an *UnterminatedError", got)
	}
	if unterm.Kind != String || unterm.Open != "'" {
		t.Errorf("UnterminatedError{Kind:%v, Open:%q}, want {String, \"'\"}", unterm.Kind, unterm.Open)
	}
	if !strings.Contains(got.Error(), "offset 2") {
		t.Errorf("error %q does not name the offset the construct started at", got)
	}
}

// The rejecting half of the bound: with a limit set, a construct that outruns it
// is refused NAMING the delimiter, the length and the limit.
func TestScan_MaxDelimiterRejectsNamingDelimiterLengthAndLimit(t *testing.T) {
	long := "a '" + strings.Repeat("x", 200)
	lex := New(WithForms(sqlish()...), WithMaxDelimiter(16))
	s := lex.ScanBytes(context.Background(), []byte(long))
	defer s.Close()

	var got error
	for _, err := range s.Tokens() {
		if err != nil {
			got = err
			break
		}
	}
	var be *BoundError
	if !errors.As(got, &be) {
		t.Fatalf("error = %v, want a *BoundError", got)
	}
	if be.Limit != 16 || be.Offset != 2 || be.Kind != String {
		t.Errorf("BoundError{Kind:%v, Offset:%d, Limit:%d}, want {String, 2, 16}", be.Kind, be.Offset, be.Limit)
	}
	if be.Length <= 0 {
		t.Errorf("BoundError.Length = %d, want the bytes examined", be.Length)
	}
	if !strings.Contains(be.Error(), "16") || !strings.Contains(be.Error(), "'") {
		t.Errorf("message %q must name the limit and the delimiter", be.Error())
	}

	// Unbounded, the same input is a plain unterminated report rather than a
	// bounded one: the limit is what makes it bounded.
	s2 := New(WithForms(sqlish()...)).ScanBytes(context.Background(), []byte(long))
	defer s2.Close()
	var got2 error
	for _, err := range s2.Tokens() {
		if err != nil {
			got2 = err
			break
		}
	}
	var unterm *UnterminatedError
	if !errors.As(got2, &unterm) {
		t.Errorf("unbounded error = %v, want an *UnterminatedError", got2)
	}
	if errors.As(got2, new(*BoundError)) {
		t.Errorf("unbounded scan reported a BoundError: %v", got2)
	}
}

// TestScan_NoFormClaimsTheByte: the core will not invent a kind, and will not
// step over input. Without a ByteForm the refusal names the offset.
func TestScan_NoFormClaimsTheByte(t *testing.T) {
	lex := New(WithForms(RunForm(Word, func(_ int, b byte) bool {
		return b >= 'a' && b <= 'z'
	})))
	s := lex.ScanBytes(context.Background(), []byte("ab%cd"))
	defer s.Close()

	var got error
	for _, err := range s.Tokens() {
		if err != nil {
			got = err
			break
		}
	}
	if !errors.Is(got, ErrUnclaimed) {
		t.Fatalf("error = %v, want ErrUnclaimed", got)
	}
	if !strings.Contains(got.Error(), "offset 2") {
		t.Errorf("error %q should name the offset", got)
	}
}

// --- form-contract decoys (the core's half) --------------------------------

type decoyForm struct {
	kind   Kind
	starts func(src []byte) (int, Match)
	ends   func(src, opener []byte, b InputBoundary) (int, error)
}

func (d decoyForm) Kind() Kind { return d.kind }
func (d decoyForm) Starts(src []byte) (int, Match) {
	if d.starts != nil {
		return d.starts(src)
	}
	if len(src) == 0 {
		return 0, Incomplete
	}
	return 1, Matched
}
func (d decoyForm) End(src, opener []byte, b InputBoundary) (int, error) {
	if d.ends != nil {
		return d.ends(src, opener, b)
	}
	return 0, nil
}

func TestScan_FormContractViolationsAreReportedNotAbsorbed(t *testing.T) {
	for _, c := range []struct {
		name string
		form Form
	}{
		{
			// Would return the scan to this offset forever.
			"Matched with n == 0",
			decoyForm{kind: Word, starts: func(src []byte) (int, Match) {
				if len(src) == 0 {
					return 0, Incomplete
				}
				return 0, Matched
			}},
		},
		{
			// Claims bytes it was never shown.
			"Matched with n > len(src)",
			decoyForm{kind: Word, starts: func(src []byte) (int, Match) {
				if len(src) == 0 {
					return 0, Incomplete
				}
				return len(src) + 1, Matched
			}},
		},
		{
			"non-zero n beside NoMatch",
			decoyForm{kind: Word, starts: func(src []byte) (int, Match) { return 3, NoMatch }},
		},
		{
			"non-zero n beside Incomplete",
			decoyForm{kind: Word, starts: func(src []byte) (int, Match) { return 2, Incomplete }},
		},
		{
			// Asks for input that cannot exist.
			"ErrNeedMore at EndOfInput",
			decoyForm{kind: Word, ends: func(_, _ []byte, _ InputBoundary) (int, error) {
				return 0, ErrNeedMore
			}},
		},
		{
			"End returning n > len(src)",
			decoyForm{kind: Word, ends: func(src, _ []byte, _ InputBoundary) (int, error) {
				return len(src) + 5, nil
			}},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := New(WithForms(c.form)).ScanBytes(context.Background(), []byte("abc"))
			defer s.Close()

			var got error
			for _, err := range s.Tokens() {
				if err != nil {
					got = err
					break
				}
			}
			if !errors.Is(got, ErrFormContract) {
				t.Fatalf("error = %v, want ErrFormContract", got)
			}
			if !strings.Contains(got.Error(), "decoyForm") {
				t.Errorf("error %q should name the offending form", got)
			}
		})
	}
}

// --- ownership and close ----------------------------------------------------

type recordingReader struct {
	io.Reader
	closed int
	err    error
}

func (r *recordingReader) Close() error { r.closed++; return r.err }

func TestScan_OwnershipDecidesWhetherTheReaderIsClosed(t *testing.T) {
	own := &recordingReader{Reader: strings.NewReader("ab")}
	s := New(WithForms(sqlish()...)).Scan(context.Background(), own, OwnReader)
	collect(t, s)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if own.closed != 1 {
		t.Errorf("OwnReader closed the reader %d times, want 1", own.closed)
	}

	borrowed := &recordingReader{Reader: strings.NewReader("ab")}
	s2 := New(WithForms(sqlish()...)).Scan(context.Background(), borrowed, BorrowReader)
	collect(t, s2)
	if err := s2.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if borrowed.closed != 0 {
		t.Errorf("BorrowReader closed the reader %d times, want 0", borrowed.closed)
	}
}

// A Scan created and never ranged still releases on Close, and a
// Close error after an early stop reaches the caller.
func TestScan_CloseWithoutRangingAndCloseErrorReachesTheCaller(t *testing.T) {
	never := &recordingReader{Reader: strings.NewReader("abc")}
	s := New(WithForms(sqlish()...)).Scan(context.Background(), never, OwnReader)
	if err := s.Close(); err != nil { // never ranged
		t.Fatalf("Close on an unranged scan: %v", err)
	}
	if never.closed != 1 {
		t.Errorf("an unranged Scan closed the reader %d times, want 1", never.closed)
	}

	boom := errors.New("close failed")
	early := &recordingReader{Reader: strings.NewReader("a b c d"), err: boom}
	s2 := New(WithForms(sqlish()...)).Scan(context.Background(), early, OwnReader)
	for range s2.Tokens() { // stop after the first token
		break
	}
	if err := s2.Close(); !errors.Is(err, boom) {
		t.Errorf("Close after an early stop = %v, want %v", err, boom)
	}
	// Idempotent, and it keeps reporting the same failure.
	if err := s2.Close(); !errors.Is(err, boom) {
		t.Errorf("second Close = %v, want %v", err, boom)
	}
}

func TestScan_UseAfterCloseIsRefused(t *testing.T) {
	s := New(WithForms(sqlish()...)).ScanBytes(context.Background(), []byte("ab"))
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := s.Acquire(Token{Kind: Word, Start: 0, End: 2}); !errors.Is(err, ErrScanClosed) {
		t.Errorf("Acquire after Close = %v, want ErrScanClosed", err)
	}
	if _, err := s.LocationAt(0); !errors.Is(err, ErrScanClosed) {
		t.Errorf("LocationAt after Close = %v, want ErrScanClosed", err)
	}
}

// TestScan_LocationsResolveForTheTokenJustSeen: the diagnostic path, over a
// stream, for the token in hand.
func TestScan_LocationsResolveForTheTokenJustSeen(t *testing.T) {
	src := "a\nbb\nccc"
	lex := New(WithForms(sqlish()...))
	s := lex.Scan(context.Background(), &oneByteAtATime{b: []byte(src)}, BorrowReader)
	defer s.Close()

	want := map[string]Location{
		"a":   {Offset: 0, Line: 1, Column: 1},
		"bb":  {Offset: 2, Line: 2, Column: 1},
		"ccc": {Offset: 5, Line: 3, Column: 1},
	}
	seen := 0
	for tok, err := range s.Tokens() {
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
		if tok.Kind != Word {
			continue
		}
		text := src[tok.Start:tok.End]
		loc, lerr := s.LocationAt(tok.Start)
		if lerr != nil {
			t.Fatalf("LocationAt(%d) for %q: %v", tok.Start, text, lerr)
		}
		if w, ok := want[text]; ok {
			if loc != w {
				t.Errorf("%q at %+v, want %+v", text, loc, w)
			}
			seen++
		}
	}
	if seen != len(want) {
		t.Errorf("checked %d words, want %d", seen, len(want))
	}
}

func TestScan_ContextCancellationStopsTheStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := New(WithForms(sqlish()...)).ScanBytes(ctx, []byte("a b c"))
	defer s.Close()

	var got error
	for _, err := range s.Tokens() {
		if err != nil {
			got = err
			break
		}
	}
	if !errors.Is(got, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", got)
	}
}

func TestScan_EmptyInputIsJustEOF(t *testing.T) {
	s := New(WithForms(sqlish()...)).ScanBytes(context.Background(), nil)
	defer s.Close()
	toks, text := collect(t, s)
	if len(toks) != 1 || toks[0].Kind != EOF || toks[0] != (Token{Kind: EOF}) {
		t.Errorf("tokens = %v, want a single zero-length EOF token at 0", toks)
	}
	if text != "" {
		t.Errorf("text = %q, want empty", text)
	}
}

func TestScan_EarlyStopThenCloseIsClean(t *testing.T) {
	s := New(WithForms(sqlish()...)).ScanBytes(context.Background(), []byte(corpus))
	n := 0
	for range s.Tokens() {
		n++
		if n == 3 {
			break
		}
	}
	if n != 3 {
		t.Fatalf("ranged %d tokens, want to stop at 3", n)
	}
	if err := s.Close(); err != nil {
		t.Errorf("Close after early stop: %v", err)
	}
}

func TestWithMaxDelimiter_NegativePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("WithMaxDelimiter(-1) did not panic")
		}
	}()
	New(WithMaxDelimiter(-1))
}

// TestScan_AcquireYieldsTheTokenBytes checks the accessor rather than the
// concatenation: a straddling span must come back whole.
func TestScan_AcquireYieldsTheTokenBytes(t *testing.T) {
	src := strings.Repeat("word ", 20)
	lex := New(WithForms(sqlish()...))
	s := lex.Scan(context.Background(), &oneByteAtATime{b: []byte(src)}, BorrowReader)
	defer s.Close()

	for tok, err := range s.Tokens() {
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
		if tok.Kind != Word {
			continue
		}
		v, aerr := s.Acquire(tok)
		if aerr != nil {
			t.Fatalf("Acquire(%v): %v", tok, aerr)
		}
		got, rerr := v.AppendTo(nil)
		v.Close()
		if rerr != nil {
			t.Fatalf("AppendTo: %v", rerr)
		}
		if !bytes.Equal(got, []byte("word")) {
			t.Fatalf("token %v bytes = %q, want %q", tok, got, "word")
		}
	}
}

// --- the delimiter bound is INCLUSIVE ---------------------------------------

// A construct exactly as wide as the limit is legal. The geometric widen used to
// reject before ever showing the form its whole allowance: with a limit of 3 and
// the literal "abc", growth went 1, 2, then refused at 4 without examining 3.
func TestScan_BoundIsInclusiveSoAnExactlyWideConstructLexes(t *testing.T) {
	lex := New(WithForms(SetForm(Operator, "abc")), WithMaxDelimiter(3))
	s := lex.ScanBytes(context.Background(), []byte("abc"))
	defer s.Close()

	toks, text := collect(t, s)
	if len(toks) != 2 || toks[0].Kind != Operator || toks[0].Len() != 3 {
		t.Fatalf("tokens = %v, want a 3-byte Operator then EOF", toks)
	}
	if text != "abc" {
		t.Errorf("text = %q, want %q", text, "abc")
	}
}

// And a construct that genuinely outruns the limit fails only after being shown
// exactly it — the reported length is the limit, never less and never more.
func TestScan_BoundFailsAtExactlyTheLimit(t *testing.T) {
	lex := New(WithForms(SetForm(Operator, "abc")), WithMaxDelimiter(2))
	s := lex.ScanBytes(context.Background(), []byte("abc"))
	defer s.Close()

	var got error
	for _, err := range s.Tokens() {
		if err != nil {
			got = err
			break
		}
	}
	var be *BoundError
	if !errors.As(got, &be) {
		t.Fatalf("error = %v, want a *BoundError", got)
	}
	if be.Length != 2 || be.Limit != 2 {
		t.Errorf("BoundError{Length:%d, Limit:%d}, want {2, 2} — it must examine exactly the limit",
			be.Length, be.Limit)
	}
}

// The bound covers the WHOLE construct, opener included. Resolving End used to
// measure only the remainder, so a 2-byte opener under a limit of 4 could read 6.
// BoundError.Open must also be the opener itself, not the opener plus the body
// scanned so far.
func TestScan_BoundCoversOpenerPlusRemainder(t *testing.T) {
	// The remainder End is SHOWN is what observes the defect: reporting a length
	// is not the same as having stopped reading at one.
	maxRest := 0
	twoByteOpener := decoyForm{
		kind: String,
		starts: func(src []byte) (int, Match) {
			if len(src) < 2 {
				return 0, Incomplete
			}
			return 2, Matched
		},
		ends: func(src, _ []byte, b InputBoundary) (int, error) {
			if len(src) > maxRest {
				maxRest = len(src)
			}
			if b == MoreInput {
				return 0, ErrNeedMore // never decides, so the bound must stop it
			}
			return 0, &UnterminatedError{Kind: String, Open: "ab"}
		},
	}
	lex := New(WithForms(twoByteOpener), WithMaxDelimiter(4))
	s := lex.ScanBytes(context.Background(), []byte("abcdefghij"))
	defer s.Close()

	var got error
	for _, err := range s.Tokens() {
		if err != nil {
			got = err
			break
		}
	}
	var be *BoundError
	if !errors.As(got, &be) {
		t.Fatalf("error = %v, want a *BoundError", got)
	}
	if maxRest > 2 {
		t.Errorf("End was shown a %d-byte remainder; with a 2-byte opener under a limit of 4 it "+
			"may be shown at most 2 — the opener counts toward the bound", maxRest)
	}
	if be.Length != 4 {
		t.Errorf("BoundError.Length = %d, want 4 — the whole construct's width", be.Length)
	}
	if be.Open != "ab" {
		t.Errorf("BoundError.Open = %q, want %q — the opener, not the opener plus the body", be.Open, "ab")
	}
}

// --- the End matrix is enforced whole ---------------------------------------

func TestScan_EndMatrixViolationsAreReported(t *testing.T) {
	for _, c := range []struct {
		name string
		ends func(src, opener []byte, b InputBoundary) (int, error)
	}{
		{
			// A count beside an error claims bytes the form just refused to judge.
			"non-zero n beside a terminal error",
			func(_, _ []byte, _ InputBoundary) (int, error) {
				return 1, errors.New("boom")
			},
		},
		{
			// While more input may arrive the only refusal available is ErrNeedMore.
			"terminal error under MoreInput",
			func(_, _ []byte, b InputBoundary) (int, error) {
				if b == MoreInput {
					return 0, errors.New("gave up early")
				}
				return 0, nil
			},
		},
		{
			// At end of input an unclosable construct must say so in a type a
			// caller can name.
			"untyped error at EndOfInput",
			func(_, _ []byte, b InputBoundary) (int, error) {
				if b == MoreInput {
					return 0, ErrNeedMore
				}
				return 0, errors.New("just an error")
			},
		},
		{
			"negative success",
			func(_, _ []byte, _ InputBoundary) (int, error) { return -1, nil },
		},
		{
			"non-zero n beside ErrNeedMore",
			func(_, _ []byte, b InputBoundary) (int, error) {
				if b == MoreInput {
					return 2, ErrNeedMore
				}
				return 0, nil
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := decoyForm{kind: Word, ends: c.ends}
			s := New(WithForms(f)).Scan(context.Background(),
				&oneByteAtATime{b: []byte("abcdef")}, BorrowReader)
			defer s.Close()

			var got error
			for _, err := range s.Tokens() {
				if err != nil {
					got = err
					break
				}
			}
			if !errors.Is(got, ErrFormContract) {
				t.Fatalf("error = %v, want ErrFormContract", got)
			}
		})
	}
}

// A contract error names the form's POSITION in the list. Two instances of one
// type are ordinary, and a type name alone cannot say which of them is wrong.
func TestScan_ContractErrorsNameTheFormsListIndex(t *testing.T) {
	good := decoyForm{kind: Word, starts: func(src []byte) (int, Match) { return 0, NoMatch }}
	bad := decoyForm{kind: Word, starts: func(src []byte) (int, Match) { return 3, NoMatch }}
	s := New(WithForms(good, good, bad)).ScanBytes(context.Background(), []byte("abc"))
	defer s.Close()

	var got error
	for _, err := range s.Tokens() {
		if err != nil {
			got = err
			break
		}
	}
	if !errors.Is(got, ErrFormContract) {
		t.Fatalf("error = %v, want ErrFormContract", got)
	}
	if !strings.Contains(got.Error(), "form[2]") {
		t.Errorf("error %q must name the offending form's index in the list", got)
	}
}

// --- a successful location is never provisional -----------------------------

// A four-byte rune arriving one byte at a time. Offset 1 is inside it, so it is
// not a position and must be refused CONSISTENTLY. The head used to track what
// the CACHE had read rather than what had been INDEXED, so the same offset
// answered 1:2 from a lone lead byte and then refused once the rune completed.
func TestScan_ALocationIsNeverProvisional(t *testing.T) {
	s := New(WithForms(ByteForm(Punct))).Scan(context.Background(),
		&oneByteAtATime{b: []byte("\U0001F600x")}, BorrowReader)
	defer s.Close()

	seen := 0
	for tok, err := range s.Tokens() {
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
		_ = tok
		seen++
		if _, lerr := s.LocationAt(1); !errors.Is(lerr, ErrLocationRange) {
			t.Fatalf("observation %d: LocationAt(1) = %v, want a stable ErrLocationRange — "+
				"offset 1 is inside a four-byte rune at every point in the scan", seen, lerr)
		}
	}
	if seen == 0 {
		t.Fatal("the scan produced no tokens")
	}
}

// Asking about a later line before the walk reaches it must be answered from the
// line index, not from whatever the cache happens to have read. NewBytes knows
// the whole length immediately, so the cache head runs far ahead of the index;
// the old code reported line 1 for an offset on line 3.
func TestScan_LocationAheadOfTheWalkIsIndexedNotGuessed(t *testing.T) {
	s := New(WithForms(sqlish()...)).ScanBytes(context.Background(), []byte("a\nb\nc"))
	defer s.Close()

	loc, err := s.LocationAt(4) // the "c", on line 3
	if err != nil {
		t.Fatalf("LocationAt(4): %v", err)
	}
	if loc.Line != 3 || loc.Column != 1 {
		t.Errorf("LocationAt(4) = %+v, want line 3 column 1", loc)
	}
}

// --- Close lets go of what the Scan holds -----------------------------------

func TestScan_CloseDropsItsBuffersAndAHeldViewSurvives(t *testing.T) {
	s := New(WithForms(sqlish()...)).ScanBytes(context.Background(), []byte(corpus))

	var held *streamcache.View
	for tok, err := range s.Tokens() {
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
		if held == nil && tok.Kind == Word {
			v, aerr := s.Acquire(tok)
			if aerr != nil {
				t.Fatalf("Acquire(%v): %v", tok, aerr)
			}
			held = v
		}
	}
	if held == nil {
		t.Fatal("no word token to hold")
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// The View retains the cache on its own, so closing the Scan cannot
	// invalidate it.
	got, rerr := held.AppendTo(nil)
	held.Close()
	if rerr != nil {
		t.Fatalf("a View held across Close: %v", rerr)
	}
	if string(got) != "select" {
		t.Errorf("held View = %q, want %q", got, "select")
	}

	if s.win != nil || s.openBuf != nil || s.idxBuf != nil || s.fixed != nil {
		t.Error("Close left the Scan holding its window buffers")
	}
	if s.cache != nil || s.src != nil {
		t.Error("Close left the Scan holding the cache and source graph")
	}
}

// --- two window providers, one state machine --------------------------------

// ScanBytes hands forms a RESLICE of the caller's input; the streamed path hands
// them its own copied buffer. The probe retains the window, which the Form
// contract forbids — deliberately, because pointer identity is the property
// under test.
func TestScan_WindowProviders(t *testing.T) {
	probe := func(seen *[]byte) Form {
		return decoyForm{kind: Word, starts: func(src []byte) (int, Match) {
			if *seen == nil && len(src) > 0 {
				*seen = src
			}
			if len(src) == 0 {
				return 0, Incomplete
			}
			return 1, Matched
		}}
	}

	b := []byte("abcdef")
	var fixedWin []byte
	sb := New(WithForms(probe(&fixedWin))).ScanBytes(context.Background(), b)
	defer sb.Close()
	for _, err := range sb.Tokens() {
		if err != nil {
			t.Fatalf("ScanBytes: %v", err)
		}
	}
	if fixedWin == nil {
		t.Fatal("the probe never saw a window")
	}
	if unsafe.SliceData(fixedWin) != unsafe.SliceData(b) {
		t.Error("ScanBytes copied the window; it must reslice the caller's slice")
	}

	var streamWin []byte
	sr := New(WithForms(probe(&streamWin))).Scan(context.Background(),
		&oneByteAtATime{b: b}, BorrowReader)
	defer sr.Close()
	for _, err := range sr.Tokens() {
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}
	}
	if streamWin == nil {
		t.Fatal("the probe never saw a streamed window")
	}
	if unsafe.SliceData(streamWin) == unsafe.SliceData(b) {
		t.Error("the streamed path aliased the input; segmented bytes must be copied once")
	}
}

// countingReader serves a fixed number of bytes and records how many it was
// asked for, which is how "a diagnostic did not read ahead" becomes observable.
type countingReader struct {
	served int64
	limit  int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	if r.served >= r.limit {
		return 0, io.EOF
	}
	k := int64(len(p))
	if r.served+k > r.limit {
		k = r.limit - r.served
	}
	for i := int64(0); i < k; i++ {
		p[i] = 'a'
	}
	r.served += k
	return int(k), nil
}

// A streamed location must not drive the reader forward. Asking about an offset
// far ahead of what has been indexed is a question about bytes the scan has not
// reached, and answering it by reading to them turns a diagnostic into an
// unbounded read — 900 kB of it, before this was refused.
func TestScan_StreamedLocationIsNotReadForward(t *testing.T) {
	r := &countingReader{limit: 1 << 20}
	s := New(WithForms(ByteForm(Punct))).Scan(context.Background(), r, BorrowReader)
	defer s.Close()

	for range s.Tokens() { // one token: live, but barely read
		break
	}
	before := r.served

	if _, err := s.LocationAt(900000); !errors.Is(err, ErrLocationRange) {
		t.Fatalf("LocationAt(900000) = %v, want ErrLocationRange", err)
	}
	if grew := r.served - before; grew != 0 {
		t.Errorf("answering a far-future location read %d more bytes from the source; a "+
			"diagnostic must refuse rather than read ahead", grew)
	}
}

// At the live edge the answer IS available, and the lookahead costs at most
// utf8.UTFMax-1 bytes of new INDEXING.
//
// Indexing only. This says nothing about bytes consumed from the reader, which
// the cache decides — see TestScan_LiveEdgeLocationConsumesAtMostOneSegment for
// that ceiling. Asserting the index growth and calling it I/O is how the doc came
// to claim the lookahead was all it would read: a 3-byte index growth sat happily
// on top of a 32 KiB segment fill.
func TestScan_LiveEdgeLocationIndexesAtMostTheLookahead(t *testing.T) {
	r := &countingReader{limit: 1 << 20}
	s := New(WithForms(ByteForm(Punct))).Scan(context.Background(), r, BorrowReader)
	defer s.Close()

	for range s.Tokens() {
		break
	}
	edge := s.notedTo
	if _, err := s.LocationAt(edge); err != nil {
		t.Fatalf("LocationAt at the indexed edge %d: %v", edge, err)
	}
	if grew := s.notedTo - edge; grew > int64(utf8.UTFMax-1) {
		t.Errorf("the lookahead indexed %d bytes past the edge; at most %d is allowed",
			grew, utf8.UTFMax-1)
	}
}

// Over a slice the whole input is already in memory, so an in-range offset ahead
// of the walk stays answerable — there is no read to bound.
func TestScan_FixedProviderStillAnswersAheadOfTheWalk(t *testing.T) {
	s := New(WithForms(ByteForm(Punct))).ScanBytes(context.Background(), []byte("a\nb\nc"))
	defer s.Close()

	for range s.Tokens() {
		break
	}
	loc, err := s.LocationAt(4)
	if err != nil {
		t.Fatalf("LocationAt(4) over a slice: %v", err)
	}
	if loc.Line != 3 || loc.Column != 1 {
		t.Errorf("LocationAt(4) = %+v, want line 3 column 1", loc)
	}
}

// Close drops EVERY reference the Scan owns, not just the buffers: a form list
// closes over whatever its author put there, and a context can carry a whole
// value graph.
func TestScan_CloseDropsEveryReference(t *testing.T) {
	s := New(WithForms(sqlish()...)).ScanBytes(context.Background(), []byte(corpus))
	collect(t, s)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	for _, c := range []struct {
		name string
		held bool
	}{
		{"the form list", s.forms != nil},
		{"its context", s.ctx != nil},
		{"the terminal error", s.err != nil},
		{"the window buffer", s.win != nil},
		{"the opener buffer", s.openBuf != nil},
		{"the lookahead buffer", s.idxBuf != nil},
		{"the fixed input", s.fixed != nil},
		{"the cache", s.cache != nil},
		{"the source", s.src != nil},
		{"the closer", s.rc != nil},
	} {
		if c.held {
			t.Errorf("Close retained %s", c.name)
		}
	}
}

// A failed scan keeps reporting what went wrong until it is closed; a closed one
// reports that it is closed, because the error value goes with every other
// reference. The FACT of the failure is kept as a scalar.
func TestScan_FailedThenClosedReportsClosed(t *testing.T) {
	bad := decoyForm{kind: Word, starts: func([]byte) (int, Match) { return 3, NoMatch }}
	s := New(WithForms(bad)).ScanBytes(context.Background(), []byte("abc"))

	first := func() error {
		for _, err := range s.Tokens() {
			if err != nil {
				return err
			}
		}
		return nil
	}

	if err := first(); !errors.Is(err, ErrFormContract) {
		t.Fatalf("first error = %v, want ErrFormContract", err)
	}
	if err := first(); !errors.Is(err, ErrFormContract) {
		t.Errorf("re-ranged before Close = %v, want the same failure", err)
	}
	if !s.failed {
		t.Error("the failure was not recorded")
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if s.err != nil {
		t.Error("Close retained the error value")
	}
	if !s.failed {
		t.Error("Close lost the fact that the scan had failed")
	}
	if err := first(); !errors.Is(err, ErrScanClosed) {
		t.Errorf("after Close = %v, want ErrScanClosed", err)
	}
}

// stagedReader dribbles one byte on its first call and fills the buffer after
// that. The dribble is what leaves the cache nearly empty at the live edge, so
// the NEXT read — a segment fill — becomes observable instead of being served
// from bytes the cache had already gathered.
type stagedReader struct {
	served, limit int64
	calls         int
}

func (r *stagedReader) Read(p []byte) (int, error) {
	if r.served >= r.limit {
		return 0, io.EOF
	}
	r.calls++
	k := int64(len(p))
	if r.calls == 1 {
		k = 1
	}
	if r.served+k > r.limit {
		k = r.limit - r.served
	}
	for i := int64(0); i < k; i++ {
		p[i] = 'a'
	}
	r.served += k
	return int(k), nil
}

// The I/O ceiling for a live-edge location is ONE SEGMENT, not three bytes and
// not the rest of the stream. The cache reads a segment at a time on purpose —
// the alternative is a syscall per rune — so a three-byte lookahead can ride on
// top of a segment fill. What must never happen is draining the gap to an
// arbitrary offset, which the streamed refusal above prevents.
func TestScan_LiveEdgeLocationConsumesAtMostOneSegment(t *testing.T) {
	const streamSize = 1 << 20
	const segment = 32 << 10 // streamcache's default; the ceiling pinned here

	r := &stagedReader{limit: streamSize}
	s := New(WithForms(ByteForm(Punct))).Scan(context.Background(), r, BorrowReader)
	defer s.Close()

	for range s.Tokens() {
		break
	}
	before := r.served
	if _, err := s.LocationAt(s.notedTo); err != nil {
		t.Fatalf("LocationAt at the indexed edge: %v", err)
	}

	grew := r.served - before
	if grew > segment {
		t.Errorf("a live-edge location consumed %d underlying bytes; the ceiling is one "+
			"segment (%d)", grew, segment)
	}
	if grew >= streamSize/2 {
		t.Errorf("a live-edge location drained %d of a %d-byte stream; it must never read an "+
			"arbitrary gap", grew, streamSize)
	}
}
