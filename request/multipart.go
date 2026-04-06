package request

import (
	"bytes"
	"mime/multipart"
)

// MultipartBuffer wraps bytes.Buffer to implement CustomPayload for multipart
// form data.
type MultipartBuffer struct {
	bytes.Buffer
	Boundary string
}

func (w *MultipartBuffer) ContentType() string {
	return "multipart/form-data; boundary=" + w.Boundary
}

// FormWriter is a convenience builder for constructing multipart/form-data
// payloads. It composes multipart.Writer and bytes.Buffer, and implements
// CustomPayload so it can be passed directly to Request as a payload.
type FormWriter struct {
	buf bytes.Buffer
	w   *multipart.Writer
}

// NewFormWriter creates a new FormWriter ready for use.
func NewFormWriter() *FormWriter {
	fw := &FormWriter{}
	fw.w = multipart.NewWriter(&fw.buf)
	return fw
}

// Writer returns the underlying multipart.Writer for advanced use cases
// (e.g. calling CreatePart directly).
func (fw *FormWriter) Writer() *multipart.Writer {
	return fw.w
}

// WriteFile adds a file part to the multipart form. Returns the number of
// bytes written. Nil files are silently skipped.
func (fw *FormWriter) WriteFile(field string, file *FileUpload) (int64, error) {
	return WriteFile(fw.w, field, file)
}

// WriteFields encodes struct fields into the multipart form using JSON tags.
func (fw *FormWriter) WriteFields(meta any) error {
	return WriteFields(fw.w, meta)
}

// WriteField writes a single form field.
func (fw *FormWriter) WriteField(key, value string) error {
	return fw.w.WriteField(key, value)
}

// Close finalizes the multipart message. Must be called before using the
// FormWriter as a payload.
func (fw *FormWriter) Close() error {
	return fw.w.Close()
}

// Read implements io.Reader, making FormWriter usable as a CustomPayload.
func (fw *FormWriter) Read(p []byte) (int, error) {
	return fw.buf.Read(p)
}

// ContentType returns the full multipart content-type header including boundary.
func (fw *FormWriter) ContentType() string {
	return fw.w.FormDataContentType()
}

// Boundary returns the multipart boundary string.
func (fw *FormWriter) Boundary() string {
	return fw.w.Boundary()
}
