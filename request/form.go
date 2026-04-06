package request

import (
	"fmt"
	"io"
	"mime/multipart"
	"path/filepath"
	"reflect"
	"strings"
)

// FileUpload represents a file with its name and content for HTTP multipart upload.
type FileUpload struct {
	FileName string
	Content  io.Reader
}

// WriteFile writes a file to a multipart.Writer using the specified field name.
// Returns the number of bytes written or an error. Returns 0 if file is nil.
func WriteFile(w *multipart.Writer, field string, file *FileUpload) (int64, error) {
	if file == nil {
		return 0, nil
	}
	f, err := w.CreateFormFile(field, filepath.Base(file.FileName))
	if err != nil {
		return 0, err
	}
	return io.Copy(f, file.Content)
}

// WriteFields encodes struct fields into a multipart.Writer using their JSON
// tags as form field keys. Fields tagged with "-" or without a JSON tag are
// skipped. Zero-value fields are omitted (empty strings, 0 ints, nil pointers,
// empty slices).
func WriteFields(w *multipart.Writer, meta any) error {
	v := reflect.ValueOf(meta)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fv := v.Field(i)

		tag := field.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		key := strings.Split(tag, ",")[0]

		if err := WriteField(w, key, fv); err != nil {
			return err
		}
	}
	return nil
}

func WriteField(w *multipart.Writer, key string, fv reflect.Value) (err error) {
	switch fv.Kind() {
	case reflect.String:
		if fv.String() != "" {
			err = w.WriteField(key, fv.String())
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if fv.Int() != 0 {
			err = w.WriteField(key, fmt.Sprintf("%d", fv.Int()))
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if fv.Uint() != 0 {
			err = w.WriteField(key, fmt.Sprintf("%d", fv.Uint()))
		}
	case reflect.Float32, reflect.Float64:
		if fv.Float() != 0 {
			err = w.WriteField(key, fmt.Sprintf("%g", fv.Float()))
		}
	case reflect.Bool:
		if fv.Bool() {
			err = w.WriteField(key, "1")
		} else {
			err = w.WriteField(key, "0")
		}
	case reflect.Slice:
		if fv.Len() > 0 {
			sliceKey := key + "[]"
			for j := 0; j < fv.Len(); j++ {
				if err = w.WriteField(sliceKey, fmt.Sprintf("%v", fv.Index(j).Interface())); err != nil {
					return
				}
			}
		}
	case reflect.Ptr:
		if !fv.IsNil() {
			err = WriteField(w, key, fv.Elem())
		}
	default:
		err = w.WriteField(key, fmt.Sprintf("%v", fv.Interface()))
	}
	return
}
