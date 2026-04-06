package request

import (
	"bytes"
	"io"
	"mime/multipart"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// WriteFile
// ---------------------------------------------------------------------------

func TestWriteFile_Success(t *testing.T) {
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)

	file := &FileUpload{
		FileName: "/path/to/track.mp3",
		Content:  strings.NewReader("audio-data"),
	}
	n, err := WriteFile(w, "track", file)
	if err != nil {
		t.Fatal(err)
	}
	if n != 10 {
		t.Fatalf("expected 10 bytes, got %d", n)
	}
	w.Close()

	// Parse the multipart form to verify.
	r := multipart.NewReader(body, w.Boundary())
	form, err := r.ReadForm(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	files := form.File["track"]
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Filename != "track.mp3" {
		t.Fatalf("expected track.mp3, got %s", files[0].Filename)
	}
	f, _ := files[0].Open()
	data, _ := io.ReadAll(f)
	if string(data) != "audio-data" {
		t.Fatalf("expected audio-data, got %q", string(data))
	}
}

func TestWriteFile_NilFile(t *testing.T) {
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	n, err := WriteFile(w, "track", nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected 0 bytes for nil file, got %d", n)
	}
}

func TestWriteFile_BaseName(t *testing.T) {
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)

	file := &FileUpload{
		FileName: "deeply/nested/dir/image.png",
		Content:  strings.NewReader("png"),
	}
	WriteFile(w, "image", file)
	w.Close()

	r := multipart.NewReader(body, w.Boundary())
	form, _ := r.ReadForm(1 << 20)
	if form.File["image"][0].Filename != "image.png" {
		t.Fatalf("expected base name image.png, got %s", form.File["image"][0].Filename)
	}
}

func TestWriteFile_MultipleFiles(t *testing.T) {
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)

	WriteFile(w, "file1", &FileUpload{FileName: "a.txt", Content: strings.NewReader("aaa")})
	WriteFile(w, "file2", &FileUpload{FileName: "b.txt", Content: strings.NewReader("bbb")})
	w.Close()

	r := multipart.NewReader(body, w.Boundary())
	form, _ := r.ReadForm(1 << 20)
	if len(form.File["file1"]) != 1 || len(form.File["file2"]) != 1 {
		t.Fatal("expected both files present")
	}
}

// ---------------------------------------------------------------------------
// WriteFields
// ---------------------------------------------------------------------------

func TestWriteFields_BasicTypes(t *testing.T) {
	type meta struct {
		Title  string `json:"title"`
		Artist string `json:"artist"`
		Year   int    `json:"year"`
		BPM    uint64 `json:"bpm"`
	}

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	err := WriteFields(w, meta{Title: "Song", Artist: "Band", Year: 2024, BPM: 120})
	if err != nil {
		t.Fatal(err)
	}
	w.Close()

	r := multipart.NewReader(body, w.Boundary())
	form, _ := r.ReadForm(1 << 20)
	assertField(t, form, "title", "Song")
	assertField(t, form, "artist", "Band")
	assertField(t, form, "year", "2024")
	assertField(t, form, "bpm", "120")
}

func TestWriteFields_Bool(t *testing.T) {
	type meta struct {
		Active   bool `json:"active"`
		Archived bool `json:"archived"`
	}

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	WriteFields(w, meta{Active: true, Archived: false})
	w.Close()

	r := multipart.NewReader(body, w.Boundary())
	form, _ := r.ReadForm(1 << 20)
	assertField(t, form, "active", "1")
	assertField(t, form, "archived", "0")
}

func TestWriteFields_Slice(t *testing.T) {
	type meta struct {
		Tags []string `json:"tags"`
	}

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	WriteFields(w, meta{Tags: []string{"rock", "indie", "alt"}})
	w.Close()

	r := multipart.NewReader(body, w.Boundary())
	form, _ := r.ReadForm(1 << 20)
	vals := form.Value["tags[]"]
	if len(vals) != 3 {
		t.Fatalf("expected 3 tags, got %d", len(vals))
	}
	if vals[0] != "rock" || vals[1] != "indie" || vals[2] != "alt" {
		t.Fatalf("unexpected tag values: %v", vals)
	}
}

func TestWriteFields_EmptySlice_Omitted(t *testing.T) {
	type meta struct {
		Tags []string `json:"tags"`
	}

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	WriteFields(w, meta{Tags: []string{}})
	w.Close()

	r := multipart.NewReader(body, w.Boundary())
	form, _ := r.ReadForm(1 << 20)
	if _, ok := form.Value["tags[]"]; ok {
		t.Fatal("empty slice should be omitted")
	}
}

func TestWriteFields_Pointer(t *testing.T) {
	type meta struct {
		Note *string `json:"note"`
	}

	note := "hello"
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	WriteFields(w, meta{Note: &note})
	w.Close()

	r := multipart.NewReader(body, w.Boundary())
	form, _ := r.ReadForm(1 << 20)
	assertField(t, form, "note", "hello")
}

