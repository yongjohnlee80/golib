package request

import (
	"bytes"
	"io"
	"mime/multipart"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// FormWriter
// ---------------------------------------------------------------------------

func TestFormWriter_ImplementsCustomPayload(t *testing.T) {
	fw := NewFormWriter()
	fw.WriteField("key", "val")
	fw.Close()
	var _ CustomPayload = fw
}

func TestFormWriter_ContentType(t *testing.T) {
	fw := NewFormWriter()
	ct := fw.ContentType()
	if !strings.HasPrefix(ct, "multipart/form-data; boundary=") {
		t.Fatalf("unexpected content-type: %s", ct)
	}
}

func TestFormWriter_Boundary(t *testing.T) {
	fw := NewFormWriter()
	if fw.Boundary() == "" {
		t.Fatal("expected non-empty boundary")
	}
}

func TestFormWriter_WriteField(t *testing.T) {
	fw := NewFormWriter()
	if err := fw.WriteField("name", "alice"); err != nil {
		t.Fatal(err)
	}
	fw.Close()

	form := parseForm(t, fw)
	assertFormField(t, form, "name", "alice")
}

func TestFormWriter_WriteFile(t *testing.T) {
	fw := NewFormWriter()
	n, err := fw.WriteFile("track", &FileUpload{
		FileName: "/music/song.mp3",
		Content:  strings.NewReader("audio-bytes"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 11 {
		t.Fatalf("expected 11 bytes, got %d", n)
	}
	fw.Close()

	form := parseForm(t, fw)
	files := form.File["track"]
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Filename != "song.mp3" {
		t.Fatalf("expected song.mp3, got %s", files[0].Filename)
	}
}

func TestFormWriter_WriteFile_Nil(t *testing.T) {
	fw := NewFormWriter()
	n, err := fw.WriteFile("track", nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected 0 for nil file, got %d", n)
	}
}

func TestFormWriter_WriteFields(t *testing.T) {
	type meta struct {
		Title  string `json:"title"`
		Artist string `json:"artist"`
		Year   int    `json:"year"`
	}

	fw := NewFormWriter()
	if err := fw.WriteFields(meta{Title: "Song", Artist: "Band", Year: 2024}); err != nil {
		t.Fatal(err)
	}
	fw.Close()

	form := parseForm(t, fw)
	assertFormField(t, form, "title", "Song")
	assertFormField(t, form, "artist", "Band")
	assertFormField(t, form, "year", "2024")
}

func TestFormWriter_Combined(t *testing.T) {
	type meta struct {
		Title string `json:"title"`
	}

	fw := NewFormWriter()
	fw.WriteFile("track", &FileUpload{FileName: "a.mp3", Content: strings.NewReader("data")})
	fw.WriteFields(meta{Title: "My Song"})
	fw.WriteField("extra", "value")
	fw.Close()

	form := parseForm(t, fw)
	if len(form.File["track"]) != 1 {
		t.Fatal("expected track file")
	}
	assertFormField(t, form, "title", "My Song")
	assertFormField(t, form, "extra", "value")
}

func TestFormWriter_Writer(t *testing.T) {
	fw := NewFormWriter()
	w := fw.Writer()
	if w == nil {
		t.Fatal("expected non-nil Writer")
	}
	// Use the raw writer directly.
	w.WriteField("raw", "yes")
	fw.Close()

	form := parseForm(t, fw)
	assertFormField(t, form, "raw", "yes")
}

func TestFormWriter_AsRequestPayload(t *testing.T) {
	fw := NewFormWriter()
	fw.WriteField("key", "val")
	fw.Close()

	// Verify it can be read as an io.Reader.
	data, err := io.ReadAll(fw)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty payload")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func parseForm(t *testing.T, fw *FormWriter) *multipart.Form {
	t.Helper()
	// Read from the FormWriter's buffer.
	buf := new(bytes.Buffer)
	// We need to re-read, but the buffer was already consumed by Close.
	// Use the boundary to parse.
	// Actually, FormWriter.Read reads from the internal buffer.
	// But if we already called Close, the data should be in the buffer.
	// Let's copy from the FormWriter.
	io.Copy(buf, fw)
	r := multipart.NewReader(buf, fw.Boundary())
	form, err := r.ReadForm(1 << 20)
	if err != nil {
		t.Fatalf("failed to parse form: %v", err)
	}
	return form
}

func assertFormField(t *testing.T, form *multipart.Form, key, want string) {
	t.Helper()
	vals, ok := form.Value[key]
	if !ok {
		t.Fatalf("expected field %q to exist", key)
	}
	if len(vals) == 0 || vals[0] != want {
		t.Fatalf("field %q: expected %q, got %v", key, want, vals)
	}
}
