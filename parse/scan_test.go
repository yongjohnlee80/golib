package parse

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
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