func TestWriteFields_NilPointer_Omitted(t *testing.T) {
	type meta struct {
		Note *string `json:"note"`
	}

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	WriteFields(w, meta{Note: nil})
	w.Close()

	r := multipart.NewReader(body, w.Boundary())
	form, _ := r.ReadForm(1 << 20)
	if _, ok := form.Value["note"]; ok {
		t.Fatal("nil pointer should be omitted")
	}
}

func TestWriteFields_ZeroValues_Omitted(t *testing.T) {
	type meta struct {
		Title string `json:"title"`
		Year  int    `json:"year"`
		BPM   uint64 `json:"bpm"`
		Rate  float64 `json:"rate"`
	}

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	WriteFields(w, meta{})
	w.Close()

	r := multipart.NewReader(body, w.Boundary())
	form, _ := r.ReadForm(1 << 20)
	for _, key := range []string{"title", "year", "bpm", "rate"} {
		if _, ok := form.Value[key]; ok {
			t.Fatalf("zero-value field %q should be omitted", key)
		}
	}
}

func TestWriteFields_SkipDashTag(t *testing.T) {
	type meta struct {
		Internal string `json:"-"`
		Public   string `json:"public"`
	}

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	WriteFields(w, meta{Internal: "secret", Public: "visible"})
	w.Close()

	r := multipart.NewReader(body, w.Boundary())
	form, _ := r.ReadForm(1 << 20)
	if _, ok := form.Value["-"]; ok {
		t.Fatal("field with json:\"-\" should be skipped")
	}
	if _, ok := form.Value["Internal"]; ok {
		t.Fatal("field with json:\"-\" should be skipped")
	}
	assertField(t, form, "public", "visible")
}

func TestWriteFields_NoTag_Skipped(t *testing.T) {
	type meta struct {
		NoTag  string
		Tagged string `json:"tagged"`
	}

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	WriteFields(w, meta{NoTag: "x", Tagged: "y"})
	w.Close()

	r := multipart.NewReader(body, w.Boundary())
	form, _ := r.ReadForm(1 << 20)
	if _, ok := form.Value["NoTag"]; ok {
		t.Fatal("untagged field should be skipped")
	}
	assertField(t, form, "tagged", "y")
}

func TestWriteFields_Float(t *testing.T) {
	type meta struct {
		Score float64 `json:"score"`
	}

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	WriteFields(w, meta{Score: 3.14})
	w.Close()

	r := multipart.NewReader(body, w.Boundary())
	form, _ := r.ReadForm(1 << 20)
	assertField(t, form, "score", "3.14")
}

func TestWriteFields_PointerToStruct(t *testing.T) {
	type meta struct {
		Title string `json:"title"`
	}

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	err := WriteFields(w, &meta{Title: "ptr"})
	if err != nil {
		t.Fatal(err)
	}
	w.Close()

	r := multipart.NewReader(body, w.Boundary())
	form, _ := r.ReadForm(1 << 20)
	assertField(t, form, "title", "ptr")
}

func TestWriteFields_IntSlice(t *testing.T) {
	type meta struct {
		IDs []int `json:"ids"`
	}

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	WriteFields(w, meta{IDs: []int{1, 2, 3}})
	w.Close()

	r := multipart.NewReader(body, w.Boundary())
	form, _ := r.ReadForm(1 << 20)
	vals := form.Value["ids[]"]
	if len(vals) != 3 || vals[0] != "1" || vals[1] != "2" || vals[2] != "3" {
		t.Fatalf("unexpected int slice values: %v", vals)
	}
}

// ---------------------------------------------------------------------------
// Combined: WriteFile + WriteFields
// ---------------------------------------------------------------------------

func TestWriteFile_And_WriteFields_Together(t *testing.T) {
	type meta struct {
		Title  string `json:"title"`
		Artist string `json:"artist"`
	}

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)

	WriteFile(w, "track", &FileUpload{FileName: "song.mp3", Content: strings.NewReader("audio")})
	WriteFields(w, meta{Title: "My Song", Artist: "Me"})
	w.Close()

	r := multipart.NewReader(body, w.Boundary())
	form, _ := r.ReadForm(1 << 20)

	// Verify file.
	if len(form.File["track"]) != 1 {
		t.Fatal("expected track file")
	}
	if form.File["track"][0].Filename != "song.mp3" {
		t.Fatalf("expected song.mp3, got %s", form.File["track"][0].Filename)
	}

	// Verify fields.
	assertField(t, form, "title", "My Song")
	assertField(t, form, "artist", "Me")
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func assertField(t *testing.T, form *multipart.Form, key, want string) {
	t.Helper()
	vals, ok := form.Value[key]
	if !ok {
		t.Fatalf("expected field %q to exist", key)
	}
	if len(vals) == 0 || vals[0] != want {
		t.Fatalf("field %q: expected %q, got %v", key, want, vals)
	}
}
